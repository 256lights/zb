// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package zbstore

import (
	"context"
	"fmt"
	"io"

	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
)

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
