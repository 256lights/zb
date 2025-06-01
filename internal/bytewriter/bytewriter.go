// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

// Package bytewriter provides a buffer type that implements [io.ReadWriteSeeker].
package bytewriter

import (
	"errors"
	"io"
	"math"
)

// Buffer implements the [io.Reader], [io.WriterTo], [io.Writer], [io.Seeker],
// and [io.ByteScanner] interfaces by reading from or writing to a byte slice.
// The zero value for Buffer operates like a Buffer of an empty slice.
type Buffer struct {
	s []byte
	i int64
}

// New returns a new [Buffer] reading from and writing to b.
func New(p []byte) *Buffer {
	return &Buffer{s: p}
}

// Reset resets the [Buffer] to be reading from and writing to b.
func (b *Buffer) Reset(p []byte) {
	*b = Buffer{s: p}
}

// Size returns the length of the underlying byte slice.
func (b *Buffer) Size() int64 {
	return int64(len(b.s))
}

// Read implements the [io.Reader] interface.
func (b *Buffer) Read(p []byte) (n int, err error) {
	if b.i >= int64(len(b.s)) {
		return 0, io.EOF
	}
	n = copy(p, b.s[b.i:])
	b.i += int64(n)
	return n, nil
}

// ReadByte implements the [io.ByteReader] interface.
func (b *Buffer) ReadByte() (byte, error) {
	if b.i >= int64(len(b.s)) {
		return 0, io.EOF
	}
	bb := b.s[b.i]
	b.i++
	return bb, nil
}

// UnreadByte complements [*Buffer.ReadByte] in implementing the [io.ByteScanner] interface.
func (b *Buffer) UnreadByte() error {
	if b.i <= 0 {
		return errors.New("bytewriter.Buffer.UnreadByte: at beginning of slice")
	}
	b.i--
	return nil
}

// Write implements the [io.Writer] interface.
// If Write would extend past the underlying byte slice's capacity,
// then Write allocates a new byte slice large enough to fit the new bytes.
// Write returns an error if and only if the byte slice length would exceed an int.
// If the offset is larger than the length of the underlying byte slice,
// then the intervening bytes are zero-filled.
func (b *Buffer) Write(p []byte) (n int, err error) {
	switch {
	case b.i > int64(math.MaxInt-len(p)):
		return 0, errors.New("bytewriter.Buffer.Write: too large")
	case b.i > int64(len(b.s)):
		b.s = append(append(b.s, make([]byte, int(b.i)-len(b.s))...), p...)
	case b.i+int64(len(p)) >= int64(len(b.s)):
		b.s = append(b.s[:b.i], p...)
	default:
		copy(b.s[b.i:], p)
	}
	b.i += int64(len(p))
	return len(p), nil
}

// Seek implements the [io.Seeker] interface.
func (b *Buffer) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = b.i + offset
	case io.SeekEnd:
		abs = int64(len(b.s)) + offset
	default:
		return 0, errors.New("bytewriter.Buffer.Seek: invalid whence")
	}
	if abs < 0 {
		return 0, errors.New("bytewriter.Buffer.Seek: negative position")
	}
	b.i = abs
	return abs, nil
}

// WriteTo implements the [io.WriterTo] interface.
func (b *Buffer) WriteTo(w io.Writer) (n int64, err error) {
	if b.i >= int64(len(b.s)) {
		return 0, nil
	}
	p := b.s[b.i:]
	m, err := w.Write(p)
	if m > len(p) {
		panic("bytewriter.Buffer.WriteTo: invalid Write count")
	}
	b.i += int64(m)
	n = int64(m)
	if m != len(p) && err == nil {
		err = io.ErrShortWrite
	}
	return
}
