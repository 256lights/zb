// Copyright (C) 1994-2024 Lua.org, PUC-Rio.
// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import (
	"math"
	"strconv"
	"strings"

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

// IsNumber reports whether the value is an integer.
func (v Value) IsInteger() bool {
	return v.t == valueTypeInteger
}

// IsString reports whether the value is a string.
func (v Value) IsString() bool {
	return v.t.noVariant() == valueTypeString
}

func (v Value) isShortString() bool {
	return v.t.noVariant() == valueTypeString && len(v.s) <= 40
}

// Bool reports whether the value tests true in Lua
// and whether the value is a boolean.
func (v Value) Bool() (_ bool, isBool bool) {
	return v.t != valueTypeNil && v.t != valueTypeFalse, v.t.noVariant() == valueTypeBoolean
}

// IsBoolean reports whether the value is a boolean.
func (v Value) IsBoolean() bool {
	return v.t.noVariant() == valueTypeBoolean
}

// Float64 returns the value as a floating-point number
// and reports whether the value is a number.
// No coercion occurs.
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
// and reports whether the value is a number.
// If the value is a floating point number
// and cannot be converted to an integer according to the mode,
// then ok will be false.
// No other coercion occurs.
func (v Value) Int64(mode FloatToIntegerMode) (_ int64, ok bool) {
	switch v.t {
	case valueTypeInteger:
		return int64(v.bits), true
	case valueTypeFloat:
		return FloatToInteger(math.Float64frombits(v.bits), OnlyIntegral)
	default:
		return 0, false
	}
}

// Unquoted returns the value as a string
// and reports whether the value is a string.
// Numbers are coerced to a string,
// but isString will be false.
func (v Value) Unquoted() (s string, isString bool) {
	switch v.t {
	case valueTypeString:
		return v.s, true
	case valueTypeFloat:
		f, _ := v.Float64()
		s = strconv.FormatFloat(f, 'g', -1, 64)
		if !strings.ContainsAny(s, ".e") {
			s += ".0"
		}
		return s, false
	case valueTypeInteger:
		i, _ := v.Int64(OnlyIntegral)
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

// Equal returns whether two values are equivalent according to [Lua equality].
//
// [Lua equality]: https://lua.org/manual/5.4/manual.html#3.4.4
func (v Value) Equal(v2 Value) bool {
	switch v.t {
	case valueTypeNil, valueTypeFalse, valueTypeTrue:
		return v.t == v2.t
	case valueTypeFloat:
		f1, _ := v.Float64()
		f2, ok := v2.Float64()
		return ok && f1 == f2
	case valueTypeInteger:
		i1, _ := v.Int64(OnlyIntegral)
		i2, ok := v2.Int64(OnlyIntegral)
		return ok && i1 == i2
	case valueTypeString:
		return v2.IsString() && v.s == v2.s
	default:
		return false
	}
}

// FloatToIntegerMode is an enumeration of rounding modes for [FloatToInteger].
type FloatToIntegerMode int

// Rounding modes.
const (
	// OnlyIntegral does not perform rounding
	// and only accepts integral values.
	OnlyIntegral FloatToIntegerMode = iota
	// Floor rounds to the greatest integer value less than or equal to the number.
	Floor
	// Ceil rounds to the least integer value greater than or equal to the number.
	Ceil
)

// FloatToInteger attempts to convert a floating-point number to an integer,
// rounding according to the given mode.
func FloatToInteger(n float64, mode FloatToIntegerMode) (_ int64, ok bool) {
	f := math.Floor(n)
	if f != n {
		switch mode {
		case OnlyIntegral:
			return 0, false
		case Ceil:
			// Convert floor to ceil.
			f += 1
		}
	}

	// Comparison is tricky here:
	// math.MinInt64 always has an exact representation as a float,
	// but math.MaxInt64 may not.
	ok = math.MinInt64 <= f && f < -math.MinInt64
	if !ok {
		return 0, false
	}
	return int64(f), true
}
