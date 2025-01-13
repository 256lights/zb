// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import (
	"math"
	"testing"
)

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
		{FloatValue(math.NaN()), "nan", false},
		{FloatValue(math.Inf(1)), "inf", false},
		{FloatValue(math.Inf(-1)), "-inf", false},
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

func TestValueEqual(t *testing.T) {
	tests := []struct {
		v1, v2 Value
		want   bool
	}{
		{Value{}, Value{}, true},
		{BoolValue(false), Value{}, false},
		{BoolValue(true), Value{}, false},
		{IntegerValue(0), Value{}, false},
		{FloatValue(0), Value{}, false},
		{StringValue(""), Value{}, false},
		{BoolValue(false), BoolValue(false), true},
		{BoolValue(true), BoolValue(true), true},
		{BoolValue(true), BoolValue(false), false},
		{IntegerValue(42), IntegerValue(42), true},
		{IntegerValue(42), IntegerValue(-42), false},
		{IntegerValue(42), FloatValue(42), true},
		{IntegerValue(math.MaxInt64 - 1023), FloatValue(math.MaxInt64 - 1023), true},
		{IntegerValue(math.MinInt64), FloatValue(math.MinInt64), true},
		{IntegerValue(42), FloatValue(-42), false},
		{FloatValue(3.14), FloatValue(3.14), true},
		{FloatValue(math.NaN()), FloatValue(42), false},
		{FloatValue(math.NaN()), FloatValue(math.NaN()), false},
		{StringValue(""), StringValue(""), true},
		{StringValue(""), StringValue("123"), false},
		{StringValue("123"), StringValue("123"), true},
		{StringValue("123"), StringValue("456"), false},
		{StringValue("123"), IntegerValue(123), false},

		// Float values that can't be represented as an integer.
		{IntegerValue(math.MaxInt64), FloatValue(math.MaxInt64), false},
		{IntegerValue(math.MinInt64), FloatValue(math.MinInt64 - 1025), false},
	}

	for _, test := range tests {
		if got := test.v1.Equal(test.v2); got && !test.want {
			t.Errorf("%v == %v", test.v1, test.v2)
		} else if !got && test.want {
			t.Errorf("%v != %v", test.v1, test.v2)
		}

		if got := test.v2.Equal(test.v1); got && !test.want {
			t.Errorf("%v == %v", test.v2, test.v1)
		} else if !got && test.want {
			t.Errorf("%v != %v", test.v2, test.v1)
		}
	}
}

func TestValueIdenticalTo(t *testing.T) {
	identicalTests := []Value{
		{},
		BoolValue(false),
		BoolValue(true),
		IntegerValue(42),
		IntegerValue(0),
		IntegerValue(-42),
		FloatValue(3.14),
		FloatValue(42),
		FloatValue(math.Inf(1)),
		FloatValue(math.Inf(-1)),
		FloatValue(math.NaN()),
		FloatValue(math.MinInt64),
		FloatValue(math.MaxInt64),
		StringValue(""),
		StringValue("abc"),
	}
	notIdenticalTests := []struct {
		v1, v2 Value
	}{
		{BoolValue(false), Value{}},
		{BoolValue(true), Value{}},
		{IntegerValue(0), Value{}},
		{FloatValue(0), Value{}},
		{StringValue(""), Value{}},
		{BoolValue(false), BoolValue(true)},
		{IntegerValue(123), IntegerValue(-123)},
		{FloatValue(123), FloatValue(-123)},
		{FloatValue(3.14), FloatValue(-3.14)},
		{FloatValue(math.Inf(1)), FloatValue(math.Inf(-1))},
		{StringValue("123"), StringValue("456")},
		{IntegerValue(123), FloatValue(123)},
		{IntegerValue(123), StringValue("123")},
		{FloatValue(123), StringValue("123")},
	}

	for _, v := range identicalTests {
		if !v.IdenticalTo(v) {
			t.Errorf("(%v).IdenticalTo(%v) = false; want true", v, v)
		}
	}
	for _, test := range notIdenticalTests {
		if test.v1.IdenticalTo(test.v2) {
			t.Errorf("(%v).IdenticalTo(%v) = true; want false", test.v1, test.v2)
		}
		if test.v2.IdenticalTo(test.v1) {
			t.Errorf("(%v).IdenticalTo(%v) = true; want false", test.v2, test.v1)
		}
	}
}
