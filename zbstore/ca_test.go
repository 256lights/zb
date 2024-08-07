// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package zbstore

import (
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zombiezen.com/go/nix"
)

func TestSourceSHA256ContentAddress(t *testing.T) {
	tests := []struct {
		name      string
		digest    string
		sourceNAR string

		wantCleartext string
	}{
		{
			name:   "NoSelfReference",
			digest: "",
			sourceNAR: "\x0d\x00\x00\x00\x00\x00\x00\x00" +
				"nix-archive-1\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				"(\x00\x00\x00\x00\x00\x00\x00" +
				"\x04\x00\x00\x00\x00\x00\x00\x00" +
				"type\x00\x00\x00\x00" +
				"\x07\x00\x00\x00\x00\x00\x00\x00" +
				"regular\x00" +
				"\x08\x00\x00\x00\x00\x00\x00\x00" +
				"contents" +
				"\x0e\x00\x00\x00\x00\x00\x00\x00" +
				"Hello, World!\n\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				")\x00\x00\x00\x00\x00\x00\x00",
			wantCleartext: "\x0d\x00\x00\x00\x00\x00\x00\x00" +
				"nix-archive-1\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				"(\x00\x00\x00\x00\x00\x00\x00" +
				"\x04\x00\x00\x00\x00\x00\x00\x00" +
				"type\x00\x00\x00\x00" +
				"\x07\x00\x00\x00\x00\x00\x00\x00" +
				"regular\x00" +
				"\x08\x00\x00\x00\x00\x00\x00\x00" +
				"contents" +
				"\x0e\x00\x00\x00\x00\x00\x00\x00" +
				"Hello, World!\n\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				")\x00\x00\x00\x00\x00\x00\x00|",
		},
		{
			name:   "SelfReference1",
			digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			sourceNAR: "\x0d\x00\x00\x00\x00\x00\x00\x00" +
				"nix-archive-1\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				"(\x00\x00\x00\x00\x00\x00\x00" +
				"\x04\x00\x00\x00\x00\x00\x00\x00" +
				"type\x00\x00\x00\x00" +
				"\x07\x00\x00\x00\x00\x00\x00\x00" +
				"regular\x00" +
				"\x08\x00\x00\x00\x00\x00\x00\x00" +
				"contents" +
				"\x34\x00\x00\x00\x00\x00\x00\x00" +
				"/zb/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-path.txt\n\x00\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				")\x00\x00\x00\x00\x00\x00\x00",
			wantCleartext: "\x0d\x00\x00\x00\x00\x00\x00\x00" +
				"nix-archive-1\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				"(\x00\x00\x00\x00\x00\x00\x00" +
				"\x04\x00\x00\x00\x00\x00\x00\x00" +
				"type\x00\x00\x00\x00" +
				"\x07\x00\x00\x00\x00\x00\x00\x00" +
				"regular\x00" +
				"\x08\x00\x00\x00\x00\x00\x00\x00" +
				"contents" +
				"\x34\x00\x00\x00\x00\x00\x00\x00" +
				"/zb/store/\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00-path.txt\n\x00\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				")\x00\x00\x00\x00\x00\x00\x00||106",
		},
		{
			name:   "SelfReference2",
			digest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			sourceNAR: "\x0d\x00\x00\x00\x00\x00\x00\x00" +
				"nix-archive-1\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				"(\x00\x00\x00\x00\x00\x00\x00" +
				"\x04\x00\x00\x00\x00\x00\x00\x00" +
				"type\x00\x00\x00\x00" +
				"\x07\x00\x00\x00\x00\x00\x00\x00" +
				"regular\x00" +
				"\x08\x00\x00\x00\x00\x00\x00\x00" +
				"contents" +
				"\x34\x00\x00\x00\x00\x00\x00\x00" +
				"/zb/store/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-path.txt\n\x00\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				")\x00\x00\x00\x00\x00\x00\x00",
			wantCleartext: "\x0d\x00\x00\x00\x00\x00\x00\x00" +
				"nix-archive-1\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				"(\x00\x00\x00\x00\x00\x00\x00" +
				"\x04\x00\x00\x00\x00\x00\x00\x00" +
				"type\x00\x00\x00\x00" +
				"\x07\x00\x00\x00\x00\x00\x00\x00" +
				"regular\x00" +
				"\x08\x00\x00\x00\x00\x00\x00\x00" +
				"contents" +
				"\x34\x00\x00\x00\x00\x00\x00\x00" +
				"/zb/store/\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00-path.txt\n\x00\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				")\x00\x00\x00\x00\x00\x00\x00||106",
		},
		{
			name: "SameContentAsSelfReference",
			sourceNAR: "\x0d\x00\x00\x00\x00\x00\x00\x00" +
				"nix-archive-1\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				"(\x00\x00\x00\x00\x00\x00\x00" +
				"\x04\x00\x00\x00\x00\x00\x00\x00" +
				"type\x00\x00\x00\x00" +
				"\x07\x00\x00\x00\x00\x00\x00\x00" +
				"regular\x00" +
				"\x08\x00\x00\x00\x00\x00\x00\x00" +
				"contents" +
				"\x34\x00\x00\x00\x00\x00\x00\x00" +
				"/zb/store/\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00-path.txt\n\x00\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				")\x00\x00\x00\x00\x00\x00\x00",
			wantCleartext: "\x0d\x00\x00\x00\x00\x00\x00\x00" +
				"nix-archive-1\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				"(\x00\x00\x00\x00\x00\x00\x00" +
				"\x04\x00\x00\x00\x00\x00\x00\x00" +
				"type\x00\x00\x00\x00" +
				"\x07\x00\x00\x00\x00\x00\x00\x00" +
				"regular\x00" +
				"\x08\x00\x00\x00\x00\x00\x00\x00" +
				"contents" +
				"\x34\x00\x00\x00\x00\x00\x00\x00" +
				"/zb/store/\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00-path.txt\n\x00\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				")\x00\x00\x00\x00\x00\x00\x00|",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := SourceSHA256ContentAddress(test.digest, strings.NewReader(test.sourceNAR))
			if err != nil {
				t.Fatal(err)
			}
			if !got.IsRecursiveFile() {
				t.Errorf("content address = %v; want recursive file", got)
			}

			h := nix.NewHasher(nix.SHA256)
			h.WriteString(test.wantCleartext)
			if got, want := got.Hash(), h.SumHash(); !got.Equal(want) {
				t.Errorf("content address hash = %v; want %v", got, want)
			}
		})
	}
}

var hashModuloGoldens = []struct {
	modulus string
	s       string

	want        string
	wantOffsets []int64
}{
	{"foo", "", "", []int64{}},
	{"foo", "x", "x", []int64{}},
	{"foo", "bar", "bar", []int64{}},
	{"foo", "foo", "\x00\x00\x00", []int64{0}},
	{"foo", "\x00\x00\x00", "\x00\x00\x00", []int64{}},
	{"foo", "xfoo", "x\x00\x00\x00", []int64{1}},
	{"foo", "fooy", "\x00\x00\x00y", []int64{0}},
	{"foo", "xfooy", "x\x00\x00\x00y", []int64{1}},
	{"foo", "xffooy", "xf\x00\x00\x00y", []int64{2}},
}

func FuzzHashModuloReader(f *testing.F) {
	for _, test := range hashModuloGoldens {
		f.Add(test.modulus, test.s)
	}

	f.Fuzz(func(t *testing.T, modulus string, s string) {
		want, wantOffsets := hashModuloOracle(modulus, s)

		t.Run("ReadAll", func(t *testing.T) {
			hmr := newHashModuloReader(modulus, strings.NewReader(s))
			got, err := io.ReadAll(hmr)
			checkHashModulo(t, modulus, s, want, wantOffsets, string(got), err, hmr.offsets)
		})

		t.Run("SourceOneByte", func(t *testing.T) {
			hmr := newHashModuloReader(modulus, iotest.OneByteReader(strings.NewReader(s)))
			got, err := io.ReadAll(hmr)
			checkHashModulo(t, modulus, s, want, wantOffsets, string(got), err, hmr.offsets)
		})

		t.Run("OneByte", func(t *testing.T) {
			hmr := newHashModuloReader(modulus, strings.NewReader(s))
			got, err := io.ReadAll(iotest.OneByteReader(hmr))
			checkHashModulo(t, modulus, s, want, wantOffsets, string(got), err, hmr.offsets)
		})
	})
}

func hashModuloOracle(modulus, s string) (want string, offsets []int64) {
	if modulus == "" {
		return s, nil
	}

	for {
		start := 0
		if len(offsets) > 0 {
			start = int(offsets[len(offsets)-1]) + len(modulus)
		}
		i := strings.Index(s[start:], modulus)
		if i < 0 {
			break
		}
		offsets = append(offsets, int64(start+i))
	}

	if len(offsets) == 0 {
		return s, nil
	}

	sb := new(strings.Builder)
	sb.Grow(len(s))
	prev := 0
	for _, i := range offsets {
		sb.WriteString(s[prev:i])
		for range len(modulus) {
			sb.WriteByte(0)
		}
		prev = int(i) + len(modulus)
	}
	sb.WriteString(s[prev:])
	return sb.String(), offsets
}

func TestHashModuloOracle(t *testing.T) {
	for _, test := range hashModuloGoldens {
		got, gotOffsets := hashModuloOracle(test.modulus, test.s)
		checkHashModulo(t, test.modulus, test.s, test.want, test.wantOffsets, got, nil, gotOffsets)
	}
}

func checkHashModulo(tb testing.TB, modulus, s string, want string, wantOffsets []int64, got string, err error, gotOffsets []int64) {
	tb.Helper()

	if err != nil {
		tb.Errorf("newHashModuloReader(%q, %q): read error: %v", modulus, s, err)
	}
	if string(got) != want {
		tb.Errorf("newHashModuloReader(%q, %q): read %q; want %q", modulus, s, got, want)
	}
	if diff := cmp.Diff(wantOffsets, gotOffsets, cmpopts.EquateEmpty()); diff != "" {
		tb.Errorf("newHashModuloReader(%q, %q): offsets (-want +got):\n%s", modulus, s, diff)
	}
}
