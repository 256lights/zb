// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package zbstore

import (
	"bytes"
	"fmt"
	"io"

	"zombiezen.com/go/nix"
)

// A ContentAddress is a content-addressibility assertion.
type ContentAddress = nix.ContentAddress

// FixedCAOutputPath computes the path of a store object
// with the given directory, name, content address, and reference set.
func FixedCAOutputPath(dir Directory, name string, ca nix.ContentAddress, refs References) (Path, error) {
	if err := ValidateContentAddress(ca, refs); err != nil {
		return "", fmt.Errorf("compute fixed output path for %s: %v", name, err)
	}
	h := ca.Hash()
	switch {
	case ca.IsText():
		return makeStorePath(dir, "text", h, name, refs)
	case IsSourceContentAddress(ca):
		return makeStorePath(dir, "source", h, name, refs)
	default:
		h2 := nix.NewHasher(nix.SHA256)
		h2.WriteString("fixed:out:")
		h2.WriteString(methodOfContentAddress(ca).prefix())
		h2.WriteString(h.Base16())
		h2.WriteString(":")
		return makeStorePath(dir, "output:out", h2.SumHash(), name, References{})
	}
}

// ValidateContentAddress checks whether the combination of the content address
// and set of references is one that will be accepted by a zb store.
// If not, it returns an error describing the issue.
func ValidateContentAddress(ca nix.ContentAddress, refs References) error {
	htype := ca.Hash().Type()
	isFixedOutput := ca.IsFixed() && !IsSourceContentAddress(ca)
	switch {
	case ca.IsZero():
		return fmt.Errorf("null content address")
	case ca.IsText() && htype != nix.SHA256:
		return fmt.Errorf("text must be content-addressed by %v (got %v)", nix.SHA256, htype)
	case refs.Self && ca.IsText():
		return fmt.Errorf("self-references not allowed in text")
	case !refs.IsEmpty() && isFixedOutput:
		return fmt.Errorf("references not allowed in fixed output")
	default:
		return nil
	}
}

// SourceSHA256ContentAddress computes the content address of a "source" store object,
// given its temporary path digest (as given by [Path.Digest])
// and its NAR serialization.
// The digest is used to detect self-references:
// if the store object is known to not contain self-references,
// digest may be the empty string.
//
// See [IsSourceContentAddress] for an explanation of "source" store objects.
func SourceSHA256ContentAddress(digest string, sourceNAR io.Reader) (nix.ContentAddress, error) {
	h := nix.NewHasher(nix.SHA256)
	var offsets *[]int64
	if digest != "" {
		hmr := newHashModuloReader(digest, sourceNAR)
		offsets = &hmr.offsets
		sourceNAR = hmr
	}

	if _, err := io.Copy(h, sourceNAR); err != nil {
		return nix.ContentAddress{}, fmt.Errorf("compute source content address: %v", err)
	}

	// This single pipe separator differentiates this content addressing algorithm
	// from Nix's implementation as of Nix commit 2ed075ffc0f4e22f6bc6c083ef7c84e77c687605.
	// I believe it to be more correct in avoiding potential hash collisions.
	h.WriteString("|")

	if offsets != nil {
		for _, off := range *offsets {
			fmt.Fprintf(h, "|%d", off)
		}
	}
	return nix.RecursiveFileContentAddress(h.SumHash()), nil
}

// IsSourceContentAddress reports whether the given content address describes a "source" store object.
// "Source" store objects are those that are hashed by their NAR serialization
// and do not have a fixed (non-SHA-256) hash.
// This typically means source files imported using the "path" function,
// but can also mean content-addressed build artifacts.
func IsSourceContentAddress(ca nix.ContentAddress) bool {
	return ca.IsRecursiveFile() && ca.Hash().Type() == nix.SHA256
}

// hashModuloReader wraps an underlying reader
// to replace any occurrences of its modulus with zero bytes
// and record the offsets of those occurrences.
type hashModuloReader struct {
	r       io.Reader
	modulus string

	pos     int64 // number of bytes read from r before buf
	offsets []int64
	err     error // first error encountered

	buf       []byte
	processed int // number of bytes in buf that are safe to send to the caller
}

func newHashModuloReader(modulus string, r io.Reader) *hashModuloReader {
	return &hashModuloReader{
		modulus: modulus,
		r:       r,
		buf:     make([]byte, 0, len(modulus)),
	}
}

func (hmr *hashModuloReader) Read(p []byte) (n int, err error) {
	if n = hmr.copyBuffered(p); n > 0 {
		if len(hmr.buf) == 0 {
			return n, hmr.err
		}
		return n, nil
	}
	if len(p) == 0 {
		if len(hmr.buf) == 0 {
			return 0, hmr.err
		}
		return 0, nil
	}

	dst := p
	nread := len(hmr.buf)
	useInternalBuffer := len(p) < cap(hmr.buf)
	if useInternalBuffer {
		dst = hmr.buf[:cap(hmr.buf)]
	} else {
		copy(p, hmr.buf)
	}
	nprocessed := 0
	for nprocessed == 0 && hmr.err == nil {
		var nn int
		nn, hmr.err = readAtLeast1(hmr.r, dst[nread:])
		nread += nn
		nprocessed, hmr.offsets = processHashModulo(hmr.modulus, hmr.offsets, hmr.pos, dst[:nread], hmr.err != nil)
	}
	if useInternalBuffer {
		n = copy(p, dst[:nprocessed])
	} else {
		n = nprocessed
	}
	newBufLen := copy(hmr.buf[:cap(hmr.buf)], dst[n:nread])
	hmr.buf = hmr.buf[:newBufLen]
	hmr.processed = nprocessed - n
	hmr.pos += int64(nread - newBufLen)
	if newBufLen == 0 {
		return n, hmr.err
	}
	return n, nil
}

func (hmr *hashModuloReader) copyBuffered(p []byte) int {
	n := copy(p, hmr.buf[:hmr.processed])
	copy(hmr.buf, hmr.buf[n:])
	hmr.buf = hmr.buf[:len(hmr.buf)-n]
	hmr.processed -= n
	hmr.pos += int64(n)
	return n
}

// processHashModulo zeroes out any occurrences of the modulus in the given stream buffer,
// returning how many bytes of the prefix of the buffer can be returned to the caller.
// The offset of any occurrences are appended to the offsets slice.
func processHashModulo(modulus string, offsets []int64, start int64, p []byte, eof bool) (int, []int64) {
	if modulus == "" {
		return len(p), offsets
	}

	nprocessed := 0
	searchEnd := len(p)
	if eof {
		// If we know this is the end of the content,
		// then there must be enough length for the modulus to be present.
		searchEnd = max(0, len(p)-len(modulus)+1)
	}
	for {
		i := bytes.IndexByte(p[nprocessed:searchEnd], modulus[0])
		if i == -1 {
			return len(p), offsets
		}
		// Go compiler optimizes out allocation in the string conversions below.
		switch pi := p[nprocessed+i:]; {
		case len(modulus) <= len(pi) && string(pi[1:len(modulus)]) == modulus[1:]:
			offsets = append(offsets, start+int64(nprocessed+i))
			clear(pi[:len(modulus)])
			nprocessed += i + len(modulus)
		case len(modulus) > len(pi) && string(pi[1:]) == modulus[1:len(pi)]:
			// Possible match at end.
			// Because of the searchEnd limiting above,
			// we don't have to check for eof here.
			nprocessed += i
			return nprocessed, offsets
		default:
			nprocessed += i + 1
		}
	}
}

type contentAddressMethod int8

const (
	textIngestionMethod contentAddressMethod = 1 + iota
	flatFileIngestionMethod
	recursiveFileIngestionMethod
)

func methodOfContentAddress(ca nix.ContentAddress) contentAddressMethod {
	switch {
	case ca.IsText():
		return textIngestionMethod
	case ca.IsRecursiveFile():
		return recursiveFileIngestionMethod
	default:
		return flatFileIngestionMethod
	}
}

func (m contentAddressMethod) prefix() string {
	switch m {
	case textIngestionMethod:
		return "text:"
	case flatFileIngestionMethod:
		return ""
	case recursiveFileIngestionMethod:
		return "r:"
	default:
		panic("unknown content address method")
	}
}

func readAtLeast1(r io.Reader, buf []byte) (n int, err error) {
	if len(buf) == 0 {
		return 0, io.ErrShortBuffer
	}
	for i := 0; n == 0 && err == nil && i < 100; i++ {
		n, err = r.Read(buf[n:])
	}
	if n == 0 && err == nil {
		err = io.ErrNoProgress
	}
	return
}
