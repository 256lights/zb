// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package storetest provides utilities for interacting with the zb store in tests.
package storetest

import (
	"bytes"
	"context"
	"io"
	"io/fs"

	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
)

// NewFlatFile creates a fixed-hash flat file.
func NewFlatFile(dir zbstore.Directory, name string, data []byte, ht nix.HashType) *zbstore.Blob {
	h := nix.NewHasher(ht)
	h.Write(data)
	ca := nix.FlatFileContentAddress(h.SumHash())
	return newFile(dir, name, data, ca, nil)
}

// NewText creates a text file (e.g. a ".drv" file).
func NewText(dir zbstore.Directory, name string, data []byte, refs *sets.Sorted[zbstore.Path]) *zbstore.Blob {
	h := nix.NewHasher(nix.SHA256)
	h.Write(data)
	ca := nix.TextContentAddress(h.SumHash())
	trimmedRefs := trimRefs(data, zbstore.References{
		Others: *refs.Clone(),
	})
	return newFile(dir, name, data, ca, &trimmedRefs.Others)
}

func newFile(dir zbstore.Directory, name string, data []byte, ca zbstore.ContentAddress, refs *sets.Sorted[zbstore.Path]) *zbstore.Blob {
	blob := &zbstore.Blob{
		ExportTrailer: zbstore.ExportTrailer{
			ContentAddress: ca,
			References:     *refs.Clone(),
		},
	}
	var err error
	blob.StorePath, err = zbstore.FixedCAOutputPath(dir, name, ca, zbstore.References{Others: blob.References})
	if err != nil {
		panic(err)
	}
	buf := new(bytes.Buffer)
	if err := SingleFileNAR(buf, data); err != nil {
		panic(err)
	}
	blob.NAR = buf.Bytes()
	return blob
}

// ReadFile reads the content of a file inside the object.
func ReadFile(ctx context.Context, object zbstore.Object, name string) ([]byte, error) {
	pr, pw := io.Pipe()
	defer pr.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := object.WriteNAR(ctx, pw)
		pw.CloseWithError(err)
	}()

	nr := nar.NewReader(pr)
	for {
		hdr, err := nr.Next()
		if err == io.EOF {
			return nil, &fs.PathError{
				Op:   "read",
				Path: name,
				Err:  fs.ErrNotExist,
			}
		}
		if err != nil {
			return nil, err
		}
		if hdr.Path == name {
			return io.ReadAll(nr)
		}
	}
}

// SingleFileNAR writes a single non-executable file NAR to the given writer
// with the given file contents.
func SingleFileNAR(w io.Writer, data []byte) error {
	nw := nar.NewWriter(w)
	if err := nw.WriteHeader(&nar.Header{Size: int64(len(data))}); err != nil {
		return err
	}
	if _, err := nw.Write(data); err != nil {
		return err
	}
	if err := nw.Close(); err != nil {
		return err
	}
	return nil
}

// BlobReceiver implements [zbstore.NARReceiver]
// by saving each object as a [*zbstore.Blob].
// The zero value is ready to use.
type BlobReceiver struct {
	Blobs []*zbstore.Blob
}

// Write appends the byte slice to the last blob in r.Blobs.
// If the last blob in r.Blobs already has a store path set,
// then Write appends the byte slice to a new blob.
func (r *BlobReceiver) Write(p []byte) (int, error) {
	blob := r.writeBlob()
	blob.NAR = append(blob.NAR, p...)
	return len(p), nil
}

// ReceiveNAR copies the export trailer to the last blob in r.Blobs.
// If the last blob in r.Blobs already has a store path set,
// then ReceiveNAR copies the export trailer to a new blob.
func (r *BlobReceiver) ReceiveNAR(t *zbstore.ExportTrailer) {
	dst := r.writeBlob()
	dst.ExportTrailer = *t
	dst.References = *dst.References.Clone()
}

func (r *BlobReceiver) writeBlob() *zbstore.Blob {
	if len(r.Blobs) == 0 || r.Blobs[len(r.Blobs)-1].StorePath != "" {
		r.Blobs = append(r.Blobs, new(zbstore.Blob))
	}
	return r.Blobs[len(r.Blobs)-1]
}

func trimRefs(data []byte, refs zbstore.References) zbstore.References {
	firstMissing := -1
	for i, ref := range refs.Others.All() {
		if !bytes.Contains(data, []byte(ref.Digest())) {
			firstMissing = i
			break
		}
	}
	if firstMissing == -1 {
		return refs
	}

	newRefs := zbstore.References{
		Self: refs.Self,
	}
	newRefs.Others.Grow(refs.Others.Len() - 1)
	for i, ref := range refs.Others.All() {
		if i == firstMissing {
			continue
		}
		if i < firstMissing || bytes.Contains(data, []byte(ref.Digest())) {
			newRefs.Others.Add(ref)
		}
	}
	return newRefs
}
