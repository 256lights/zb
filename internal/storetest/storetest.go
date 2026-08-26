// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package storetest provides utilities for interacting with the zb store in tests.
package storetest

import "zb.256lights.llc/pkg/zbstore"

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
