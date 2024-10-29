// Copyright 2020 YourBase Inc.
// SPDX-License-Identifier: BSD-3-Clause

package jsonstring

import (
	"crypto/rand"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestAppend(t *testing.T) {
	tests := []string{
		"",
		"\x00",
		"\x01",
		"\x02",
		"\x03",
		"\x04",
		"\x05",
		"\x06",
		"\x07",
		"\x08",
		"\x09",
		"\x0a",
		"\x0b",
		"\x0c",
		"\x0d",
		"\x0e",
		"\x0f",
		"\x10",
		"\x11",
		"\x12",
		"\x13",
		"\x14",
		"\x15",
		"\x16",
		"\x17",
		"\x18",
		"\x19",
		"\x1a",
		"\x1b",
		"\x1c",
		"\x1d",
		"\x1e",
		"\x1f",
		"\x20",
		"a",
		"abc",
		"\\",
		"\"",
		"\xff",
		"\xc3\xb1",
		"\u2028",
		"\u2029",
	}
	for _, test := range tests {
		wantJSON, _ := json.Marshal(test)
		got := Append(nil, test)
		var parsed string
		if err := json.Unmarshal(got, &parsed); err != nil {
			t.Errorf("Append(nil, %q) = `%s` (invalid JSON!); want something like `%s`", test, got, wantJSON)
			continue
		}
		want := new(strings.Builder)
		for _, r := range test {
			// Invalid UTF-8 sequences become \ufffd, which is the behavior we want.
			want.WriteRune(r)
		}
		if parsed != want.String() {
			t.Errorf("Append(nil, %q) = `%s`; want something like `%s`", test, got, wantJSON)
		}
	}
}

const smallInput = "Hello, World!"

func BenchmarkAppend(b *testing.B) {
	b.Run("Small", func(b *testing.B) {
		b.SetBytes(int64(len(smallInput)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			Append(nil, smallInput)
		}
	})
	b.Run("Large", func(b *testing.B) {
		input := new(strings.Builder)
		if _, err := io.CopyN(input, rand.Reader, 1<<20); err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(input.Len()))
		b.ReportAllocs()
		b.ResetTimer()
		var buf []byte
		for i := 0; i < b.N; i++ {
			buf = Append(buf[:0], input.String())
		}
	})
}

func BenchmarkJSONMarshal(b *testing.B) {
	b.Run("Small", func(b *testing.B) {
		b.SetBytes(int64(len(smallInput)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			json.Marshal(smallInput)
		}
	})
	b.Run("Large", func(b *testing.B) {
		input := new(strings.Builder)
		if _, err := io.CopyN(input, rand.Reader, 1<<20); err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(input.Len()))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			json.Marshal(input.String())
		}
	})
}
