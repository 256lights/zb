// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

// Package xmaps provides more generic functions in the spirit of the [maps] package.
package xmaps

import (
	"cmp"
	"iter"
	"slices"
)

// SortedKeys returns a slice of the map's keys in sorted order.
func SortedKeys[M ~map[K]V, K cmp.Ordered, V any](m M) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// Sorted iterates over a map in sorted order.
func Sorted[M ~map[K]V, K cmp.Ordered, V any](m M) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for _, k := range SortedKeys(m) {
			if !yield(k, m[k]) {
				return
			}
		}
	}
}

// Init clears m and returns m if m != nil
// or returns a new map otherwise.
func Init[M ~map[K]V, K comparable, V any](m M) M {
	if m == nil {
		return make(M)
	}
	clear(m)
	return m
}
