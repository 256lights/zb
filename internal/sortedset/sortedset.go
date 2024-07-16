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

func (s *Set[T]) Add(elem ...T) {
	for _, x := range elem {
		i, present := slices.BinarySearch(s.elems, x)
		if !present {
			s.elems = slices.Insert(s.elems, i, x)
		}
	}
}

func (s *Set[T]) AddSet(other *Set[T]) {
	// TODO(someday): Because we know others.elems is sorted,
	// we can almost certainly do this more efficiently.
	s.Add(other.elems...)
}

func (s *Set[T]) Clone() *Set[T] {
	if s == nil {
		return nil
	}
	return &Set[T]{elems: slices.Clone(s.elems)}
}

func (s *Set[T]) Grow(n int) {
	s.elems = slices.Grow(s.elems, n)
}

func (s *Set[T]) Len() int {
	if s == nil {
		return 0
	}
	return len(s.elems)
}

func (s *Set[T]) At(i int) T {
	return s.elems[i]
}
