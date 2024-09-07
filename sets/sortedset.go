// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package sets

import (
	"cmp"
	"iter"
	"slices"
)

// Sorted is a sorted list of unique items.
// The zero value is an empty set.
// nil is treated like an empty set, but any attempts to add to it will panic.
type Sorted[T cmp.Ordered] struct {
	elems []T
}

// NewSorted returns a new set with the given elements.
// Equivalent to calling [Sorted.Add] on a zero set.
func NewSorted[T cmp.Ordered](elem ...T) *Sorted[T] {
	s := new(Sorted[T])
	s.Add(elem...)
	return s
}

// Collect returns a new set that contains the elements of the given iterator.
// Equivalent to calling [Sorted.AddSeq] on a zero set.
func CollectSorted[T cmp.Ordered](seq iter.Seq[T]) *Sorted[T] {
	s := new(Sorted[T])
	s.AddSeq(seq)
	return s
}

// Add adds the arguments to the set.
func (s *Sorted[T]) Add(elem ...T) {
	s.AddSeq(slices.Values(elem))
}

// AddSeq adds the values from seq to the set.
func (s *Sorted[T]) AddSeq(seq iter.Seq[T]) {
	for x := range seq {
		i, present := slices.BinarySearch(s.elems, x)
		if !present {
			s.elems = slices.Insert(s.elems, i, x)
		}
	}
}

// AddSet adds the elements in other to s.
func (s *Sorted[T]) AddSet(other *Sorted[T]) {
	// TODO(someday): Because we know others.elems is sorted,
	// we can almost certainly do this more efficiently.
	s.Add(other.elems...)
}

// Has reports whether the set contains x.
func (s *Sorted[T]) Has(x T) bool {
	if s == nil {
		return false
	}
	_, present := slices.BinarySearch(s.elems, x)
	return present
}

// Clone returns a new set that contains the same elements as s.
func (s *Sorted[T]) Clone() *Sorted[T] {
	if s == nil {
		return new(Sorted[T])
	}
	return &Sorted[T]{elems: slices.Clone(s.elems)}
}

// Grow ensures that the set can add n more unique elements
// without allocating.
func (s *Sorted[T]) Grow(n int) {
	s.elems = slices.Grow(s.elems, n)
}

// Len returns the number of elements in the set.
func (s *Sorted[T]) Len() int {
	if s == nil {
		return 0
	}
	return len(s.elems)
}

// At returns the i'th element in ascending order of the set.
func (s *Sorted[T]) At(i int) T {
	return s.elems[i]
}

// All returns an iterator of the elements of s.
func (s *Sorted[T]) All() iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for i := 0; i < s.Len(); i++ {
			if !yield(i, s.At(i)) {
				return
			}
		}
	}
}

// Delete removes x from the set if present.
func (s *Sorted[T]) Delete(x T) {
	if s == nil {
		return
	}
	i, present := slices.BinarySearch(s.elems, x)
	if !present {
		return
	}
	s.elems = slices.Delete(s.elems, i, i+1)
}

// Clear removes all elements from the set,
// but retains the space allocated for the set.
func (s *Sorted[T]) Clear() {
	if s == nil {
		return
	}
	s.elems = slices.Delete(s.elems, 0, len(s.elems))
}
