// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package xhttp

import (
	"slices"
	"testing"
)

func TestParseRange(t *testing.T) {
	tests := []struct {
		s    string
		want []RangeSpec
		err  bool
	}{
		{s: "", want: []RangeSpec{}},
		{s: "bytes=0-499", want: []RangeSpec{IntRange(0, 499)}},
		{s: "bytes=500-999", want: []RangeSpec{IntRange(500, 999)}},
		{s: "bytes=-500", want: []RangeSpec{RangeStartingAt(-500)}},
		{s: "bytes=9500-", want: []RangeSpec{RangeStartingAt(9500)}},
		{s: "bytes=0-0,-1", want: []RangeSpec{IntRange(0, 0), RangeStartingAt(-1)}},
		{
			s: "bytes= 0-999, 4500-5499, -1000",
			want: []RangeSpec{
				IntRange(0, 999),
				IntRange(4500, 5499),
				RangeStartingAt(-1000),
			},
		},
		{
			s: "bytes=500-600,601-999",
			want: []RangeSpec{
				IntRange(500, 600),
				IntRange(601, 999),
			},
		},
		{
			s: "bytes=500-700,601-999",
			want: []RangeSpec{
				IntRange(500, 700),
				IntRange(601, 999),
			},
		},
	}

	for _, test := range tests {
		got, err := ParseRange(test.s)
		if !slices.Equal(got, test.want) || (err != nil) != test.err {
			errString := "<nil>"
			if test.err {
				errString = "<error>"
			}
			t.Errorf("Parse(%q) = %v, %v; want %v, %s", test.s, got, err, test.want, errString)
		}
	}
}
