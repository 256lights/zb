// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package rangeheader

import (
	"slices"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		s    string
		want []Spec
		err  bool
	}{
		{s: "", want: []Spec{}},
		{s: "bytes=0-499", want: []Spec{IntRange(0, 499)}},
		{s: "bytes=500-999", want: []Spec{IntRange(500, 999)}},
		{s: "bytes=-500", want: []Spec{StartingAt(-500)}},
		{s: "bytes=9500-", want: []Spec{StartingAt(9500)}},
		{s: "bytes=0-0,-1", want: []Spec{IntRange(0, 0), StartingAt(-1)}},
		{
			s: "bytes= 0-999, 4500-5499, -1000",
			want: []Spec{
				IntRange(0, 999),
				IntRange(4500, 5499),
				StartingAt(-1000),
			},
		},
		{
			s: "bytes=500-600,601-999",
			want: []Spec{
				IntRange(500, 600),
				IntRange(601, 999),
			},
		},
		{
			s: "bytes=500-700,601-999",
			want: []Spec{
				IntRange(500, 700),
				IntRange(601, 999),
			},
		},
	}

	for _, test := range tests {
		got, err := Parse(test.s)
		if !slices.Equal(got, test.want) || (err != nil) != test.err {
			errString := "<nil>"
			if test.err {
				errString = "<error>"
			}
			t.Errorf("Parse(%q) = %v, %v; want %v, %s", test.s, got, err, test.want, errString)
		}
	}
}
