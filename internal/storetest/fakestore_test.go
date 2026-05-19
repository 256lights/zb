// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package storetest

import (
	"bytes"
	"errors"
	"testing"

	"zb.256lights.llc/pkg/internal/testcontext"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
)

func TestEmptyStore(t *testing.T) {
	ctx := testcontext.New(t)

	path, err := zbstore.DefaultUnixDirectory.Object("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	store := new(Store)

	if _, err := store.Object(ctx, path); !errors.Is(err, zbstore.ErrNotFound) {
		t.Errorf("new(Store).Object(ctx, %s) = _, %v; want <nil>, %v", path, err, zbstore.ErrNotFound)
	}
}

func TestStoreImport(t *testing.T) {
	ctx := testcontext.New(t)

	narContent := new(bytes.Buffer)
	if err := SingleFileNAR(narContent, []byte("Hello, World!\n")); err != nil {
		t.Fatal(err)
	}
	exportBuffer := new(bytes.Buffer)
	exp := zbstore.NewExportWriter(exportBuffer)
	path, ca, err := ExportSourceNAR(exp, narContent.Bytes(), SourceExportOptions{
		Name:      "hello.txt",
		Directory: zbstore.DefaultUnixDirectory,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := exp.Close(); err != nil {
		t.Fatal(err)
	}

	store := new(Store)
	if err := store.StoreImport(ctx, exportBuffer); err != nil {
		t.Error(err)
	}

	t.Run("Object", func(t *testing.T) {
		obj, err := store.Object(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		trailer := obj.Trailer()
		if trailer.StorePath != path {
			t.Errorf("obj.Trailer().StorePath = %q; want %q", trailer.StorePath, path)
		}
		if !trailer.ContentAddress.Equal(ca) {
			t.Errorf("obj.Trailer().ContentAddress = %v; want %v", trailer.ContentAddress, ca)
		}

		got := new(bytes.Buffer)
		if err := obj.WriteNAR(ctx, got); err != nil {
			t.Error("obj.WriteNAR:", err)
		}
		if !bytes.Equal(got.Bytes(), narContent.Bytes()) {
			t.Error("NAR content does not match")
		}
	})

	t.Run("ObjectBatch", func(t *testing.T) {
		batch, err := store.ObjectBatch(ctx, sets.New(path))
		if err != nil {
			t.Error("ObjectBatch:", err)
		}
		if len(batch) != 1 {
			t.Errorf("len(batch) == %d; want 1", len(batch))
		}

		obj := batch[0]
		trailer := obj.Trailer()
		if trailer.StorePath != path {
			t.Errorf("batch[0].Trailer().StorePath = %q; want %q", trailer.StorePath, path)
		}
		if !trailer.ContentAddress.Equal(ca) {
			t.Errorf("batch[0].Trailer().ContentAddress = %v; want %v", trailer.ContentAddress, ca)
		}

		got := new(bytes.Buffer)
		if err := obj.WriteNAR(ctx, got); err != nil {
			t.Error("batch[0].WriteNAR:", err)
		}
		if !bytes.Equal(got.Bytes(), narContent.Bytes()) {
			t.Error("NAR content does not match")
		}
	})
}
