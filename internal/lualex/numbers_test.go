// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package lualex

import "testing"

var parseIntTests = []struct {
	s    string
	want int64
	err  bool
}{
	{s: "-0x8000000000000001", want: -0x8000000000000000, err: true},
	{s: "-0x8000000000000000", want: -0x8000000000000000},
	{s: "-0x7fffffffffffffff", want: -0x7fffffffffffffff},
	{s: "-1", want: -1},
	{s: "0", want: 0},
	{s: "1", want: 1},
	{s: "3", want: 3},
	{s: "0xff", want: 0xff},
	{s: "345", want: 345},
	{s: "1000000", want: 1000000},
	{s: "1_000_000", err: true},
	{s: "0xBEBADA", want: 0xBEBADA},
	{s: "0x7fffffffffffffff", want: 0x7fffffffffffffff},
	{s: "0x8000000000000000", want: 0x7fffffffffffffff, err: true},
}

func TestParseInt(t *testing.T) {
	for _, test := range parseIntTests {
		got, err := ParseInt(test.s)
		if got != test.want || (err != nil) != test.err {
			wantError := "<nil>"
			if test.err {
				wantError = "<error>"
			}
			t.Errorf("ParseInt(%q) = %d, %v; want %d, %s", test.s, got, err, test.want, wantError)
		}
	}
}

func TestParseNumber(t *testing.T) {
	tests := []struct {
		s    string
		want float64
		err  bool
	}{
		{s: "-inf", err: true},
		{s: "-INF", err: true},
		{s: "-infinity", err: true},
		{s: "-INFINITY", err: true},
		{s: "-0x8000000000000001", want: -0x8000000000000001},
		{s: "-0x8000000000000000", want: -0x8000000000000000},
		{s: "-0x7fffffffffffffff", want: -0x7fffffffffffffff},
		{s: "-1.0", want: -1},
		{s: "0.0", want: 0},
		{s: "1.0", want: 1},
		{s: "3.0", want: 3.0},
		{s: "3.1416", want: 3.1416},
		{s: "314.16e-2", want: 314.16e-2},
		{s: "0.31416E1", want: 0.31416e1},
		{s: "34e1", want: 34e1},
		{s: "0x0.1E", want: 0x0.1Ep0},
		{s: "0xA23p-4", want: 0xa23p-4},
		{s: "0X1.921FB54442D18P+1", want: 0x1.921FB54442D18p+1},
		{s: "0x1.fp10", want: 1984},
		{s: "1_000_000", err: true},
		{s: "0x7fffffffffffffff", want: 0x7fffffffffffffff},
		{s: "0x8000000000000000", want: 0x8000000000000000},
		{s: "inf", err: true},
		{s: "INF", err: true},
		{s: "infinity", err: true},
		{s: "INFINITY", err: true},
		{s: "nan", err: true},
		{s: "NaN", err: true},
	}

	// All valid integers should parse as numbers.
	for _, test := range parseIntTests {
		if test.err {
			continue
		}

		got, err := ParseNumber(test.s)
		if want := float64(test.want); got != want || (err != nil) != test.err {
			wantError := "<nil>"
			if test.err {
				wantError = "<error>"
			}
			t.Errorf("ParseNumber(%q) = %g, %v; want %g, %s", test.s, got, err, want, wantError)
		}
	}

	for _, test := range tests {
		got, err := ParseNumber(test.s)
		if got != test.want || (err != nil) != test.err {
			wantError := "<nil>"
			if test.err {
				wantError = "<error>"
			}
			t.Errorf("ParseNumber(%q) = %g, %v; want %g, %s", test.s, got, err, test.want, wantError)
		}
	}
}
