// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package detect

import (
	stdcmp "cmp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"zb.256lights.llc/pkg/sets"
)

var refFinderGoldens = []struct {
	s      string
	search []string
	want   *sets.Sorted[string]
}{
	{"", nil, sets.NewSorted[string]()},
	{"", []string{""}, sets.NewSorted("")},
	{"foo", []string{""}, sets.NewSorted("")},
	{"foo", []string{"f"}, sets.NewSorted("f")},
	{"foo", []string{"o"}, sets.NewSorted("o")},

	{"foo", []string{"foo"}, sets.NewSorted("foo")},
	{"xfoo", []string{"foo"}, sets.NewSorted("foo")},
	{"fooy", []string{"foo"}, sets.NewSorted("foo")},
	{"xfooy", []string{"foo"}, sets.NewSorted("foo")},
	{"bar", []string{"foo"}, sets.NewSorted[string]()},

	{"foo", []string{"f", "foo"}, sets.NewSorted("f", "foo")},
	{"foo", []string{"o", "foo"}, sets.NewSorted("o", "foo")},

	{"foo", []string{"foo", "bar"}, sets.NewSorted("foo")},
	{"bar", []string{"foo", "bar"}, sets.NewSorted("bar")},
	{"foobar", []string{"foo", "bar"}, sets.NewSorted("foo", "bar")},
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
			rf := NewRefFinder(slices.Values(search))
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
			rf := NewRefFinder(slices.Values(search))
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

func refFinderOracle(s string, search []string) *sets.Sorted[string] {
	result := new(sets.Sorted[string])
	for _, substr := range search {
		if strings.Contains(s, substr) {
			result.Add(substr)
		}
	}
	return result
}

func transformSortedSet[E stdcmp.Ordered]() cmp.Option {
	return cmp.Transformer("transformSortedSet", func(s sets.Sorted[E]) []E {
		list := make([]E, s.Len())
		for i := range list {
			list[i] = s.At(i)
		}
		return list
	})
}
