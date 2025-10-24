// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package zbstore

import (
	"iter"
	"strings"
)

// A type that implements Replacer can transform a string.
// For example, a simple implementation of Replacer may replace occurrences of "$foo" with "bar".
// Calling Replace("x$fooy") on such a Replacer would return "xbary".
// Implementations of Replace must be safe to call from multiple goroutines simultaneously.
//
// [*strings.Replacer] is a common implementation of Replacer.
type Replacer interface {
	Replace(s string) string
}

func newReplacer[K, V ~string](rewrites iter.Seq2[K, V]) *strings.Replacer {
	var args []string
	for k, v := range rewrites {
		args = append(args, string(k), string(v))
	}
	return strings.NewReplacer(args...)
}
