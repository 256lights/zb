// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

// Package sortedset provides a set type implemented as a sorted list.
package sortedset

import (
	"cmp"
	"slices"
)

// Set is a sorted list of unique items.
// The zero value is an empty set.
// nil is treated like an empty set, but any attempts to add to it will panic.
type Set[T cmp.Ordered] struct {
	elems []T
}

// New returns a new set with the given elements.
// Equivalent to calling [Set.Add] on a zero set.
func New[T cmp.Ordered](elem ...T) *Set[T] {
	s := new(Set[T])
	s.Add(elem...)
	return s
}

// Add adds the arguments to the set.
func (s *Set[T]) Add(elem ...T) {
	for _, x := range elem {
		i, present := slices.BinarySearch(s.elems, x)
		if !present {
			s.elems = slices.Insert(s.elems, i, x)
		}
	}
}

// AddSet adds the elements in other to s.
func (s *Set[T]) AddSet(other *Set[T]) {
	// TODO(someday): Because we know others.elems is sorted,
	// we can almost certainly do this more efficiently.
	s.Add(other.elems...)
}

// Has reports whether the set contains x.
func (s *Set[T]) Has(x T) bool {
	if s == nil {
		return false
	}
	_, present := slices.BinarySearch(s.elems, x)
	return present
}

// Clone returns a new set that contains the same elements as s.
func (s *Set[T]) Clone() *Set[T] {
	if s == nil {
		return new(Set[T])
	}
	return &Set[T]{elems: slices.Clone(s.elems)}
}

// Grow ensures that the set can add n more unique elements
// without allocating.
func (s *Set[T]) Grow(n int) {
	s.elems = slices.Grow(s.elems, n)
}

// Len returns the number of elements in the set.
func (s *Set[T]) Len() int {
	if s == nil {
		return 0
	}
	return len(s.elems)
}

// At returns the i'th element in ascending order of the set.
func (s *Set[T]) At(i int) T {
	return s.elems[i]
}

// Delete removes x from the set if present.
func (s *Set[T]) Delete(x T) {
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
func (s *Set[T]) Clear() {
	if s == nil {
		return
	}
	s.elems = slices.Delete(s.elems, 0, len(s.elems))
}
