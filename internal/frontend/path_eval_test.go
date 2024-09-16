// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package frontend

import "testing"

func TestCollatePath(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"", "a", -1},
		{"a", "", 1},
		{"a", "a", 0},
		{"a", "b", -1},
		{"b", "a", 1},
		{"a", "a/b", -1},
		{"a/b", "a", 1},
		{"a!b", "a/b", 1},
		{"a/b", "a!b", -1},
	}
	for _, test := range tests {
		if got := collatePath(test.a, test.b); got != test.want {
			t.Errorf("collatePath(%q, %q) = %d; want %d", test.a, test.b, got, test.want)
		}
	}
}
