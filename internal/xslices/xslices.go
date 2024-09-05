// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

// Package xslices provides more generic functions in the spirit of the [slices] package.
package xslices

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
