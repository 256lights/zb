// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestEmptyLineInfo(t *testing.T) {
	var info LineInfo
	if got, want := info.Len(), 0; got != want {
		t.Errorf("LineInfo{}.Len() = %d; want %d", got, want)
	}
	for pc, line := range info.All() {
		t.Errorf("LineInfo{}.All() yielded %d, %d", pc, line)
	}
}

func TestLineInfo(t *testing.T) {
	tests := [][]int{
		{},
		{1},
		{200},
		{1, 1, 2, 3},
	}

	t.Run("Len", func(t *testing.T) {
		for _, test := range tests {
			got := CollectLineInfo(slices.Values(test))

			if got, want := got.Len(), len(test); got != want {
				t.Errorf("CollectLineInfo(slices.Values(%v)).Len() = %d; want %d", test, got, want)
			}
		}
	})

	t.Run("At", func(t *testing.T) {
		for _, test := range tests {
			got := CollectLineInfo(slices.Values(test))

			for i, want := range test {
				if got := got.At(i); got != want {
					t.Errorf("CollectLineInfo(slices.Values(%v)).At(%d) = %d; want %d", test, i, got, want)
				}
			}
		}
	})

	t.Run("All", func(t *testing.T) {
		for _, test := range tests {
			got := CollectLineInfo(slices.Values(test))

			gotAll := make([]int, 0, len(test))
			for i, line := range got.All() {
				if want := len(gotAll); i != want {
					t.Errorf("CollectLineInfo(slices.Values(%v)).All()[%d] has index %d", test, want, i)
				}
				gotAll = append(gotAll, line)
			}
			if diff := cmp.Diff(test, gotAll); diff != "" {
				t.Errorf("CollectLineInfo(slices.Values(%v)).All() (-want +got):\n%s", test, diff)
			}
		}
	})
}

func TestLineInfoWriter(t *testing.T) {
	type nextCall struct {
		line int
		want int8
	}

	tests := []struct {
		name  string
		base  int
		calls []nextCall
	}{
		{
			name: "AllSame",
			calls: []nextCall{
				{100, 100},
				{100, 0},
				{100, 0},
			},
		},
		{
			name: "AllRelative",
			calls: []nextCall{
				{100, 100},
				{200, 100},
				{300, 100},
			},
		},
		{
			name: "AllRelativeWithBase",
			base: 99,
			calls: []nextCall{
				{100, 1},
				{200, 100},
				{300, 100},
			},
		},
		{
			name: "StartAbsolute",
			calls: []nextCall{
				{200, absMarker},
				{300, 100},
				{400, 100},
			},
		},
		{
			name: "InsertAbsoluteAfterLimit",
			calls: append(
				append(
					[]nextCall{{100, 100}},
					slices.Repeat([]nextCall{{100, 0}}, maxInstructionsWithoutAbsLineInfo-1)...,
				),
				nextCall{100, absMarker},
			),
		},
		{
			name: "InsertAbsoluteAfterSecondLimit",
			calls: append(
				append(
					append(
						append(
							[]nextCall{{100, 100}},
							slices.Repeat([]nextCall{{100, 0}}, maxInstructionsWithoutAbsLineInfo-1)...,
						),
						nextCall{100, absMarker},
					),
					slices.Repeat([]nextCall{{100, 0}}, maxInstructionsWithoutAbsLineInfo-1)...,
				),
				nextCall{100, absMarker},
			),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := lineInfoWriter{previousLine: test.base}
			for i, call := range test.calls {
				if got := w.next(call.line); got != call.want {
					t.Errorf("[%d]: w.next(%d) = %d; want %d", i, call.line, got, call.want)
				}
			}
		})
	}
}

var lineInfoCompareOption = cmp.Transformer("lineInfoToSlice", lineInfoToSlice)

func lineInfoToSlice(info LineInfo) []int {
	s := make([]int, 0, info.Len())
	for _, line := range info.All() {
		s = append(s, line)
	}
	return s
}
