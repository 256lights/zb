// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package xslices provides more generic functions in the spirit of the [slices] package.
package xslices

import "slices"

// Last returns the last element in s.
// Last panics if len(s) == 0.
func Last[S ~[]E, E any](s S) E {
	return s[len(s)-1]
}

// Pop sets the last n elements of the slice to zero values
// and returns s[:len(s)-n].
func Pop[S ~[]E, E any](s S, n int) S {
	end := len(s) - n
	clear(s[end:])
	return s[:end]
}

// Filter removes any element x from s for which f(x) reports false,
// returning the modified slice.
func Filter[S ~[]E, E any](s S, f func(E) bool) S {
	n := 0
	for _, x := range s {
		if f(x) {
			s[n] = x
			n++
		}
	}
	clear(s[n:])
	return s[:n]
}

// ClonePointers returns a copy of the slice.
// The referenced values are copied using assignment,
// so this is a clone with depth of 1.
// The result may have additional unused capacity.
// The result preserves the nilness of s.
func ClonePointers[S ~[]*E, E any](s S) S {
	if s == nil {
		return nil
	}
	n := 0
	for _, p := range s {
		if p != nil {
			n++
		}
	}
	pool := make([]E, n)
	result := slices.Grow(S{}, len(s))[:len(s)]
	for i, p := range s {
		if p == nil {
			continue
		}
		x := &pool[0]
		pool = pool[1:]
		*x = *p
		result[i] = x
	}
	return result
}
