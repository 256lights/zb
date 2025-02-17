// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package lua

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
	if got := tab.next(nil); got.key != nil || got.value != nil {
		t.Errorf("newTable(0).next(nil) = %#v; want {<nil> <nil>}", got)
	}
}

func TestArrayTable(t *testing.T) {
	tab := newTable(3)
	const want1 integerValue = 42
	if err := tab.set(integerValue(1), want1); err != nil {
		t.Error(err)
	}
	const want2 = "abc"
	if err := tab.set(integerValue(2), stringValue{s: want2}); err != nil {
		t.Error(err)
	}
	const want3 floatValue = 3.14
	if err := tab.set(integerValue(3), want3); err != nil {
		t.Error(err)
	}

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

	wantEntries := [...]tableEntry{
		{key: integerValue(1), value: want1},
		{key: integerValue(2), value: stringValue{s: want2}},
		{key: integerValue(3), value: want3},
		{},
	}
	for i, control := 0, value(nil); i < len(wantEntries); i++ {
		got := tab.next(control)
		want := wantEntries[i]
		if !cmp.Equal(want, got, cmpValueOptions, cmp.AllowUnexported(tableEntry{})) {
			t.Errorf("tab.next(%#v) = %#v; want %#v", control, got, want)
		}
		control = got.key
	}
}

func TestHashTable(t *testing.T) {
	tab := newTable(3)
	const wantFoo integerValue = 42
	if err := tab.set(stringValue{s: "foo"}, wantFoo); err != nil {
		t.Error(err)
	}
	const wantBar = "abc"
	if err := tab.set(stringValue{s: "bar"}, stringValue{s: wantBar}); err != nil {
		t.Error(err)
	}
	const wantBaz floatValue = 3.14
	if err := tab.set(stringValue{s: "baz"}, wantBaz); err != nil {
		t.Error(err)
	}

	if got, want := tab.len(), integerValue(0); got != want {
		t.Errorf("tab.len() = %d; want %d", got, want)
	}
	if got := tab.get(stringValue{s: "foo"}); got != wantFoo {
		t.Errorf("tab.get(stringValue{s: \"foo\"}) = %#v; want %#v", got, wantFoo)
	}
	if got := tab.get(stringValue{s: "bar"}); !cmp.Equal(stringValue{s: wantBar}, got, cmpValueOptions) {
		t.Errorf("tab.get(stringValue{s: \"bar\"}) = %#v; want %#v", got, wantBar)
	}
	if got := tab.get(stringValue{s: "baz"}); got != wantBaz {
		t.Errorf("tab.get(stringValue{s: \"baz\"}) = %#v; want %#v", got, wantBaz)
	}
	if got := tab.get(integerValue(1)); got != nil {
		t.Errorf("tab.get(integerValue(1)) = %#v; want <nil>", got)
	}

	wantEntries := [...]tableEntry{
		{key: stringValue{s: "bar"}, value: stringValue{s: wantBar}},
		{key: stringValue{s: "baz"}, value: wantBaz},
		{key: stringValue{s: "foo"}, value: wantFoo},
		{},
	}
	for i, control := 0, value(nil); i < len(wantEntries); i++ {
		got := tab.next(control)
		want := wantEntries[i]
		if !cmp.Equal(want, got, cmpValueOptions, cmp.AllowUnexported(tableEntry{})) {
			t.Errorf("tab.next(%#v) = %#v; want %#v", control, got, want)
		}
		control = got.key
	}
}

var cmpValueOptions = cmp.Options{
	cmp.AllowUnexported(stringValue{}),
}
