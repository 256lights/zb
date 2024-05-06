// Copyright 2023 Ross Light
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of
// this software and associated documentation files (the “Software”), to deal in
// the Software without restriction, including without limitation the rights to
// use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
// the Software, and to permit persons to whom the Software is furnished to do so,
// subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED “AS IS”, WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
// FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
// COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
// IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
// CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
//
// SPDX-License-Identifier: MIT

// Package bufseek provides a buffered [io.Reader] that also implements [io.Seeker].
package bufseek

import (
	"errors"
	"fmt"
	"io"
)

const defaultBufSize = 4096
const maxConsecutiveEmptyReads = 100

// Reader implements buffering for an [io.ReadSeeker] object.
type Reader struct {
	buf  []byte
	rd   io.ReadSeeker
	r, w int
	err  error
	// pos is the stream position of the beginning of buf.
	pos int64
}

// NewReaderSize returns a new [Reader]
// whose buffer has at least the specified size.
// If the argument [io.Reader] is already a *Reader or *ReadWriter with large enough size,
// it returns the underlying *Reader.
func NewReaderSize(rd io.ReadSeeker, size int) *Reader {
	switch b := rd.(type) {
	case *Reader:
		if len(b.buf) >= size {
			return b
		}
	case *ReadWriter:
		if len(b.r.buf) >= size {
			return b.r
		}
	}
	size = max(size, 16)
	return &Reader{
		buf: make([]byte, size),
		rd:  rd,
		pos: -1,
	}
}

// NewReader returns a new Reader whose buffer has the default size.
func NewReader(rd io.ReadSeeker) *Reader {
	return NewReaderSize(rd, defaultBufSize)
}

func (b *Reader) advance(n int) {
	if b.pos >= 0 {
		b.pos += int64(n)
	}
}

func (b *Reader) fill() {
	// Slide existing data to beginning.
	if b.r > 0 {
		copy(b.buf, b.buf[b.r:b.w])
		b.advance(b.r)
		b.w -= b.r
		b.r = 0
	}

	if b.w >= len(b.buf) {
		panic("bufseek: tried to fill full buffer")
	}

	// Read new data: try a limited number of times.
	for i := maxConsecutiveEmptyReads; i > 0; i-- {
		n, err := b.rd.Read(b.buf[b.w:])
		if n < 0 {
			panic(errNegativeRead)
		}
		b.w += n
		if err != nil {
			b.err = err
			return
		}
		if n > 0 {
			return
		}
	}
	b.err = io.ErrNoProgress
}

func (b *Reader) readErr() error {
	err := b.err
	b.err = nil
	return err
}

// ReadByte reads and returns a single byte.
// If no byte is available, returns an error.
func (b *Reader) ReadByte() (byte, error) {
	for b.r == b.w {
		if b.err != nil {
			return 0, b.readErr()
		}
		b.fill() // Buffer is empty.
	}
	c := b.buf[b.r]
	b.r++
	return c, nil
}

// Read reads data into p.
// It returns the number of bytes read into p.
// The bytes are taken from at most one Read on the underlying Reader,
// hence n may be less than len(p).
// To read exactly len(p) bytes, use io.ReadFull(b, p).
// If the underlying Reader can return a non-zero count with io.EOF,
// then this Read method can do so as well; see the [io.Reader] docs.
func (b *Reader) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		if b.Buffered() > 0 {
			return 0, nil
		}
		return 0, b.readErr()
	}
	if b.r == b.w {
		if b.err != nil {
			return 0, b.readErr()
		}
		if len(p) >= len(b.buf) {
			// Large read, empty buffer.
			// Read directly into p to avoid copy.
			n, b.err = b.rd.Read(p)
			if n < 0 {
				panic(errNegativeRead)
			}
			b.advance(b.r)
			b.advance(n)
			b.r = 0
			b.w = 0
			return n, b.readErr()
		}
		// One read.
		// Do not use b.fill, which will loop.
		b.advance(b.r)
		b.r = 0
		b.w = 0
		n, b.err = b.rd.Read(b.buf)
		if n < 0 {
			panic(errNegativeRead)
		}
		if n == 0 {
			return 0, b.readErr()
		}
		b.w += n
	}

	// Copy as much as we can.
	// Note: if the slice panics here, it is probably because
	// the underlying reader returned a bad count. See https://go.dev/issue/49795.
	n = copy(p, b.buf[b.r:b.w])
	b.r += n
	return n, nil
}

// Seek sets the offset for the next Read to offset, interpreted according to whence;
// see the [io.Seeker] docs.
func (b *Reader) Seek(offset int64, whence int) (pos int64, err error) {
	if whence == io.SeekCurrent {
		if 0 <= offset && offset <= int64(b.Buffered()) {
			if b.pos < 0 {
				pos, err := b.rd.Seek(0, io.SeekCurrent)
				if err != nil {
					return 0, err
				}
				b.pos = pos - int64(b.w)
			}
			b.r += int(offset)
			return b.pos + int64(b.r), nil
		}
		pos, err = b.rd.Seek(offset-int64(b.w), io.SeekCurrent)
	} else {
		pos, err = b.rd.Seek(offset, whence)
	}
	if err == nil {
		b.clear(pos)
	}
	return pos, err
}

func (b *Reader) clear(pos int64) {
	b.pos = pos
	b.r = 0
	b.w = 0
	b.err = nil
}

// Buffered returns the number of bytes that can be read from the current buffer.
func (b *Reader) Buffered() int { return b.w - b.r }

// ReadWriter implements buffering for an [io.ReadWriter] or an [io.ReadWriteSeeker] object.
type ReadWriter struct {
	r *Reader
	w io.Writer
}

// NewReadWriterSize returns a new [ReadWriter]
// whose buffer has at least the specified size.
// If the argument [io.ReadWriter] is already a *ReadWriter with large enough size,
// it returns the underlying *ReadWriter.
func NewReadWriterSize(rw io.ReadWriteSeeker, size int) *ReadWriter {
	if b, ok := rw.(*ReadWriter); ok && len(b.r.buf) >= size {
		return b
	}
	return &ReadWriter{
		r: NewReaderSize(rw, size),
		w: rw,
	}
}

// NewReadWriter returns a new ReadWriter that has the default size.
func NewReadWriter(rw io.ReadWriteSeeker) *ReadWriter {
	return NewReadWriterSize(rw, defaultBufSize)
}

// ReadByte reads and returns a single byte.
// If no byte is available, returns an error.
func (b *ReadWriter) ReadByte() (byte, error) {
	return b.r.ReadByte()
}

// Read reads data into p.
// It returns the number of bytes read into p.
// The bytes are taken from at most one Read on the underlying Reader,
// hence n may be less than len(p).
// To read exactly len(p) bytes, use io.ReadFull(b, p).
// If the underlying Reader can return a non-zero count with io.EOF,
// then this Read method can do so as well; see the [io.Reader] docs.
func (b *ReadWriter) Read(p []byte) (n int, err error) {
	return b.r.Read(p)
}

// Seek sets the offset for the next Read to offset, interpreted according to whence;
// see the [io.Seeker] docs.
func (b *ReadWriter) Seek(offset int64, whence int) (int64, error) {
	return b.r.Seek(offset, whence)
}

// Write writes data from p.
// It returns the number of bytes written from p
// and any error encountered that caused the write to stop early.
func (b *ReadWriter) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := b.syncWritePosition(); err != nil {
		return 0, err
	}

	n, err = b.w.Write(p)
	// If we cached a Seek position, we have certainly invalidated it.
	// Files opened for appending make the final position hard to predict,
	// so we just clear the position and recompute it as needed.
	b.r.clear(-1)
	return n, err
}

// WriteString writes data from s.
// It returns the number of bytes written from s
// and any error encountered that caused the write to stop early.
func (b *ReadWriter) WriteString(s string) (n int, err error) {
	if len(s) == 0 {
		return 0, nil
	}
	if err := b.syncWritePosition(); err != nil {
		return 0, err
	}

	n, err = io.WriteString(b.w, s)
	// Same note as in Write.
	b.r.clear(-1)
	return n, err
}

func (b *ReadWriter) syncWritePosition() error {
	if b.r.Buffered() > 0 {
		_, err := b.r.rd.Seek(-int64(b.r.Buffered()), io.SeekCurrent)
		if err != nil {
			return fmt.Errorf("bufseek: seek for write: %w", err)
		}
	}
	return nil
}

var errNegativeRead = errors.New("bufseek: reader returned negative count from Read")
