// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package mylua

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestEmptyTable(t *testing.T) {
	tab := newTable(0)
	if got, want := valueType(tab), TypeTable; got != want {
		t.Errorf("valueType(newTable(0)) = %v; want %v", got, want)
	}
	if got := tab.len(); got != 0 {
		t.Errorf("newTable(0).len() = %d; want 0", got)
	}
	if got := tab.get(stringValue{s: "bork"}); got != nil {
		t.Errorf("newTable(0).get(\"bork\") = %#v; want <nil>", got)
	}
}

func TestArrayTable(t *testing.T) {
	tab := newTable(3)
	const want1 integerValue = 42
	tab.set(integerValue(1), want1)
	const want2 = "abc"
	tab.set(integerValue(2), stringValue{s: want2})
	const want3 floatValue = 3.14
	tab.set(integerValue(3), want3)

	if got, want := tab.len(), integerValue(3); got != want {
		t.Errorf("tab.len() = %d; want %d", got, want)
	}
	if got := tab.get(integerValue(1)); got != want1 {
		t.Errorf("tab.get(integerValue(1)) = %#v; want %#v", got, want1)
	}
	if got := tab.get(integerValue(2)); !cmp.Equal(stringValue{s: want2}, got, cmpValueOptions) {
		t.Errorf("tab.get(integerValue(2)) = %#v; want %#v", got, want2)
	}
	if got := tab.get(integerValue(3)); got != want3 {
		t.Errorf("tab.get(integerValue(3)) = %#v; want %#v", got, want3)
	}
	if got := tab.get(integerValue(4)); got != nil {
		t.Errorf("tab.get(integerValue(4)) = %#v; want <nil>", got)
	}
}

var cmpValueOptions = cmp.Options{
	cmp.AllowUnexported(stringValue{}),
}
