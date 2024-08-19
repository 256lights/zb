// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package detect

import (
	stdcmp "cmp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"zombiezen.com/go/zb/sortedset"
)

var refFinderGoldens = []struct {
	s      string
	search []string
	want   *sortedset.Set[string]
}{
	{"", nil, sortedset.New[string]()},
	{"", []string{""}, sortedset.New("")},
	{"foo", []string{""}, sortedset.New("")},
	{"foo", []string{"f"}, sortedset.New("f")},
	{"foo", []string{"o"}, sortedset.New("o")},

	{"foo", []string{"foo"}, sortedset.New("foo")},
	{"xfoo", []string{"foo"}, sortedset.New("foo")},
	{"fooy", []string{"foo"}, sortedset.New("foo")},
	{"xfooy", []string{"foo"}, sortedset.New("foo")},
	{"bar", []string{"foo"}, sortedset.New[string]()},

	{"foo", []string{"f", "foo"}, sortedset.New("f", "foo")},
	{"foo", []string{"o", "foo"}, sortedset.New("o", "foo")},

	{"foo", []string{"foo", "bar"}, sortedset.New("foo")},
	{"bar", []string{"foo", "bar"}, sortedset.New("bar")},
	{"foobar", []string{"foo", "bar"}, sortedset.New("foo", "bar")},
}

func FuzzRefFinder(f *testing.F) {
	const sep = "\x1f"
	for _, test := range refFinderGoldens {
		f.Add(test.s, strings.Join(test.search, sep))
	}

	f.Fuzz(func(t *testing.T, s string, searchJoined string) {
		search := strings.Split(searchJoined, sep)
		want := refFinderOracle(s, search)

		t.Run("Write", func(t *testing.T) {
			rf := NewRefFinder(search)
			if n, err := rf.Write([]byte(s)); n != len(s) || err != nil {
				t.Errorf("NewRefFinder(%q).Write(%q) = %d, %v; want %d, <nil>",
					search, s, n, err, len(s))
			}
			got := rf.Found()
			if diff := cmp.Diff(want, got, transformSortedSet[string]()); diff != "" {
				t.Errorf("rf := NewRefFinder(%q); rf.Write(%q); rf.Found() (-want +got):\n%s",
					search, s, diff)
			}
		})

		t.Run("WriteString", func(t *testing.T) {
			rf := NewRefFinder(search)
			if n, err := rf.WriteString(s); n != len(s) || err != nil {
				t.Errorf("NewRefFinder(%q).WriteString(%q) = %d, %v; want %d, <nil>",
					search, s, n, err, len(s))
			}
			got := rf.Found()
			if diff := cmp.Diff(want, got, transformSortedSet[string]()); diff != "" {
				t.Errorf("rf := NewRefFinder(%q); rf.WriteString(%q); rf.Found() (-want +got):\n%s",
					search, s, diff)
			}
		})
	})
}

func TestRefFinderOracle(t *testing.T) {
	for _, test := range refFinderGoldens {
		got := refFinderOracle(test.s, test.search)
		if diff := cmp.Diff(test.want, got, transformSortedSet[string]()); diff != "" {
			t.Errorf("refFinderOracle(%q, %q) (-want +got):\n%s", test.s, test.search, diff)
		}
	}
}

func refFinderOracle(s string, search []string) *sortedset.Set[string] {
	result := new(sortedset.Set[string])
	for _, substr := range search {
		if strings.Contains(s, substr) {
			result.Add(substr)
		}
	}
	return result
}

func transformSortedSet[E stdcmp.Ordered]() cmp.Option {
	return cmp.Transformer("transformSortedSet", func(s sortedset.Set[E]) []E {
		list := make([]E, s.Len())
		for i := range list {
			list[i] = s.At(i)
		}
		return list
	})
}
