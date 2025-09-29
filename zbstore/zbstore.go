// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

// Package zbstore provides data types and functions used to represent the zb store.
// Conceptually, a zb store is a directory.
// The direct children of a store directory are called store objects.
// Store objects can be regular files, executable files, symbolic links (symlinks),
// or directories containing any of the file types listed.
// Store objects are content-addressed, so they are named by their contents.
//
// Package zbstore provides the [Directory] and [Path] types for path manipulation.
// The [Store] interface allows access to a collection of objects.
package zbstore

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"slices"
	"sync"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"golang.org/x/sync/errgroup"
	"zb.256lights.llc/pkg/sets"
	"zombiezen.com/go/nix"
)

// A Store represents a collection of zb store objects.
type Store interface {
	// Object reads the metadata and obtains a handle to the object with the given path.
	// If there is no such object, then Object returns an error
	// for which errors.Is(err, [ErrNotFound]) reports true.
	// Object must be safe to call concurrently from multiple goroutines.
	Object(ctx context.Context, path Path) (Object, error)
}

// An Object represents a handle to a zb store object.
// All methods on Object must be safe to call concurrently from multiple goroutines.
type Object interface {
	// WriteNAR writes the NAR serialization of the store object to w.
	WriteNAR(ctx context.Context, dst io.Writer) error
	// Trailer returns the metadata of the object.
	// The caller must not modify any fields in the returned ExportTrailer.
	Trailer() *ExportTrailer
}

// A RandomAccessStore is a [Store] that supports efficient access of store object files.
//
// StoreFS returns a filesystem of the store directory.
// The filesystem may not support listing the root directory.
// Operations in the filesystem should use the provided context if applicable.
// StoreFS must be safe to call concurrently from multiple goroutines.
type RandomAccessStore interface {
	Store
	StoreFS(ctx context.Context, dir Directory) fs.FS
}

// An Importer can receive serialized zb store objects
// in the `nix-store --export` format.
// If an Importer receives an object identical one it already has,
// it should ignore the new object and it should not return an error.
type Importer interface {
	StoreImport(ctx context.Context, r io.Reader) error
}

// BatchStore is a [Store] that can efficiently query for multiple objects
// in a single request.
// If a path is not found in the store,
// then it will not be present in the resulting list
// but ObjectBatch will not return an error.
// ObjectBatch must be safe to call concurrently from multiple goroutines.
type BatchStore interface {
	Store
	ObjectBatch(ctx context.Context, storePaths sets.Set[Path]) ([]Object, error)
}

// ObjectBatch retrieves zero or more store objects.
// If the store implements [BatchStore], then the ObjectBatch method will be used.
// Otherwise, the objects will be fetched using many calls to [Store.Object]
// with at most maxConcurrency called concurrently.
func ObjectBatch(ctx context.Context, store Store, storePaths sets.Set[Path], maxConcurrency int) ([]Object, error) {
	if maxConcurrency < 1 {
		return nil, errors.New("fetch zb store objects: non-positive concurrency")
	}
	if len(storePaths) == 0 {
		return nil, nil
	}
	if b, ok := store.(BatchStore); ok {
		return b.ObjectBatch(ctx, storePaths)
	}

	grp, grpCtx := errgroup.WithContext(ctx)
	grp.SetLimit(maxConcurrency)

	var mu sync.Mutex
	result := make([]Object, 0, len(storePaths))
	for path := range storePaths.All() {
		grp.Go(func() error {
			info, err := store.Object(grpCtx, path)
			if errors.Is(err, ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}

			mu.Lock()
			result = append(result, info)
			mu.Unlock()
			return nil
		})
	}

	err := grp.Wait()
	return result, err
}

// A RealizationFetcher lists known [Realization] values for a derivation.
// The argument to FetchRealizations is a [derivation hash].
//
// FetchRealizations must be safe to call concurrently from multiple goroutines simultaneously.
//
// [derivation hash]: https://zb.256lights.llc/binary-cache/realizations#derivation-hashes
type RealizationFetcher interface {
	FetchRealizations(ctx context.Context, derivationHash nix.Hash) (RealizationMap, error)
}

// A RealizationMap is a multi-map of [RealizationOutputReference] to [*Realization].
// The zero value is an empty map.
type RealizationMap struct {
	DerivationHash nix.Hash
	Realizations   map[string][]*Realization
}

// All returns an iterator over all the realizations in the map.
func (m RealizationMap) All() iter.Seq2[RealizationOutputReference, *Realization] {
	return func(yield func(RealizationOutputReference, *Realization) bool) {
		for outputName, slice := range m.Realizations {
			ref := RealizationOutputReference{
				DerivationHash: m.DerivationHash,
				OutputName:     outputName,
			}
			for _, v := range slice {
				if !yield(ref, v) {
					return
				}
			}
		}
	}
}

// A Realization is a known output path for a particular [RealizationOutputReference].
type Realization struct {
	OutputPath       Path                    `json:"outputPath"`
	ReferenceClasses []*ReferenceClass       `json:"referenceClasses"`
	Signatures       []*RealizationSignature `json:"signatures,omitempty"`
}

// A ReferenceClass is a mapping of referenced path to optional realization.
type ReferenceClass struct {
	Path        Path                                 `json:"path"`
	Realization Nullable[RealizationOutputReference] `json:"realization"`
}

// RealizationOutputReference is a reference to an output of an equivalence class of derivations.
// It is similar to an [OutputReference], but can refers to many derivations.
type RealizationOutputReference struct {
	DerivationHash nix.Hash `json:"derivationHash"`
	OutputName     string   `json:"outputName"`
}

// String returns the hash and the output name separated by "!".
func (ref RealizationOutputReference) String() string {
	return ref.DerivationHash.Base64() + "!" + ref.OutputName
}

// RealizationSignatureFormat is an enumeration of formats for [RealizationSignature].
type RealizationSignatureFormat string

// Known signature formats.
const (
	Ed25519SignatureFormat RealizationSignatureFormat = "ed25519"
)

// A RealizationSignature is a cryptographic signature of a [RealizationOutputReference], [Realization] tuple.
type RealizationSignature struct {
	Format    RealizationSignatureFormat `json:"format"`
	PublicKey []byte                     `json:"publicKey,format:base64"`
	Signature []byte                     `json:"signature,format:base64"`
}

// SignRealizationWithEd25519 creates a signature for the realization
// using the Ed25519 signature algorithm.
func SignRealizationWithEd25519(ref RealizationOutputReference, r *Realization, key ed25519.PrivateKey) (*RealizationSignature, error) {
	v, err := marshalRealizationForSignature(ref, r)
	if err != nil {
		return nil, fmt.Errorf("sign realization: %v", err)
	}
	sig := ed25519.Sign(key, v)
	return &RealizationSignature{
		Format:    Ed25519SignatureFormat,
		PublicKey: key.Public().(ed25519.PublicKey),
		Signature: sig,
	}, nil
}

// VerifyRealizationSignature verifies that the signature for the realization is valid.
func VerifyRealizationSignature(ref RealizationOutputReference, r *Realization, sig *RealizationSignature) error {
	switch sig.Format {
	case Ed25519SignatureFormat:
		if got, want := len(sig.PublicKey), ed25519.PublicKeySize; got != want {
			return fmt.Errorf("verify realization signature: ed25519 public key is the wrong size (%d instead of %d bytes)", got, want)
		}
		v, err := marshalRealizationForSignature(ref, r)
		if err != nil {
			return fmt.Errorf("verify realization signature: %v", err)
		}
		if !ed25519.Verify(ed25519.PublicKey(sig.PublicKey), v, sig.Signature) {
			return fmt.Errorf("verify realization signature: ed25519 signature does not match")
		}
		return nil
	default:
		return fmt.Errorf("verify realization signature: unsupported format %q", sig.Format)
	}
}

type realizationForSignature struct {
	DerivationHash   nix.Hash          `json:"derivationHash"`
	OutputName       string            `json:"outputName"`
	OutputPath       Path              `json:"outputPath"`
	ReferenceClasses []*ReferenceClass `json:"referenceClasses"`
}

// marshalRealizationForSignature marshals a realization using the [JSON Canonicalization Scheme].
//
// [JSON Canonicalization Scheme]: https://datatracker.ietf.org/doc/html/rfc8785
func marshalRealizationForSignature(ref RealizationOutputReference, r *Realization) (jsontext.Value, error) {
	rsig := &realizationForSignature{
		DerivationHash:   ref.DerivationHash,
		OutputName:       ref.OutputName,
		OutputPath:       r.OutputPath,
		ReferenceClasses: r.ReferenceClasses,
	}
	if !slices.IsSortedFunc(rsig.ReferenceClasses, compareReferenceClassesForSignature) {
		rsig.ReferenceClasses = slices.Clone(rsig.ReferenceClasses)
		slices.SortFunc(rsig.ReferenceClasses, compareReferenceClassesForSignature)
	}
	firstPass, err := jsonv2.Marshal(
		rsig,
		jsonv2.WithMarshalers(jsonv2.MarshalToFunc(MarshalHashJSONTo)),
	)
	if err != nil {
		return nil, fmt.Errorf("marshal realization: %v", err)
	}
	canonicalOutput := jsontext.Value(firstPass)
	if err := canonicalOutput.Canonicalize(); err != nil {
		return nil, fmt.Errorf("marshal realization: %v", err)
	}
	return canonicalOutput, nil
}

// MarshalHashJSONTo is a [jsonv2.MarshalToFunc] for [nix.Hash]
// that encodes the hash as a JSON object in the [realization format].
//
// [realization format]: https://zb.256lights.llc/binary-cache/realizations
func MarshalHashJSONTo(enc *jsontext.Encoder, hash nix.Hash) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return fmt.Errorf("marshal hash: %v", err)
	}
	if err := enc.WriteToken(jsontext.String("algorithm")); err != nil {
		return fmt.Errorf("marshal hash: %v", err)
	}
	if err := enc.WriteToken(jsontext.String(hash.Type().String())); err != nil {
		return fmt.Errorf("marshal hash: %v", err)
	}
	if err := enc.WriteToken(jsontext.String("digest")); err != nil {
		return fmt.Errorf("marshal hash: %v", err)
	}
	if err := enc.WriteToken(jsontext.String(hash.RawBase64())); err != nil {
		return fmt.Errorf("marshal hash: %v", err)
	}
	if err := enc.WriteToken(jsontext.EndObject); err != nil {
		return fmt.Errorf("marshal hash: %v", err)
	}
	return nil
}

// UnmarshalHashJSONFrom is a [jsonv2.UnmarshalFromFunc] for [nix.Hash]
// that decodes a JSON object in the [realization format] to a hash.
//
// [realization format]: https://zb.256lights.llc/binary-cache/realizations
func UnmarshalHashJSONFrom(dec *jsontext.Decoder, hash *nix.Hash) error {
	var parsed struct {
		Type nix.HashType `json:"algorithm"`
		Bits []byte       `json:"digest,format:base64"`
	}
	if err := jsonv2.UnmarshalDecode(dec, &parsed, jsonv2.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("unmarshal hash: %v", err)
	}
	if !parsed.Type.IsValid() {
		return fmt.Errorf("unmarshal hash: unsupported algorithm %q", parsed.Type)
	}
	if got, want := len(parsed.Bits), parsed.Type.Size(); got != want {
		return fmt.Errorf("unmarshal hash: digest is incorrect size (%d instead of %d) for %s",
			got, want, parsed.Type)
	}
	*hash = nix.NewHash(parsed.Type, parsed.Bits)
	return nil
}

func compareReferenceClassesForSignature(rc1, rc2 *ReferenceClass) int {
	if result := cmp.Compare(rc1.Path, rc2.Path); result != 0 {
		return result
	}
	if rc1.Realization.Valid != rc2.Realization.Valid {
		if rc1.Realization.Valid {
			return 1
		} else {
			return -1
		}
	}
	switch {
	case !rc1.Realization.Valid && !rc2.Realization.Valid:
		return 0
	case !rc1.Realization.Valid && rc2.Realization.Valid:
		return -1
	case rc1.Realization.Valid && !rc2.Realization.Valid:
		return 1
	}
	return cmp.Or(
		cmp.Compare(
			rc1.Realization.X.DerivationHash.Type().String(),
			rc2.Realization.X.DerivationHash.Type().String(),
		),
		cmp.Compare(
			rc1.Realization.X.DerivationHash.RawBase64(),
			rc2.Realization.X.DerivationHash.RawBase64(),
		),
		cmp.Compare(
			rc1.Realization.X.OutputName,
			rc2.Realization.X.OutputName,
		),
	)
}

// ErrNotFound is the error returned by various [Store] methods
// when a store object does not exist.
var ErrNotFound = errors.New("zb store object not found")
