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
	"bytes"
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
	"zombiezen.com/go/nix/nar"
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

// VerifyObject returns an error if the store object's content
// does not match its path or its content address.
// opts.Digest is ignored: obj.Trailer().StorePath.Digest() will always be used.
func VerifyObject(ctx context.Context, obj Object, opts *ContentAddressOptions) (err error) {
	trailer := obj.Trailer()
	defer func(path Path) {
		if err != nil {
			err = fmt.Errorf("verify %s content address: %v", path, err)
		}
	}(trailer.StorePath)

	computed, err := computeObjectAddress(ctx, obj, opts)
	if err != nil {
		return err
	}
	if !trailer.ContentAddress.Equal(computed) {
		return fmt.Errorf("%v does not match content (computed %v)", trailer.ContentAddress, computed)
	}

	dir := trailer.StorePath.Dir()
	name := trailer.StorePath.Name()
	storeRefs := MakeReferences(trailer.StorePath, &trailer.References)
	computedPath, err := FixedCAOutputPath(dir, name, computed, storeRefs)
	if err != nil {
		return err
	}
	if trailer.StorePath != computedPath {
		return fmt.Errorf("does not match computed path %s", computedPath)
	}
	return nil
}

func computeObjectAddress(ctx context.Context, obj Object, opts *ContentAddressOptions) (ContentAddress, error) {
	trailer := obj.Trailer()
	storeRefs := MakeReferences(trailer.StorePath, &trailer.References)
	if err := ValidateContentAddress(trailer.ContentAddress, storeRefs); err != nil {
		return ContentAddress{}, err
	}

	switch {
	case IsSourceContentAddress(trailer.ContentAddress) && trailer.ContentAddress.Hash().Type() == nix.SHA256:
		pr, pw := io.Pipe()
		done := make(chan struct{})
		go func() {
			defer close(done)
			err := obj.WriteNAR(ctx, pw)
			pw.CloseWithError(err)
		}()
		defer func() {
			pr.Close()
			<-done
		}()

		var digest string
		if storeRefs.Self {
			digest = trailer.StorePath.Digest()
		}
		opts = contentAddressOptionsWithDigest(opts, digest)
		computed, _, err := SourceSHA256ContentAddress(pr, opts)
		if err != nil {
			return ContentAddress{}, err
		}
		return computed, nil
	case IsSourceContentAddress(trailer.ContentAddress):
		// Future-proofing in case we add new algorithms but don't update backends.
		return ContentAddress{}, fmt.Errorf("unsupported source content address %v", trailer.ContentAddress.Hash().Type())
	case trailer.ContentAddress.IsRecursiveFile():
		h := nix.NewHasher(trailer.ContentAddress.Hash().Type())
		if err := obj.WriteNAR(ctx, h); err != nil {
			return ContentAddress{}, err
		}
		return nix.RecursiveFileContentAddress(h.SumHash()), nil
	default:
		pr, pw := io.Pipe()
		done := make(chan struct{})
		go func() {
			defer close(done)
			err := obj.WriteNAR(ctx, pw)
			pw.CloseWithError(err)
		}()
		defer func() {
			pr.Close()
			<-done
		}()

		nr := nar.NewReader(pr)
		hdr, err := nr.Next()
		if err != nil {
			return ContentAddress{}, err
		}
		if !hdr.Mode.IsRegular() {
			return ContentAddress{}, fmt.Errorf("not a flat file")
		}
		if hdr.Mode&0o111 != 0 {
			return ContentAddress{}, fmt.Errorf("must not be executable")
		}
		h := nix.NewHasher(trailer.ContentAddress.Hash().Type())
		if _, err := io.Copy(h, nr); err != nil {
			return ContentAddress{}, err
		}
		var computed ContentAddress
		if trailer.ContentAddress.IsText() {
			computed = nix.TextContentAddress(h.SumHash())
		} else {
			computed = nix.FlatFileContentAddress(h.SumHash())
		}
		if _, err := nr.Next(); err == nil {
			return ContentAddress{}, fmt.Errorf("more than a single file (bug in NAR reader?)")
		} else if err != io.EOF {
			return ContentAddress{}, err
		}
		return computed, nil
	}
}

func contentAddressOptionsWithDigest(opts *ContentAddressOptions, wantDigest string) *ContentAddressOptions {
	if opts == nil && wantDigest == "" || opts != nil && opts.Digest == wantDigest {
		return opts
	}
	if opts == nil {
		opts = new(ContentAddressOptions)
	} else {
		opts = new(*opts)
	}
	opts.Digest = wantDigest
	return opts
}

// A RandomAccessStore is a [Store] that supports efficient access of store object files.
//
// StoreFS returns a filesystem of the store directory.
// The filesystem must support listing object directories,
// but may not support listing the root (store) directory.
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
// FetchRealizations may return a non-empty [RealizationMap] in addition to an error.
//
// FetchRealizations must be safe to call concurrently from multiple goroutines simultaneously.
//
// [derivation hash]: https://zb.256lights.llc/binary-cache/realizations#derivation-hashes
type RealizationFetcher interface {
	FetchRealizations(ctx context.Context, derivationHash nix.Hash) (RealizationMap, error)
}

// A RealizationMap is a multi-map of [RealizationOutputReference] to [*Realization].
// The zero value is an empty map.
// RealizationMap is equivalent to a [realization document].
//
// [realization document]: https://zb.256lights.llc/binary-cache/realizations
type RealizationMap struct {
	DerivationHash nix.Hash                  `json:"derivationHash"`
	Realizations   map[string][]*Realization `json:"realizations"`
}

// IsEmpty reports whether m is an empty map.
func (m RealizationMap) IsEmpty() bool {
	for _, slice := range m.Realizations {
		if len(slice) > 0 {
			return false
		}
	}
	return true
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

// Compact deduplicates [*Realization] entries in m.
func (m RealizationMap) Compact() {
	if m.Realizations == nil {
		return
	}
	for outputName, realizations := range m.Realizations {
		newRealizations := realizations[:0]
		for _, r := range realizations {
			newRealizations = appendRealization(newRealizations, r)
		}
		clear(realizations[len(newRealizations):])
		m.Realizations[outputName] = newRealizations
	}
}

// Merge updates m with realizations from src.
func (m *RealizationMap) Merge(src RealizationMap) error {
	if src.IsEmpty() {
		return nil
	}
	if !src.DerivationHash.Equal(m.DerivationHash) {
		return fmt.Errorf("mismatched hash %v", src.DerivationHash)
	}
	if m.Realizations == nil {
		m.Realizations = src.Realizations
		return nil
	}
	for outputName, realizations := range src.Realizations {
		if len(realizations) == 0 {
			continue
		}
		if m.Realizations == nil {
			m.Realizations = make(map[string][]*Realization)
		}
		newRealizations := m.Realizations[outputName]
		for _, r := range realizations {
			newRealizations = appendRealization(newRealizations, r)
		}
		m.Realizations[outputName] = newRealizations
	}
	return nil
}

// appendRealization joins r with realizations and returns the resulting slice.
// If another realization in the slice has the same output path and reference classes as r,
// unique r.Signatures will be appended to the first such realization in the slice.
func appendRealization(realizations []*Realization, r *Realization) []*Realization {
	i := slices.IndexFunc(realizations, func(r2 *Realization) bool {
		return realizationKeysEqual(r, r2)
	})
	if i == -1 {
		return append(realizations, r)
	}
	for _, sig := range r.Signatures {
		found := slices.ContainsFunc(realizations[i].Signatures, func(other *RealizationSignature) bool {
			return realizationSignaturesEqual(sig, other)
		})
		if !found {
			realizations[i].Signatures = append(realizations[i].Signatures, sig)
		}
	}
	return realizations
}

// A Realization is a known output path for a particular [RealizationOutputReference].
type Realization struct {
	OutputPath       Path                    `json:"outputPath"`
	ReferenceClasses []*ReferenceClass       `json:"referenceClasses"`
	Signatures       []*RealizationSignature `json:"signatures,omitempty"`
}

func realizationKeysEqual(r1, r2 *Realization) bool {
	if r1.OutputPath != r2.OutputPath || len(r1.ReferenceClasses) != len(r2.ReferenceClasses) {
		return false
	}
	rc1 := slices.Clone(r1.ReferenceClasses)
	slices.SortFunc(rc1, compareReferenceClasses)
	rc2 := slices.Clone(r2.ReferenceClasses)
	slices.SortFunc(rc2, compareReferenceClasses)
	for i := range rc1 {
		if compareReferenceClasses(rc1[i], rc2[i]) != 0 {
			return false
		}
	}
	return true
}

// A ReferenceClass is a mapping of referenced path to optional realization.
type ReferenceClass struct {
	Path        Path                                 `json:"path"`
	Realization Nullable[RealizationOutputReference] `json:"realization"`
}

func compareReferenceClasses(rc1, rc2 *ReferenceClass) int {
	if result := cmp.Compare(rc1.Path, rc2.Path); result != 0 {
		return result
	}
	switch {
	case !rc1.Realization.Valid && !rc2.Realization.Valid:
		return 0
	case !rc1.Realization.Valid && rc2.Realization.Valid:
		return -1
	case rc1.Realization.Valid && !rc2.Realization.Valid:
		return 1
	}
	ref1 := rc1.Realization.X
	ref2 := rc2.Realization.X
	if result := cmp.Compare(ref1.DerivationHash.Type(), ref2.DerivationHash.Type()); result != 0 {
		return result
	}
	return cmp.Or(
		bytes.Compare(ref1.DerivationHash.Bytes(nil), ref2.DerivationHash.Bytes(nil)),
		cmp.Compare(ref1.OutputName, ref2.OutputName),
	)
}

// RealizationOutputReference is a reference to an output of an equivalence class of derivations.
// It is similar to an [OutputReference], but can refers to many derivations.
type RealizationOutputReference struct {
	DerivationHash nix.Hash `json:"derivationHash"`
	OutputName     string   `json:"outputName"`
}

// IsZero reports whether ref is the zero value.
func (ref RealizationOutputReference) IsZero() bool {
	return ref.DerivationHash.IsZero() && ref.OutputName == ""
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

// RealizationPublicKey stores a public key used for a [RealizationSignature].
type RealizationPublicKey struct {
	Format RealizationSignatureFormat `json:"format"`
	Data   []byte                     `json:"publicKey,format:base64"`
}

// Equal reports whether pub and other are equal.
func (pub *RealizationPublicKey) Equal(other *RealizationPublicKey) bool {
	switch {
	case (pub != nil) != (other != nil):
		return false
	case pub == nil && other == nil:
		return true
	default:
		return pub.Format == other.Format && bytes.Equal(pub.Data, other.Data)
	}
}

// A RealizationSignature is a cryptographic signature of a [RealizationOutputReference], [Realization] tuple.
type RealizationSignature struct {
	PublicKey RealizationPublicKey `json:",inline"`
	Signature []byte               `json:"signature,format:base64"`
}

func realizationSignaturesEqual(sig1, sig2 *RealizationSignature) bool {
	return sig1.PublicKey.Equal(&sig2.PublicKey) && bytes.Equal(sig1.Signature, sig2.Signature)
}

// SignRealizationWithEd25519 creates a signature for the realization
// using the Ed25519 signature algorithm.
func SignRealizationWithEd25519(ref RealizationOutputReference, r *Realization, key ed25519.PrivateKey) (*RealizationSignature, error) {
	v, err := marshalRealizationForSignature(ref, r)
	if err != nil {
		return nil, fmt.Errorf("sign realization %v: %v", ref, err)
	}
	sig := ed25519.Sign(key, v)
	return &RealizationSignature{
		PublicKey: RealizationPublicKey{
			Format: Ed25519SignatureFormat,
			Data:   key.Public().(ed25519.PublicKey),
		},
		Signature: sig,
	}, nil
}

// VerifyRealizationSignature verifies that the signature for the realization is valid.
func VerifyRealizationSignature(ref RealizationOutputReference, r *Realization, sig *RealizationSignature) error {
	switch sig.PublicKey.Format {
	case Ed25519SignatureFormat:
		if got, want := len(sig.PublicKey.Data), ed25519.PublicKeySize; got != want {
			return fmt.Errorf("verify realization signature: ed25519 public key is the wrong size (%d instead of %d bytes)", got, want)
		}
		v, err := marshalRealizationForSignature(ref, r)
		if err != nil {
			return fmt.Errorf("verify realization signature: %v", err)
		}
		if !ed25519.Verify(ed25519.PublicKey(sig.PublicKey.Data), v, sig.Signature) {
			return fmt.Errorf("verify realization signature: ed25519 signature does not match")
		}
		return nil
	default:
		return fmt.Errorf("verify realization signature: unsupported format %q", sig.PublicKey.Format)
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
		Type string `json:"algorithm"`
		Bits []byte `json:"digest,format:base64"`
	}
	if err := jsonv2.UnmarshalDecode(dec, &parsed, jsonv2.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("unmarshal hash: %v", err)
	}
	ht, err := nix.ParseHashType(parsed.Type)
	if err != nil {
		return fmt.Errorf("unmarshal hash: %v", err)
	}
	if got, want := len(parsed.Bits), ht.Size(); got != want {
		return fmt.Errorf("unmarshal hash: digest is incorrect size (%d instead of %d) for %s",
			got, want, parsed.Type)
	}
	*hash = nix.NewHash(ht, parsed.Bits)
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
