// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package bytebuffer

import (
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestRead(t *testing.T) {
	tests := []string{
		"",
		"Hello, World!\n",
		strings.Repeat("01234567890abcdef", 4096/16),
	}

	for _, test := range tests {
		if err := iotest.TestReader(New([]byte(test)), []byte(test)); err != nil {
			t.Errorf("iotest.TestReader(New(%q), %q): %v", test, test, err)
		}
	}
}

func TestWrite(t *testing.T) {
	tests := []struct {
		name   string
		init   string
		offset int64
		p      string
		want   string
	}{
		{
			name: "WriteToEmpty",
			p:    "Hello, World!\n",
			want: "Hello, World!\n",
		},
		{
			name:   "Replace",
			init:   "Hello, World!\n",
			offset: 0,
			p:      "ByeBye",
			want:   "ByeBye World!\n",
		},
		{
			name:   "ReplaceMiddle",
			init:   "aaabbbccc",
			offset: 3,
			p:      "XXX",
			want:   "aaaXXXccc",
		},
		{
			name:   "Extend",
			init:   "aaabbbccc",
			offset: 6,
			p:      "XXXYYY",
			want:   "aaabbbXXXYYY",
		},
		{
			name:   "ZeroExtend",
			init:   "aaabbbccc",
			offset: 12,
			p:      "XXX",
			want:   "aaabbbccc\x00\x00\x00XXX",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := New([]byte(test.init))
			if got, err := b.Seek(test.offset, io.SeekStart); got != test.offset || err != nil {
				t.Errorf("b.Seek(%d, io.SeekStart) = %d, %v; want %d, <nil>", test.offset, got, err, test.offset)
			}
			if n, err := b.Write([]byte(test.p)); n != len(test.p) || err != nil {
				t.Errorf("b.Write(%q) = %d, %v; want %d, <nil>", test.p, n, err, len(test.p))
			}

			if got, err := b.Seek(0, io.SeekStart); got != 0 || err != nil {
				t.Errorf("b.Seek(0, io.SeekStart) = %d, %v; want 0, <nil>", got, err)
			}
			if err := iotest.TestReader(b, []byte(test.want)); err != nil {
				t.Error("iotest.TestReader(...)", err)
			}
		})
	}

	t.Run("Multiple", func(t *testing.T) {
		const init = "aaabbbcccddd"
		b := New([]byte(init))
		const offset = 3
		if got, err := b.Seek(offset, io.SeekStart); got != offset || err != nil {
			t.Errorf("b.Seek(%d, io.SeekStart) = %d, %v; want %d, <nil>", offset, got, err, offset)
		}
		if n, err := b.Write([]byte("XXX")); n != 3 || err != nil {
			t.Errorf("b.Write(\"XXX\") = %d, %v; want 3, <nil>", n, err)
		}
		if n, err := b.Write([]byte("YYY")); n != 3 || err != nil {
			t.Errorf("b.Write(\"YYY\") = %d, %v; want 3, <nil>", n, err)
		}

		const want = "aaaXXXYYYddd"
		if got, err := b.Seek(0, io.SeekStart); got != 0 || err != nil {
			t.Errorf("b.Seek(0, io.SeekStart) = %d, %v; want 0, <nil>", got, err)
		}
		if err := iotest.TestReader(b, []byte(want)); err != nil {
			t.Error("iotest.TestReader(...)", err)
		}
	})
}
