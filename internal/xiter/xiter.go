// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package xiter provides various functions useful with iterators of any type.
package xiter

import (
	"errors"
	"iter"
)

// Chain returns an [iter.Seq] that is the logical concatenation of the provided iterators.
func Chain[T any](iterators ...iter.Seq[T]) iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, it := range iterators {
			for v := range it {
				if !yield(v) {
					return
				}
			}
		}
	}
}

// Chain2 returns an [iter.Seq2] that is the logical concatenation of the provided iterators.
func Chain2[K, V any](iterators ...iter.Seq2[K, V]) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for _, it := range iterators {
			for k, v := range it {
				if !yield(k, v) {
					return
				}
			}
		}
	}
}

// All reports whether f reports true for all elements in seq.
func All[T any](seq iter.Seq[T], f func(T) bool) bool {
	for x := range seq {
		if !f(x) {
			return false
		}
	}
	return true
}

// Single obtains the only value from the given iterator.
// Single returns an error if the iterator does not yield exactly one value.
func Single[T any](seq iter.Seq[T]) (T, error) {
	var got T
	var n int
	for x := range seq {
		n++
		if n > 1 {
			return got, errMultipleValues
		}
		got = x
	}
	if n == 0 {
		return got, errNoValues
	}
	return got, nil
}

var (
	errNoValues       = errors.New("no values")
	errMultipleValues = errors.New("multiple values")
)
