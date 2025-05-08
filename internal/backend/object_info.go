// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package backend

import (
	"bytes"
	"context"
	"encoding"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"

	"zb.256lights.llc/pkg/internal/zbstorerpc"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
)

// ObjectInfo is the full metadata of an object in the backend.
// Conceptually, it is a tuple of a [zbstore.Path] and a [zbstorerpc.ObjectInfo].
// However, it is is more suitable as an in-memory representation.
type ObjectInfo struct {
	// StorePath is the absolute path of this store object
	// (e.g. "/opt/zb/store/s66mzxpvicwk07gjbjfw9izjfa797vsw-hello-2.12.1").
	StorePath zbstore.Path
	// NARHash is the hash of the store object as an uncompressed .nar file.
	NARHash nix.Hash
	// NARSize is the size of the decompressed .nar file in bytes.
	NARSize int64
	// References is the set of store objects that this store object references.
	References sets.Sorted[zbstore.Path]
	// CA is a content-addressability assertion.
	CA zbstore.ContentAddress
}

var _ interface {
	encoding.TextAppender
	encoding.TextMarshaler
	encoding.TextUnmarshaler
} = (*ObjectInfo)(nil)

// NewObjectInfo constructs a new [ObjectInfo]
// from a [zbstore.Path] and a [zbstorerpc.ObjectInfo].
func NewObjectInfo(path zbstore.Path, info *zbstorerpc.ObjectInfo) *ObjectInfo {
	return &ObjectInfo{
		StorePath:  path,
		NARHash:    info.NARHash,
		NARSize:    info.NARSize,
		References: *sets.NewSorted(info.References...),
		CA:         info.CA,
	}
}

// ToRPC converts info to a [*zbstorerpc.ObjectInfo] value.
func (info *ObjectInfo) ToRPC() *zbstorerpc.ObjectInfo {
	return &zbstorerpc.ObjectInfo{
		NARHash: info.NARHash,
		NARSize: info.NARSize,
		CA:      info.CA,
		// Don't send null for the array.
		References: slices.AppendSeq([]zbstore.Path{}, info.References.Values()),
	}
}

// ToExportTrailer converts info to a [*zbstore.ExportTrailer] value.
func (info *ObjectInfo) ToExportTrailer() *zbstore.ExportTrailer {
	return &zbstore.ExportTrailer{
		StorePath:      info.StorePath,
		References:     *info.References.Clone(),
		ContentAddress: info.CA,
	}
}

// AppendText implements [encoding.TextAppender]
// by appending a condensed version of a .narinfo file to dst.
// Any zero values are omitted except for store path.
func (info *ObjectInfo) AppendText(dst []byte) ([]byte, error) {
	dst = append(dst, "StorePath: "...)
	dst = append(dst, info.StorePath...)
	if !info.NARHash.IsZero() {
		dst = append(dst, "\nNarHash: "...)
		dst = append(dst, info.NARHash.Base32()...)
	}
	if info.NARSize > 0 {
		dst = append(dst, "\nNarSize: "...)
		dst = strconv.AppendInt(dst, info.NARSize, 10)
	}
	if info.References.Len() > 0 {
		dst = append(dst, "\nReferences:"...)
		for ref := range info.References.Values() {
			dst = append(dst, ' ')
			dst = append(dst, ref.Base()...)
		}
	}
	if !info.CA.IsZero() {
		dst = append(dst, "\nCA: "...)
		dst = append(dst, info.CA.String()...)
	}
	dst = append(dst, '\n')
	return dst, nil
}

// MarshalText implements [encoding.TextMarshaler]
// by calling info.AppendText(nil).
func (info *ObjectInfo) MarshalText() ([]byte, error) {
	return info.AppendText(nil)
}

// UnmarshalText implements [encoding.TextUnmarshaler]
// by parsing a .narinfo file format.
// Unrecognized keys are ignored.
func (info *ObjectInfo) UnmarshalText(src []byte) (err error) {
	*info = ObjectInfo{}
	defer func() {
		if err != nil {
			if info.StorePath != "" {
				err = fmt.Errorf("unmarshal store object info: %s: %v", info.StorePath, err)
			} else {
				err = fmt.Errorf("unmarshal store object info: %v", err)
			}
		}
	}()

	var references []byte
	hasReferences := false
	for len(src) > 0 {
		i := bytes.IndexAny(src, ":\n")
		if i < 0 || src[i] == '\n' {
			if i < 0 {
				i = len(src)
			}
			for _, b := range src[:i] {
				if b != ' ' && b != '\t' {
					return fmt.Errorf("non-empty line without ':'")
				}
			}
			i++
			if i >= len(src) {
				break
			}
			src = src[i:]
			continue
		}
		if i+len(": ") > len(src) {
			return io.ErrUnexpectedEOF
		}
		key := string(src[:i])
		if src[i+1] != ' ' {
			return fmt.Errorf("%s: space must follow ':'", key)
		}
		src = src[i+len(": "):]

		var value []byte
		if i := bytes.IndexByte(src, '\n'); i >= 0 {
			value = src[:i]
			src = src[i+1:]
		} else {
			value = src
			src = nil
		}

		switch key {
		case "StorePath":
			if info.StorePath != "" {
				return fmt.Errorf("duplicate StorePath")
			}
			if len(value) == 0 {
				return fmt.Errorf("empty StorePath")
			}
			var err error
			info.StorePath, err = zbstore.ParsePath(string(value))
			if err != nil {
				return err
			}
		case "NarHash":
			if !info.NARHash.IsZero() {
				return fmt.Errorf("duplicate NarHash")
			}
			if err := info.NARHash.UnmarshalText(value); err != nil {
				return fmt.Errorf("NarHash: %v", err)
			}
		case "NarSize":
			if info.NARSize > 0 {
				return fmt.Errorf("duplicate NarSize")
			}
			var err error
			info.NARSize, err = strconv.ParseInt(string(value), 10, 64)
			if err != nil {
				return fmt.Errorf("NarSize: %v", err)
			}
			if info.NARSize <= 0 {
				return fmt.Errorf("NarSize is non-positive")
			}
		case "References":
			if hasReferences {
				return fmt.Errorf("duplicate References")
			}
			references = value
			hasReferences = true
		case "CA":
			if !info.CA.IsZero() {
				return fmt.Errorf("duplicate CA")
			}
			if err := info.CA.UnmarshalText(value); err != nil {
				return fmt.Errorf("CA: %v", err)
			}
		}
	}

	if info.StorePath == "" {
		return fmt.Errorf("store path empty")
	}
	if len(references) > 0 {
		info.References.Clear()
		info.References.Grow(len(references))
		for w := range bytes.FieldsSeq(references) {
			ref, err := info.StorePath.Dir().Object(string(w))
			if err != nil {
				return fmt.Errorf("References: %v", err)
			}
			info.References.Add(ref)
		}
	}

	return nil
}

func objectInfosEqual(info1, info2 *ObjectInfo) bool {
	if info1.StorePath != info2.StorePath ||
		info1.NARSize != info2.NARSize ||
		!info1.NARHash.Equal(info2.NARHash) ||
		!info1.CA.Equal(info2.CA) ||
		info1.References.Len() != info2.References.Len() {
		return false
	}
	for i, ref := range info1.References.All() {
		if info2.References.At(i) != ref {
			return false
		}
	}
	return true
}

// Register adds the info for a store object that is already present in the store directory
// to the store's database.
// If the store path is already in the database
// and the information matches what is already there,
// then Register is a no-op and returns nil.
func (s *Server) Register(ctx context.Context, info *ObjectInfo) error {
	if info.CA.IsZero() {
		return fmt.Errorf("register %s: missing content address (CA) assertion", info.StorePath)
	}

	unlock, err := s.writing.lock(ctx, info.StorePath)
	if err != nil {
		return fmt.Errorf("register %s: %v", info.StorePath, err)
	}
	realPath := s.realPath(info.StorePath)
	_, statError := os.Lstat(realPath)
	unlock()
	if statError != nil {
		return fmt.Errorf("register %s: %v", info.StorePath, statError)
	}

	conn, err := s.db.Get(ctx)
	if err != nil {
		return err
	}
	defer s.db.Put(conn)

	existingInfo, err := pathInfo(conn, info.StorePath)
	if err == nil {
		if !objectInfosEqual(info, existingInfo) {
			return fmt.Errorf("register %s: does not match existing data", info.StorePath)
		}
		return nil
	}
	if !errors.Is(err, errObjectNotExist) {
		return fmt.Errorf("register %s: %v", info.StorePath, err)
	}

	pr, pw := io.Pipe()
	done := make(chan struct{})
	wc := new(writeCounter)
	hasher := nix.NewHasher(info.NARHash.Type())
	go func() {
		err := nar.DumpPath(io.MultiWriter(wc, hasher, pw), realPath)
		pw.CloseWithError(err)
		close(done)
	}()
	_, err = verifyContentAddress(info.StorePath, pr, &info.References, info.CA)
	pr.Close()
	<-done
	if err != nil {
		return fmt.Errorf("register %s: %v", info.StorePath, err)
	}

	// TODO(maybe): Is it important to validate these fields?
	// As long as we know the content address,
	// these two fields are computed.
	if want := int64(*wc); want != info.NARSize {
		return fmt.Errorf("register %s: nar size %d does not match %d from filesystem", info.StorePath, info.NARSize, want)
	}
	if want := hasher.SumHash(); !want.Equal(info.NARHash) {
		return fmt.Errorf("register %s: nar hash %v does not match %v from filesystem", info.StorePath, info.NARHash, want)
	}

	if err := insertObject(ctx, conn, info); err != nil {
		return fmt.Errorf("register %s: %v", info.StorePath, err)
	}
	return nil
}
