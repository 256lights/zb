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

package bufseek

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/iotest"
)

var _ interface {
	io.Reader
	io.ByteReader
	io.Seeker
} = (*Reader)(nil)

var _ interface {
	io.Reader
	io.Writer
	io.ByteReader
	io.Seeker
} = (*ReadWriter)(nil)

func TestRead(t *testing.T) {
	want := bytes.Repeat([]byte{
		0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
		0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0x00, 0x00,
	}, 4096/16)
	rd := NewReaderSize(bytes.NewReader(want), 20)
	if err := iotest.TestReader(rd, want); err != nil {
		t.Error(err)
	}
}

func TestReadByte(t *testing.T) {
	want := bytes.Repeat([]byte{
		0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
		0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0x00, 0x00,
	}, 4096/16)
	rd := NewReaderSize(bytes.NewReader(want), 20)

	for i, want := range want {
		c, err := rd.ReadByte()
		if err != nil {
			t.Fatal(err)
		}
		if c != want {
			t.Errorf("at offset %d, ReadByte() = %#02x, <nil>; want %#02x, <nil>", i, c, want)
		}
	}
}

func TestReadWriter(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "foo.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b := NewReadWriter(f)

	const line1 = "Hello, World!\n"
	const line2 = "second line\n"
	if n, err := io.WriteString(b, line1+line2); n != len(line1+line2) || err != nil {
		t.Errorf("io.WriteString(b, %q) = %d, %v; want %d, <nil>",
			line1+line2, n, err, len(line1+line2))
	}
	if pos, err := b.Seek(0, io.SeekStart); pos != 0 || err != nil {
		t.Errorf("b.Seek(0, io.SeekStart) = %d, %v; want 0, <nil>", pos, err)
	}
	buf := make([]byte, 2)
	if n, err := io.ReadFull(b, buf); n != len(buf) || err != nil {
		t.Errorf("io.ReadFull(...) = %d, %v; want %d, <nil>", n, err, len(buf))
	}
	if want := line1[:len(buf)]; string(buf) != want {
		t.Errorf("after io.ReadFull, buf = %q; want %q", buf, want)
	}
	offset := int64(len(line1) - len(buf))
	pos, err := b.Seek(offset, io.SeekCurrent)
	if pos != int64(len(line1)) || err != nil {
		t.Errorf("b.Seek(%d, io.SeekStart) = %d, %v; want %d, <nil>",
			offset, pos, err, len(line1))
	}
	const newLine2 = "this is a really long line that replaces the old one\n"
	if n, err := io.WriteString(b, newLine2); n != len(newLine2) || err != nil {
		t.Errorf("io.WriteString(b, %q) = %d, %v; want %d, <nil>",
			newLine2, n, err, len(newLine2))
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(f)
	if want := line1 + newLine2; string(got) != want || err != nil {
		t.Errorf("final contents = %q, %v; want %q, <nil>", got, err, want)
	}
}
