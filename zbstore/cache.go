// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package zbstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"zb.256lights.llc/pkg/sets"
)

// A WritableRandomAccessStore is a [RandomAccessStore]
// that can be added to via the [Importer] interface.
// After an object is imported,
// it should be available via the store's other methods.
type WritableRandomAccessStore interface {
	RandomAccessStore
	Importer
}

// A Cache is a [WritableRandomAccessStore]
// that wraps an underlying [WritableRandomAccessStore]
// and performs lookups in a fallback [Store]
// for any objects not in the wrapped store.
type Cache struct {
	Store    WritableRandomAccessStore
	Fallback Store
}

// Object returns an object from c.Store (consulted first) or c.Fallback.
// If [Object.DownloadNAR] is called on the returned object
// and the object was not in c.Store,
// then it will be imported from c.Fallback into c.Store first.
func (c *Cache) Object(ctx context.Context, path Path) (Object, error) {
	obj, err := c.Store.Object(ctx, path)
	if err == nil || !errors.Is(err, ErrNotFound) {
		return obj, err
	}
	obj, err = c.Fallback.Object(ctx, path)
	if err != nil {
		return nil, err
	}
	return &fallbackObject{
		c:       c,
		trailer: obj.Trailer(),
		mu:      make(chan struct{}, 1),
		wrapped: obj,
	}, nil
}

// StoreFS returns a [fs.FS] that represents the given store directory.
func (c *Cache) StoreFS(ctx context.Context, dir Directory) fs.FS {
	return cacheFS{ctx, c, dir}
}

// StoreImport imports the store objects serialized in r
// by calling c.Store.StoreImport(ctx, r).
func (c *Cache) StoreImport(ctx context.Context, r io.Reader) error {
	return c.Store.StoreImport(ctx, r)
}

// makeLocal copies the path from c.Fallback to c.Store.
// If the object does not exist, then errors.Is(err, ErrNotFound) reports true.
func (c *Cache) makeLocal(ctx context.Context, path Path) error {
	pr, pw := io.Pipe()
	done := make(chan struct{})

	if obj, err := c.Fallback.Object(ctx, path); err != nil {
		return err
	} else if t := obj.Trailer(); t.References.Len() == 0 ||
		t.References.Len() == 1 && t.References.At(0) == t.StorePath {
		// Special optimization: if self-contained, then import directly.
		go func() {
			defer close(done)
			w := NewExportWriter(pw)
			if err := obj.WriteNAR(ctx, w); err != nil {
				pw.CloseWithError(err)
				return
			}
			if err := w.Trailer(t); err != nil {
				pw.CloseWithError(err)
				return
			}
			if err := w.Close(); err != nil {
				pw.CloseWithError(err)
				return
			}
			pw.Close()
		}()
	} else {
		go func() {
			defer close(done)
			err := Export(ctx, c.Fallback, pw, sets.New(path), &ExportOptions{
				MaxConcurrency: 1,
			})
			pw.CloseWithError(err)
		}()
	}

	err := c.StoreImport(ctx, pr)
	pr.Close()
	<-done
	return err
}

type fallbackObject struct {
	c       *Cache
	trailer *ExportTrailer

	mu      chan struct{}
	wrapped Object
	copied  bool
}

func (obj *fallbackObject) Trailer() *ExportTrailer {
	return obj.trailer
}

func (obj *fallbackObject) WriteNAR(ctx context.Context, dst io.Writer) error {
	// Acquire mutex.
	select {
	case obj.mu <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("write %s: %w", obj.trailer.StorePath, ctx.Err())
	}

	// Make local if we haven't already.
	if !obj.copied {
		if err := obj.c.makeLocal(ctx, obj.trailer.StorePath); err != nil {
			<-obj.mu
			return err
		}
		newObject, err := obj.c.Store.Object(ctx, obj.trailer.StorePath)
		if err != nil {
			<-obj.mu
			return err
		}
		obj.wrapped = newObject
		obj.copied = true
	}

	// Release mutex then write to w.
	wrapped := obj.wrapped
	<-obj.mu
	return wrapped.WriteNAR(ctx, dst)

}

// TODO(Go 1.25): Implement ReadLinkFS.

type cacheFS struct {
	ctx context.Context
	c   *Cache
	dir Directory
}

func (fsys cacheFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  fs.ErrInvalid,
		}
	}

	// Pass through to fsys.c.Store first.
	// If it works (or there's an I/O error), return that result.
	// If the file was not found, then we have more work to do.
	storeFS := fsys.c.Store.StoreFS(fsys.ctx, fsys.dir)
	f, originalError := storeFS.Open(name)
	if originalError == nil || name == "." || !errors.Is(originalError, fs.ErrNotExist) {
		return f, originalError
	}

	// Check first whether this is a valid store object path.
	objectName := firstPathElement(name)
	path, err := fsys.dir.Object(objectName)
	if err != nil {
		return nil, originalError
	}

	// Narrow down whether the not found error is because...
	if _, err := fsys.c.Store.Object(fsys.ctx, path); err == nil {
		// ...the file does not exist inside the object.
		// Nothing further needed.
		return nil, originalError
	} else if !errors.Is(err, fs.ErrNotExist) {
		// Unfortunately, some other error occurred in the process.
		// Return that.
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  err,
		}
	}

	// We don't have the object.
	// Try downloading it.
	if err := fsys.c.makeLocal(fsys.ctx, path); err != nil {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  err,
		}
	}
	return storeFS.Open(name)
}

func firstPathElement(name string) string {
	i := strings.IndexByte(name, '/')
	if i < 0 {
		return name
	}
	return name[:i]
}
