// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package detect

import (
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var hashModuloGoldens = []struct {
	old string
	new string
	s   string

	want        string
	wantOffsets []int64
}{
	{"", "", "", "", []int64{}},
	{"", "", "foo", "foo", []int64{}},

	{"foo", "\x00\x00\x00", "", "", []int64{}},
	{"foo", "\x00\x00\x00", "x", "x", []int64{}},
	{"foo", "\x00\x00\x00", "bar", "bar", []int64{}},
	{"foo", "\x00\x00\x00", "foo", "\x00\x00\x00", []int64{0}},
	{"foo", "\x00\x00\x00", "\x00\x00\x00", "\x00\x00\x00", []int64{}},
	{"foo", "\x00\x00\x00", "xfoo", "x\x00\x00\x00", []int64{1}},
	{"foo", "\x00\x00\x00", "fooy", "\x00\x00\x00y", []int64{0}},
	{"foo", "\x00\x00\x00", "xfooy", "x\x00\x00\x00y", []int64{1}},
	{"foo", "\x00\x00\x00", "xffooy", "xf\x00\x00\x00y", []int64{2}},

	{"foo", "bar", "", "", []int64{}},
	{"foo", "bar", "x", "x", []int64{}},
	{"foo", "bar", "bar", "bar", []int64{}},
	{"foo", "bar", "foo", "bar", []int64{0}},
	{"foo", "bar", "\x00\x00\x00", "\x00\x00\x00", []int64{}},
	{"foo", "bar", "xfoo", "xbar", []int64{1}},
	{"foo", "bar", "fooy", "bary", []int64{0}},
	{"foo", "bar", "xfooy", "xbary", []int64{1}},
	{"foo", "bar", "xffooy", "xfbary", []int64{2}},
}

func FuzzHashModuloReader(f *testing.F) {
	for _, test := range hashModuloGoldens {
		f.Add(test.old, test.new, test.s)
	}

	f.Fuzz(func(t *testing.T, oldString, newString string, s string) {
		if len(oldString) != len(newString) {
			return
		}
		want, wantOffsets := hashModuloOracle(oldString, newString, s)

		t.Run("ReadAll", func(t *testing.T) {
			hmr := NewHashModuloReader(oldString, newString, strings.NewReader(s))
			got, err := io.ReadAll(hmr)
			checkHashModulo(t, oldString, newString, s, want, wantOffsets, string(got), err, hmr.offsets)
		})

		t.Run("SourceOneByte", func(t *testing.T) {
			hmr := NewHashModuloReader(oldString, newString, iotest.OneByteReader(strings.NewReader(s)))
			got, err := io.ReadAll(hmr)
			checkHashModulo(t, oldString, newString, s, want, wantOffsets, string(got), err, hmr.offsets)
		})

		t.Run("OneByte", func(t *testing.T) {
			hmr := NewHashModuloReader(oldString, newString, strings.NewReader(s))
			got, err := io.ReadAll(iotest.OneByteReader(hmr))
			checkHashModulo(t, oldString, newString, s, want, wantOffsets, string(got), err, hmr.offsets)
		})
	})
}

func hashModuloOracle(oldString, newString, s string) (want string, offsets []int64) {
	if oldString == "" {
		return s, nil
	}

	for {
		start := 0
		if len(offsets) > 0 {
			start = int(offsets[len(offsets)-1]) + len(oldString)
		}
		i := strings.Index(s[start:], oldString)
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
		sb.WriteString(newString)
		prev = int(i) + len(oldString)
	}
	sb.WriteString(s[prev:])
	return sb.String(), offsets
}

func TestHashModuloOracle(t *testing.T) {
	for _, test := range hashModuloGoldens {
		got, gotOffsets := hashModuloOracle(test.old, test.new, test.s)
		checkHashModulo(t, test.old, test.new, test.s, test.want, test.wantOffsets, got, nil, gotOffsets)
	}
}

func checkHashModulo(tb testing.TB, old, new, s string, want string, wantOffsets []int64, got string, err error, gotOffsets []int64) {
	tb.Helper()

	if err != nil {
		tb.Errorf("NewHashModuloReader(%q, %q, %q): read error: %v", old, new, s, err)
	}
	if string(got) != want {
		tb.Errorf("NewHashModuloReader(%q, %q, %q): read %q; want %q", old, new, s, got, want)
	}
	if diff := cmp.Diff(wantOffsets, gotOffsets, cmpopts.EquateEmpty()); diff != "" {
		tb.Errorf("NewHashModuloReader(%q, %q, %q): offsets (-want +got):\n%s", old, new, s, diff)
	}
}
