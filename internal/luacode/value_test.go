// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import "testing"

func TestValueUnquoted(t *testing.T) {
	tests := []struct {
		value    Value
		want     string
		isString bool
	}{
		{Value{}, "", false},
		{BoolValue(false), "", false},
		{BoolValue(true), "", false},
		{IntegerValue(42), "42", false},
		{FloatValue(42), "42.0", false},
		{FloatValue(3.14), "3.14", false},
		{StringValue(""), "", true},
		{StringValue("abc"), "abc", true},
	}

	for _, test := range tests {
		got, isString := test.value.Unquoted()
		if got != test.want || isString != test.isString {
			t.Errorf("%v.Unquoted() = %q, %t; want %q, %t", test.value, got, isString, test.want, test.isString)
		}
	}
}
