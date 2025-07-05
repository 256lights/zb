// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package zbstore

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"iter"
	"slices"

	"zb.256lights.llc/pkg/sets"
	"zombiezen.com/go/nix/nar"
)

const (
	exportObjectMarker  = "\x01\x00\x00\x00\x00\x00\x00\x00"
	exportTrailerMarker = "NIXE\x00\x00\x00\x00"
	exportEOFMarker     = "\x00\x00\x00\x00\x00\x00\x00\x00"
)

// An Exporter is a [Store] that can efficiently export objects.
// The [Export] function will use this method if available.
// StoreExport must be safe to call concurrently from multiple goroutines.
type Exporter interface {
	Store
	StoreExport(ctx context.Context, dst io.Writer, paths sets.Set[Path], opts *ExportOptions) error
}

// ExportOptions is the set of optional parameters to [Exporter.StoreExport].
type ExportOptions struct {
	// If ExcludeReferences is true, then only the paths given will be exported.
	// Otherwise, paths that are referenced by those store objects will also be included
	// and the store objects will be sorted in dependency order.
	ExcludeReferences bool

	// MaxConcurrency specifies the maximum number of objects to look up concurrently
	// if the store does not implement [BatchStore].
	// Values of MaxConcurrency less than 1 are treated as if 1 was given.
	MaxConcurrency int
}

// Export writes the store objects named by paths
// (and the transitive closure of objects they reference by default)
// to dst in `nix-store --export` format.
// If no paths are given, then an empty export will be written to dst
// without using store.
//
// If store implements [Exporter], then Export will use store.Export to perform the export.
// Otherwise, Export will query the store for all the objects needed
// and serialize them to dst.
func Export(ctx context.Context, store Store, dst io.Writer, paths sets.Set[Path], opts *ExportOptions) error {
	if len(paths) == 0 {
		_, err := io.WriteString(dst, exportEOFMarker)
		return newExportError(slices.Sorted(paths.All()), err)
	}
	if e, ok := store.(Exporter); ok {
		return e.StoreExport(ctx, dst, paths, opts)
	}

	maxConcurrency := 1
	if opts != nil && opts.MaxConcurrency > 1 {
		maxConcurrency = opts.MaxConcurrency
	}
	objects, err := orderedObjectBatch(ctx, store, slices.Values(slices.Sorted(paths.All())), maxConcurrency)
	if err != nil {
		return newExportError(slices.Sorted(paths.All()), err)
	}
	if excludeRefs := opts != nil && opts.ExcludeReferences; !excludeRefs {
		objects, err = expandClosure(ctx, store, objects, maxConcurrency)
		if err != nil {
			return newExportError(slices.Sorted(paths.All()), err)
		}
	}

	w := NewExportWriter(dst)
	for _, obj := range objects {
		if err := obj.WriteNAR(ctx, w); err != nil {
			return newExportError(slices.Sorted(paths.All()), err)
		}
		if err := w.Trailer(obj.Trailer()); err != nil {
			return newExportError(slices.Sorted(paths.All()), err)
		}
	}
	if err := w.Close(); err != nil {
		return newExportError(slices.Sorted(paths.All()), err)
	}

	return nil
}

func expandClosure(ctx context.Context, store Store, objects []Object, maxConcurrency int) ([]Object, error) {
	for {
		newObjects, err := orderedObjectBatch(ctx, store, missingReferences(objects), maxConcurrency)
		if err != nil {
			return nil, err
		}
		objects = append(objects, newObjects...)
		if len(newObjects) == 0 {
			break
		}
	}

	// Topologically sort new closure.
	for sortEnd := 0; sortEnd < len(objects); sortEnd++ {
		sorted := objects[:sortEnd]
		unsorted := objects[sortEnd:]
		i := slices.IndexFunc(unsorted, func(obj Object) bool {
			trailer := obj.Trailer()
			for ref := range trailer.References.Values() {
				if ref != trailer.StorePath && !objectsHavePath(slices.Values(sorted), ref) {
					return false
				}
			}
			return true
		})
		// Move object to front of unsorted slice.
		unsorted[0], unsorted[i] = unsorted[i], unsorted[0]
	}

	return objects, nil
}

func orderedObjectBatch(ctx context.Context, store Store, paths iter.Seq[Path], maxConcurrency int) ([]Object, error) {
	pathSet := sets.Collect(paths)
	if pathSet.Len() == 0 {
		return nil, nil
	}
	batch, err := ObjectBatch(ctx, store, pathSet, maxConcurrency)
	if err != nil {
		return nil, err
	}
	result := make([]Object, 0, pathSet.Len())
	for path := range paths {
		i := slices.IndexFunc(batch, func(obj Object) bool {
			return obj.Trailer().StorePath == path
		})
		if i < 0 {
			return nil, fmt.Errorf("store object %s: %w", path, ErrNotFound)
		}
		result = append(result, batch[i])
	}
	return result, nil
}

func missingReferences(objects []Object) iter.Seq[Path] {
	return func(yield func(Path) bool) {
		for _, obj := range objects {
			for ref := range obj.Trailer().References.Values() {
				if !objectsHavePath(slices.Values(objects), ref) {
					if !yield(ref) {
						return
					}
				}
			}
		}
	}
}

func objectsHavePath(objects iter.Seq[Object], path Path) bool {
	for obj := range objects {
		if obj.Trailer().StorePath == path {
			return true
		}
	}
	return false
}

// ExportTrailer holds metadata about a Nix store object
// used in the `nix-store --export` format.
type ExportTrailer struct {
	StorePath      Path
	References     sets.Sorted[Path]
	Deriver        Path
	ContentAddress ContentAddress
}

// An ExportWriter serializes zero or more NARs to a stream
// in `nix-store --export` format.
type ExportWriter struct {
	w          io.Writer
	trailerBuf []byte
	header     bool
	closed     bool
}

// NewExportWriter returns a new [ExportWriter] that writes to w.
// The caller is responsible for calling [ExportWriter.Close] on the returned exporter
// to finish the stream.
func NewExportWriter(w io.Writer) *ExportWriter {
	return &ExportWriter{w: w}
}

// Write writes bytes of a store object to the exporter's underlying writer.
func (ew *ExportWriter) Write(p []byte) (int, error) {
	if ew.closed {
		return 0, fmt.Errorf("write to closed exporter")
	}
	if !ew.header {
		if _, err := io.WriteString(ew.w, exportObjectMarker); err != nil {
			return 0, err
		}
		ew.header = true
	}
	return ew.w.Write(p)
}

// Trailer marks the end of a store object in the stream.
// Subsequent calls to [ExportWriter.Write] will be part of a new store object.
func (ew *ExportWriter) Trailer(t *ExportTrailer) error {
	if ew.closed {
		return fmt.Errorf("write nix store export trailer: write to closed exporter")
	}
	if !ew.header {
		return fmt.Errorf("write nix store export trailer: NAR not yet written")
	}
	ew.header = false

	ew.trailerBuf = ew.trailerBuf[:0]
	ew.trailerBuf = append(ew.trailerBuf, exportTrailerMarker...)
	ew.trailerBuf = appendNARString(ew.trailerBuf, string(t.StorePath))
	ew.trailerBuf = binary.LittleEndian.AppendUint64(ew.trailerBuf, uint64(t.References.Len()))
	for _, ref := range t.References.All() {
		ew.trailerBuf = appendNARString(ew.trailerBuf, string(ref))
	}
	ew.trailerBuf = appendNARString(ew.trailerBuf, string(t.Deriver))
	if t.ContentAddress.IsZero() {
		ew.trailerBuf = binary.LittleEndian.AppendUint64(ew.trailerBuf, 0)
	} else {
		// Nix 1.X used this field to store RSA-based signatures.
		// Nix 2.0 onwards ignore this field, so we use it to inject a content addressability assertion.
		ew.trailerBuf = binary.LittleEndian.AppendUint64(ew.trailerBuf, 1)
		ew.trailerBuf = appendNARString(ew.trailerBuf, t.ContentAddress.String())
	}

	if _, err := ew.w.Write(ew.trailerBuf); err != nil {
		return err
	}
	return nil
}

// Close writes the footer of the export to the exporter's underlying writer.
// Close returns an error if a store object has been written
// but [ExportWriter.Trailer] has not been called.
// Close does not close the underlying writer.
func (ew *ExportWriter) Close() error {
	if ew.closed {
		return fmt.Errorf("close nar exporter: exporter already closed")
	}
	if ew.header {
		return fmt.Errorf("close nar exporter: missing trailer")
	}

	_, err := io.WriteString(ew.w, exportEOFMarker)
	if err != nil {
		return err
	}
	return nil
}

// A type that implements NARReceiver processes multiple NAR files.
// After the NAR file has been written to the receiver,
// ReceiveNAR is called to provide metadata about the written NAR file.
// Subsequent writes will be for a new NAR file.
// If the Write method of a NARReceiver returns an error,
// the NARReceiver should not receive further calls.
type NARReceiver interface {
	io.Writer
	ReceiveNAR(trailer *ExportTrailer)
}

// ReceiveExport processes a stream of NARs in `nix-store --export` format,
// returning the first error encountered.
//
// ReceiveExport will not read beyond the end of the export,
// so there may still be data remaining in r after a call to ReceiveExport.
func ReceiveExport(receiver NARReceiver, r io.Reader) error {
	buf := make([]byte, len(exportObjectMarker))
	ew := &errWriter{w: receiver}
	for {
		if _, err := readFull(r, buf[:len(exportObjectMarker)]); err != nil {
			return err
		}
		if string(buf[:len(exportEOFMarker)]) == exportEOFMarker {
			return nil
		}
		if string(buf[:len(exportObjectMarker)]) != exportObjectMarker {
			return fmt.Errorf("invalid object separator %x", buf[:])
		}

		nr := nar.NewReader(io.TeeReader(r, ew))
		nr.AllowTrailingData()
		for {
			_, err := nr.Next()
			if ew.err != nil {
				// Always pass through writer errors verbatim.
				return ew.err
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
		}

		if _, err := readFull(r, buf[:len(exportTrailerMarker)]); err != nil {
			return err
		}
		if string(buf[:len(exportTrailerMarker)]) != exportTrailerMarker {
			return fmt.Errorf("invalid trailer start %x", buf[:])
		}

		t := new(ExportTrailer)
		var err error
		buf, err = readNARString(r, buf[:0])
		if err != nil {
			return fmt.Errorf("read store path: %w", err)
		}
		t.StorePath = Path(buf)

		buf = buf[:0]
		nrefs, err := readUint64(r, &buf)
		if err != nil {
			return fmt.Errorf("read references: %w", err)
		}
		if nrefs > 100_000 {
			return fmt.Errorf("read references: too many references (%d)", nrefs)
		}
		t.References.Grow(int(nrefs))
		for range nrefs {
			var err error
			buf, err = readNARString(r, buf[:0])
			if err != nil {
				return fmt.Errorf("read references: %w", err)
			}
			t.References.Add(Path(buf))
		}

		buf, err = readNARString(r, buf[:0])
		if err != nil {
			return fmt.Errorf("read deriver: %w", err)
		}
		t.Deriver = Path(buf)

		buf = buf[:0]
		x, err := readUint64(r, &buf)
		if err != nil {
			return err
		}
		switch x {
		case 0:
			// No content address assertion or signatures.
		case 1:
			buf, err = readNARString(r, buf[:0])
			if err != nil {
				return fmt.Errorf("read content address assertion: %v", err)
			}
			if err := t.ContentAddress.UnmarshalText(buf); err != nil {
				return fmt.Errorf("read content address assertion: %v", err)
			}
		default:
			return fmt.Errorf("invalid end of object marker %x", x)
		}

		receiver.ReceiveNAR(t)
	}
}

const stringAlign = 8

func appendNARString(dst []byte, s string) []byte {
	dst = binary.LittleEndian.AppendUint64(dst, uint64(len(s)))
	dst = append(dst, s...)
	if off := len(s) % stringAlign; off != 0 {
		for i := 0; i < stringAlign-off; i++ {
			dst = append(dst, 0)
		}
	}
	return dst
}

// readNARString reads a NAR-style string from r
// and appends it to the given byte slice.
// NAR strings start with an unsigned 64-bit little endian length
// and are padded to 8-byte alignment.
func readNARString(r io.Reader, buf []byte) ([]byte, error) {
	start := len(buf)
	n, err := readUint64(r, &buf)
	buf = buf[:start] // drop length from buffer
	if err != nil {
		return buf, err
	}
	if n > 4096 {
		return buf, fmt.Errorf("nar string too large (%d bytes)", n)
	}
	readSize := padStringSize(int(n))
	buf = slices.Grow(buf, readSize)
	if _, err := readFull(r, buf[start:start+readSize]); err != nil {
		return buf, err
	}
	return buf[:start+int(n)], nil
}

func readUint64(r io.Reader, buf *[]byte) (uint64, error) {
	if buf == nil {
		buf = new([]byte)
	}
	*buf = slices.Grow(*buf, 8)
	newEnd := len(*buf) + 8
	readBuf := (*buf)[len(*buf):newEnd]
	if _, err := readFull(r, readBuf); err != nil {
		return 0, err
	}
	*buf = (*buf)[:newEnd]
	return binary.LittleEndian.Uint64(readBuf), nil
}

// padStringSize returns the smallest integer >= n
// that is evenly divisible by [stringAlign].
func padStringSize(n int) int {
	return (n + stringAlign - 1) &^ (stringAlign - 1)
}

// readFull is the same as [io.ReadFull]
// except it never returns [io.EOF]:
// it will instead return [io.ErrUnexpectedEOF] if no bytes were read before EOF.
func readFull(r io.Reader, p []byte) (int, error) {
	n, err := io.ReadFull(r, p)
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}

type errWriter struct {
	w   io.Writer
	err error
}

func (ew *errWriter) Write(p []byte) (int, error) {
	if ew.err != nil {
		return 0, ew.err
	}
	var n int
	n, ew.err = ew.w.Write(p)
	return n, ew.err
}

func newExportError(paths []Path, err error) error {
	switch len(paths) {
	case 0:
		return fmt.Errorf("export store objects: %w", err)
	case 1:
		return fmt.Errorf("export %s: %w", paths[0], err)
	default:
		return fmt.Errorf("export %s (+%d more): %w", paths[0], len(paths)-1, err)
	}
}
