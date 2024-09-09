// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

// Package xiter provides various functions useful with iterators of any type.
package xiter

import "iter"

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
