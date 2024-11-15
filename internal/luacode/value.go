// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import (
	"math"
	"strconv"

	"zb.256lights.llc/pkg/internal/lualex"
)

type valueType byte

const (
	valueTypeNil     valueType = 0
	valueTypeBoolean valueType = 1
	valueTypeNumber  valueType = 3
	valueTypeString  valueType = 4
)

// Variants.
const (
	valueTypeFalse   = valueTypeBoolean
	valueTypeTrue    = valueTypeBoolean | (1 << 4)
	valueTypeFloat   = valueTypeNumber
	valueTypeInteger = valueTypeNumber | (1 << 4)
)

func (t valueType) noVariant() valueType {
	return t & 0x0f
}

// Value is a subset of Lua values that can be used as constants:
// nil, booleans, floats, integers, and strings.
// The zero value is nil.
// Values can be compared for equality with the == operator.
type Value struct {
	bits uint64
	s    string
	t    valueType
}

// BoolValue converts a boolean to a [Value].
func BoolValue(b bool) Value {
	if b {
		return Value{t: valueTypeTrue}
	} else {
		return Value{t: valueTypeFalse}
	}
}

// IntegerValue converts an integer to a [Value].
func IntegerValue(i int64) Value {
	return Value{
		t:    valueTypeInteger,
		bits: uint64(i),
	}
}

// FloatValue converts a floating-point number to a [Value].
func FloatValue(f float64) Value {
	return Value{
		t:    valueTypeFloat,
		bits: math.Float64bits(f),
	}
}

// StringValue converts a string to a [Value].
func StringValue(s string) Value {
	return Value{
		t: valueTypeString,
		s: s,
	}
}

// IsNil reports whether v is the zero value.
func (v Value) IsNil() bool {
	return v.t == valueTypeNil
}

// IsNumber reports whether the value is a number.
func (v Value) IsNumber() bool {
	return v.t.noVariant() == valueTypeNumber
}

// IsString reports whether the value is a string.
func (v Value) IsString() bool {
	return v.t.noVariant() == valueTypeString
}

// Bool reports whether the value tests true in Lua
// and whether the value is a boolean.
func (v Value) Bool() (_ bool, isBool bool) {
	return v.t != valueTypeNil && v.t != valueTypeFalse, v.t.noVariant() == valueTypeBoolean
}

// Float64 returns the value as a floating-point number
// and reports whether the value is a number.
// Strings are not coerced to numbers.
func (v Value) Float64() (_ float64, isNumber bool) {
	switch v.t {
	case valueTypeInteger:
		return float64(int64(v.bits)), true
	case valueTypeFloat:
		return math.Float64frombits(v.bits), true
	default:
		return 0, false
	}
}

// Int64 returns the value as an integer
// and reports whether the value is an integer.
// Strings are not coerced to integers.
func (v Value) Int64() (_ int64, isInteger bool) {
	switch v.t {
	case valueTypeInteger:
		return int64(v.bits), true
	case valueTypeFloat:
		return int64(math.Float64frombits(v.bits)), false
	default:
		return 0, false
	}
}

// Unquoted returns the value as a string
// and reports whether the value is a string.
// Numbers be coerced to a string,
// but isString will be false.
func (v Value) Unquoted() (s string, isString bool) {
	switch v.t {
	case valueTypeString:
		return v.s, true
	case valueTypeFloat:
		f, _ := v.Float64()
		return strconv.FormatFloat(f, 'g', -1, 64), false
	case valueTypeInteger:
		i, _ := v.Int64()
		return strconv.FormatInt(i, 10), false
	default:
		return "", false
	}
}

// String returns the value as a Lua constant.
func (v Value) String() string {
	switch v.t {
	case valueTypeNil:
		return "nil"
	case valueTypeFalse:
		return "false"
	case valueTypeTrue:
		return "true"
	case valueTypeFloat, valueTypeInteger:
		s, _ := v.Unquoted()
		return s
	case valueTypeString:
		return lualex.Quote(v.s)
	default:
		return "<invalid value>"
	}
}