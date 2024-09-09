// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package sets

import (
	"fmt"
	"iter"
	"maps"
	"strings"
)

// Set is an unordered set with O(1) lookup, insertion, and deletion.
// The zero value is an empty set.
type Set[T comparable] map[T]struct{}

// New returns a new set that contains the arguments passed to it.
func New[T comparable](elem ...T) Set[T] {
	s := make(Set[T])
	s.Add(elem...)
	return s
}

// Collect returns a new set that contains the elements of the given iterator.
func Collect[T comparable](seq iter.Seq[T]) Set[T] {
	s := make(Set[T])
	for x := range seq {
		s[x] = struct{}{}
	}
	return s
}

// Add adds the arguments to the set.
func (s Set[T]) Add(elem ...T) {
	for _, x := range elem {
		s[x] = struct{}{}
	}
}

// AddSeq adds the values from seq to the set.
func (s Set[T]) AddSeq(seq iter.Seq[T]) {
	for x := range seq {
		s[x] = struct{}{}
	}
}

// Has reports whether the set contains x.
func (s Set[T]) Has(x T) bool {
	_, present := s[x]
	return present
}

// Clone returns a new set that contains the same elements as s.
func (s Set[T]) Clone() Set[T] {
	if s == nil {
		return make(Set[T])
	}
	return maps.Clone(s)
}

// Len returns the number of elements in the set.
func (s Set[T]) Len() int {
	return len(s)
}

// All returns an iterator of the elements of s.
func (s Set[T]) All() iter.Seq[T] {
	return maps.Keys(s)
}

// Delete removes x from the set if present.
func (s Set[T]) Delete(x T) {
	delete(s, x)
}

// Clear removes all elements from the set,
// but retains the space allocated for the set.
func (s Set[T]) Clear() {
	clear(s)
}

// Format implements [fmt.Formatter]
// by formatting its elements according to the printer state and verb
// surrounded by braces.
func (s Set[T]) Format(f fmt.State, verb rune) {
	format(f, verb, s.All())
}

func format[T any](f fmt.State, verb rune, seq iter.Seq[T]) {
	var buf [1]byte
	buf[0] = '{'
	f.Write(buf[:])

	fmtString := new(strings.Builder)
	fmtString.WriteByte('%')
	for _, flag := range "+-# 0" {
		if f.Flag(int(flag)) {
			fmtString.WriteRune(flag)
		}
	}
	width, hasWidth := f.Width()
	if hasWidth {
		fmtString.WriteByte('*')
	}
	precision, hasPrecision := f.Precision()
	if hasPrecision {
		fmtString.WriteString(".*")
	}
	fmtString.WriteRune(verb)
	args := make([]any, 0, 3)
	if hasWidth {
		args = append(args, width)
	}
	if hasPrecision {
		args = append(args, precision)
	}
	args = append(args, nil)

	first := true
	buf[0] = ' '
	for x := range seq {
		if first {
			first = false
		} else {
			f.Write(buf[:])
		}
		args[len(args)-1] = x
		fmt.Fprintf(f, fmtString.String(), args...)
	}

	buf[0] = '}'
	f.Write(buf[:])
}
