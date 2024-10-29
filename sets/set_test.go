// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package sets

import (
	stdcmp "cmp"
	"iter"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

type set[T any] interface {
	Add(elem ...T)
	AddSeq(seq iter.Seq[T])
	Has(x T) bool
	Len() int
	Delete(x T)
	Clear()
}

var sortUintSlices = cmpopts.SortSlices(stdcmp.Less[uint])

func testSet[S set[uint]](t *testing.T, newSet func() S, all func(S) iter.Seq[uint], sorted bool) {
	diffOptions := cmp.Options{cmpopts.EquateEmpty()}
	if !sorted {
		diffOptions = append(diffOptions, sortUintSlices)
	}

	check := func(t *testing.T, s S, want []uint) {
		t.Helper()

		if got := s.Len(); got != len(want) {
			t.Errorf("s.Len() = %d; want %d", got, len(want))
		}
		for _, x := range want {
			if !s.Has(x) {
				t.Errorf("s.Has(%d) = false; want true", x)
			}
		}
		if diff := cmp.Diff(want, slices.Collect(all(s)), diffOptions); diff != "" {
			if sorted {
				t.Errorf("slices.Collect(s.Values()) (-want +got):\n%s", diff)
			} else {
				t.Errorf("slices.Collect(s.All()) (-want +got):\n%s", diff)
			}
		}
	}

	t.Run("Empty", func(t *testing.T) {
		s := newSet()

		check(t, s, []uint{})
		if s.Has(123) {
			t.Error("s.Has(123) = true; want false")
		}
	})

	t.Run("EmptyDelete", func(t *testing.T) {
		s := newSet()
		s.Delete(123)

		check(t, s, []uint{})
		if s.Has(123) {
			t.Error("s.Has(123) = true; want false")
		}
	})

	t.Run("Add", func(t *testing.T) {
		s := newSet()
		s.Add(123)

		check(t, s, []uint{123})
		if s.Has(456) {
			t.Error("s.Has(456) = true; want false")
		}
	})

	t.Run("Add3", func(t *testing.T) {
		s := newSet()
		s.Add(10, 123, 100)

		check(t, s, []uint{10, 100, 123})
		if s.Has(456) {
			t.Error("s.Has(456) = true; want false")
		}
	})

	t.Run("AddMultiple", func(t *testing.T) {
		s := newSet()
		s.Add(10)
		s.Add(123)
		s.Add(100)

		check(t, s, []uint{10, 100, 123})
		if s.Has(456) {
			t.Error("s.Has(456) = true; want false")
		}
	})

	t.Run("AddSeq", func(t *testing.T) {
		s := newSet()
		s.AddSeq(slices.Values([]uint{10, 123, 100}))

		check(t, s, []uint{10, 100, 123})
		if s.Has(456) {
			t.Error("s.Has(456) = true; want false")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		s := newSet()
		s.Add(10)
		s.Add(123)
		s.Delete(123)

		check(t, s, []uint{10})
		if s.Has(123) {
			t.Error("s.Has(123) = true; want false")
		}
		if s.Has(456) {
			t.Error("s.Has(456) = true; want false")
		}
	})

	t.Run("DeleteMissing", func(t *testing.T) {
		s := newSet()
		s.Add(10)
		s.Add(123)
		s.Delete(456)

		check(t, s, []uint{10, 123})
		if s.Has(456) {
			t.Error("s.Has(456) = true; want false")
		}
	})

	t.Run("Clear", func(t *testing.T) {
		s := newSet()
		s.Add(123)
		s.Clear()

		check(t, s, []uint{})
		if s.Has(123) {
			t.Error("s.Has(123) = true; want false")
		}
	})
}

func TestSet(t *testing.T) {
	testSet(t, func() Set[uint] { return make(Set[uint]) }, Set[uint].All, false)
}

func TestSorted(t *testing.T) {
	testSet(t, func() *Sorted[uint] { return new(Sorted[uint]) }, (*Sorted[uint]).Values, true)

	t.Run("Nil", func(t *testing.T) {
		var s *Sorted[uint]

		if got := s.Len(); got != 0 {
			t.Errorf("s.Len() = %d; want 0", got)
		}
		if s.Has(123) {
			t.Error("s.Has(123) = true; want false")
		}
		if got := slices.Collect(s.Values()); len(got) > 0 {
			t.Errorf("slices.Collect(s.Values()) = %v; want []", got)
		}

		// Make sure this doesn't crash.
		s.Delete(123)
	})
}

func TestBit(t *testing.T) {
	testSet(t, func() *Bit { return new(Bit) }, (*Bit).All, false)

	t.Run("Nil", func(t *testing.T) {
		var s *Bit

		if got := s.Len(); got != 0 {
			t.Errorf("s.Len() = %d; want 0", got)
		}
		if s.Has(123) {
			t.Error("s.Has(123) = true; want false")
		}
		if got := slices.Collect(s.All()); len(got) > 0 {
			t.Errorf("slices.Collect(s.Values()) = %v; want []", got)
		}

		// Make sure this doesn't crash.
		s.Delete(123)
	})
}
