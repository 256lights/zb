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
	"context"
	"errors"
	"io"
	"io/fs"
	"sync"

	"golang.org/x/sync/errgroup"
	"zb.256lights.llc/pkg/sets"
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

// ErrNotFound is the error returned by various [Store] methods
// when a store object does not exist.
var ErrNotFound = errors.New("zb store object not found")
