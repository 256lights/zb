// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package detect

import (
	"bytes"
	"io"
	"slices"
)

// HashModuloReader wraps an underlying reader
// to replace any occurrences of a search string with a same-sized replacement string
// and record the offsets of those occurrences.
type HashModuloReader struct {
	r   io.Reader
	old string
	new string

	pos     int64 // number of bytes read from r before buf
	offsets []int64
	err     error // first error encountered

	buf       []byte
	processed int // number of bytes in buf that are safe to send to the caller
}

// NewHashModuloReader returns a new [HashModuloReader]
// that reads from r and replaces old with new.
// NewHashModuloReader panics if len(old) != len(new).
func NewHashModuloReader(old, new string, r io.Reader) *HashModuloReader {
	if len(old) != len(new) {
		panic("NewHashModuloReader replacment string not the same size as search string")
	}
	return &HashModuloReader{
		old: old,
		new: new,
		r:   r,
		buf: make([]byte, 0, len(old)),
	}
}

// Offsets returns a list of the offsets in the reader
// where the search string has occurred
// in ascending order.
func (hmr *HashModuloReader) Offsets() []int64 {
	return slices.Clone(hmr.offsets)
}

// Read implements [io.Reader].
// Read may read more bytes from the underlying reader
// than the number of bytes returned to the caller
// in order to determine whether the bytes are part of the reader's search string.
func (hmr *HashModuloReader) Read(p []byte) (n int, err error) {
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
		nprocessed, hmr.offsets = processHashModulo(hmr.old, hmr.new, hmr.offsets, hmr.pos, dst[:nread], hmr.err != nil)
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

func (hmr *HashModuloReader) copyBuffered(p []byte) int {
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
func processHashModulo(old, new string, offsets []int64, start int64, p []byte, eof bool) (int, []int64) {
	if old == "" {
		return len(p), offsets
	}

	nprocessed := 0
	searchEnd := len(p)
	if eof {
		// If we know this is the end of the content,
		// then there must be enough length for the modulus to be present.
		searchEnd = max(0, len(p)-len(old)+1)
	}
	for {
		i := bytes.IndexByte(p[nprocessed:searchEnd], old[0])
		if i == -1 {
			return len(p), offsets
		}
		// Go compiler optimizes out allocation in the string conversions below.
		switch pi := p[nprocessed+i:]; {
		case len(old) <= len(pi) && string(pi[1:len(old)]) == old[1:]:
			offsets = append(offsets, start+int64(nprocessed+i))
			copy(pi[:len(old)], new)
			nprocessed += i + len(old)
		case len(old) > len(pi) && string(pi[1:]) == old[1:len(pi)]:
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
