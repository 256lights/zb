// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"cmp"
	"fmt"
	"math"
	"sync"

	"zb.256lights.llc/pkg/internal/luacode"
	"zb.256lights.llc/pkg/internal/lualex"
	"zb.256lights.llc/pkg/sets"
)

// Type is an enumeration of Lua data types.
type Type int

// TypeNone is the value returned from [State.Type]
// for a non-valid but acceptable index.
const TypeNone Type = -1

// Value types.
const (
	TypeNil           Type = 0
	TypeBoolean       Type = 1
	TypeLightUserdata Type = 2
	TypeNumber        Type = 3
	TypeString        Type = 4
	TypeTable         Type = 5
	TypeFunction      Type = 6
	TypeUserdata      Type = 7
	TypeThread        Type = 8
)

// String returns the name of the type encoded by the value tp.
func (tp Type) String() string {
	switch tp {
	case TypeNone:
		return "no value"
	case TypeNil:
		return "nil"
	case TypeBoolean:
		return "boolean"
	case TypeLightUserdata, TypeUserdata:
		return "userdata"
	case TypeNumber:
		return "number"
	case TypeString:
		return "string"
	case TypeTable:
		return "table"
	case TypeFunction:
		return "function"
	case TypeThread:
		return "thread"
	default:
		return fmt.Sprintf("lua.Type(%d)", int(tp))
	}
}

// value is the internal representation of a Lua value.
type value interface {
	valueType() Type
}

// valueType returns the [Type] of a [value].
func valueType(v value) Type {
	if v == nil {
		return TypeNil
	}
	return v.valueType()
}

// importConstant converts a compile-time constant to a [value].
func importConstant(v luacode.Value) value {
	switch {
	case v.IsNil():
		return nil
	case v.IsBoolean():
		b, _ := v.Bool()
		return booleanValue(b)
	case v.IsInteger():
		i, _ := v.Int64(luacode.OnlyIntegral)
		return integerValue(i)
	case v.IsNumber():
		f, _ := v.Float64()
		return floatValue(f)
	case v.IsString():
		s, _ := v.Unquoted()
		return stringValue{s: s}
	default:
		panic("unreachable")
	}
}

// exportNumericConstant converts a [floatValue] or an [integerValue]
// to a [luacode.Value].
func exportNumericConstant(v value) (_ luacode.Value, ok bool) {
	switch v := v.(type) {
	case floatValue:
		return luacode.FloatValue(float64(v)), true
	case integerValue:
		return luacode.IntegerValue(int64(v)), true
	default:
		return luacode.Value{}, false
	}
}

// compareValues returns
//
//   - -1 if v1 is less than v2,
//   - 0 if v1 equals v2,
//   - +1 if v1 is greater than v2.
//
// Values of differing types are compared by their [Type] values.
//
// For numbers, a NaN is considered less than any non-NaN,
// a NaN is considered equal to a NaN,
// and -0.0 is equal to 0.0.
// comparedWithNaN will be true if both arguments are numbers
// and at least one of them is a NaN.
//
// This is a superset of the comparisons performed by [Lua relational operators]
// for the purpose of providing a total ordering for tables.
//
// If you only need to check for equality, [valuesEqual] is more efficient.
//
// [Lua relational operators]: https://www.lua.org/manual/5.4/manual.html#3.4.4
func compareValues(v1, v2 value) (_ int, comparedWithNaN bool) {
	switch v1 := v1.(type) {
	case nil:
		return cmp.Compare(TypeNil, valueType(v2)), false
	case booleanValue:
		b2, ok := v2.(booleanValue)
		switch {
		case !ok:
			return cmp.Compare(TypeBoolean, valueType(v2)), false
		case bool(v1 && !b2):
			return 1, false
		case bool(!v1 && b2):
			return -1, false
		default:
			return 0, false
		}
	case floatValue:
		switch v2 := v2.(type) {
		case floatValue:
			return cmp.Compare(v1, v2), math.IsNaN(float64(v1)) || math.IsNaN(float64(v2))
		case integerValue:
			return -compareIntFloat(v2, v1), math.IsNaN(float64(v1))
		default:
			return cmp.Compare(TypeNumber, valueType(v2)), false
		}
	case integerValue:
		switch v2 := v2.(type) {
		case integerValue:
			return cmp.Compare(v1, v2), false
		case floatValue:
			return compareIntFloat(v1, v2), math.IsNaN(float64(v2))
		default:
			return cmp.Compare(TypeNumber, valueType(v2)), false
		}
	case stringValue:
		s2, ok := v2.(stringValue)
		if !ok {
			return cmp.Compare(TypeString, valueType(v2)), false
		}
		return cmp.Compare(v1.s, s2.s), false
	case *table:
		t2, ok := v2.(*table)
		if !ok {
			return cmp.Compare(TypeTable, valueType(v2)), false
		}
		return cmp.Compare(v1.id, t2.id), false
	case function:
		f2, ok := v2.(function)
		if !ok {
			return cmp.Compare(TypeFunction, valueType(v2)), false
		}
		return cmp.Compare(v1.functionID(), f2.functionID()), false
	case *userdata:
		u2, ok := v2.(*userdata)
		if !ok {
			return cmp.Compare(TypeTable, valueType(v2)), false
		}
		return cmp.Compare(v1.id, u2.id), false
	default:
		panic("unhandled type")
	}
}

// compareIntFloat returns
//
//   - -1 if i is less than f
//   - 0 if i equals f
//   - +1 if i is greater than f
//
// Like [cmp.Compare], NaN is considered less than any non-NaN,
// a NaN is considered equal to a NaN,
// and -0.0 is equal to 0.0.
func compareIntFloat(i integerValue, f floatValue) int {
	if i.fitsInFloat() {
		return cmp.Compare(floatValue(i), f)
	}

	floor := math.Floor(float64(f))
	if floor < math.MinInt64 {
		// f is less than any integer.
		return 1
	}
	if floor >= -math.MinInt64 {
		// f is greater than any integer.
		return -1
	}
	fi := integerValue(floor)
	switch {
	case i > fi:
		return 1
	case i == fi && floor == float64(f):
		return 0
	default:
		return -1
	}
}

// valuesEqual reports whether v1 and v2 are [primitively equal] —
// that is, whether they are equal in Lua without consulting the “__eq” metamethod.
// This involves less comparisons than [compareValues].
//
// [primitively equal]: https://www.lua.org/manual/5.4/manual.html#3.4.4
func valuesEqual(v1, v2 value) bool {
	switch v1 := v1.(type) {
	case nil:
		return v2 == nil
	case booleanValue:
		b2, ok := v2.(booleanValue)
		return ok && v1 == b2
	case floatValue:
		switch v2 := v2.(type) {
		case integerValue:
			i1, ok := v1.toInteger()
			return ok && i1 == v2
		case floatValue:
			return v1 == v2
		default:
			return false
		}
	case integerValue:
		switch v2 := v2.(type) {
		case integerValue:
			return v1 == v2
		case floatValue:
			i2, ok := v2.toInteger()
			return ok && v1 == i2
		default:
			return false
		}
	case stringValue:
		s2, ok := v2.(stringValue)
		return ok && v1.s == s2.s
	case *table, function, *userdata:
		return v1 == v2
	default:
		panic("unhandled type")
	}
}

// numericValue is an optional interface for types that implement [value]
// and can be [coerced] to a number.
//
// [coerced]: https://www.lua.org/manual/5.4/manual.html#3.4.3
type numericValue interface {
	value
	toNumber() (_ floatValue, ok bool)
	toInteger() (_ integerValue, ok bool)
}

var (
	_ numericValue = floatValue(0)
	_ numericValue = integerValue(0)
	_ numericValue = stringValue{}
)

// toNumber [coerces] a [value] to a floating-point number,
// returning the result and whether the conversion succeeded.
//
// [coerces]: https://www.lua.org/manual/5.4/manual.html#3.4.3
func toNumber(v value) (_ floatValue, isNumber bool) {
	nv, ok := v.(numericValue)
	if !ok {
		return 0, false
	}
	return nv.toNumber()
}

// toBoolean reports whether the value is anything except nil or a false [booleanValue].
func toBoolean(v value) bool {
	switch v := v.(type) {
	case nil:
		return false
	case booleanValue:
		return bool(v)
	default:
		return true
	}
}

type valueStringer interface {
	stringValue() stringValue
}

var (
	_ valueStringer = floatValue(0)
	_ valueStringer = integerValue(0)
	_ valueStringer = stringValue{}
)

func toString(v value) (_ stringValue, ok bool) {
	sv, ok := v.(valueStringer)
	if !ok {
		return stringValue{}, false
	}
	return sv.stringValue(), true
}

// lenValue is a [value] that has a defined "raw" length.
type lenValue interface {
	value
	len() integerValue
}

var (
	_ lenValue = (*table)(nil)
	_ lenValue = stringValue{}
)

// booleanValue is a boolean [value].
type booleanValue bool

func (v booleanValue) valueType() Type { return TypeBoolean }

// integerValue is an integer [value].
type integerValue int64

func (v integerValue) valueType() Type                 { return TypeNumber }
func (v integerValue) toNumber() (floatValue, bool)    { return floatValue(v), true }
func (v integerValue) toInteger() (integerValue, bool) { return v, true }

func (v integerValue) stringValue() stringValue {
	s, _ := luacode.IntegerValue(int64(v)).Unquoted()
	return stringValue{s: s}
}

func (v integerValue) fitsInFloat() bool {
	const float64MantissaBits = 53
	const maxIntegerFitsInFloat64 = 1 << float64MantissaBits
	return maxIntegerFitsInFloat64+uint64(v) <= 2*maxIntegerFitsInFloat64
}

// floatValue is a floating-point [value].
type floatValue float64

func (v floatValue) valueType() Type              { return TypeNumber }
func (v floatValue) toNumber() (floatValue, bool) { return v, true }

func (v floatValue) toInteger() (integerValue, bool) {
	i, ok := luacode.FloatToInteger(float64(v), luacode.OnlyIntegral)
	return integerValue(i), ok
}

func (v floatValue) stringValue() stringValue {
	s, _ := luacode.FloatValue(float64(v)).Unquoted()
	return stringValue{s: s}
}

// stringValue is a string [value].
// stringValues implement [numericValue] because they can be coerced to numbers.
//
// This interpreter's string values include an optional "context",
// which is a set of strings that are unioned upon concatenation.
// Their meaning is application-defined.
type stringValue struct {
	s       string
	context sets.Set[string]
}

func (v stringValue) valueType() Type {
	return TypeString
}

func (v stringValue) len() integerValue {
	return integerValue(len(v.s))
}

func (v stringValue) isEmpty() bool {
	return len(v.s) == 0 && len(v.context) == 0
}

func (v stringValue) stringValue() stringValue {
	return v
}

func (v stringValue) toNumber() (floatValue, bool) {
	f, err := lualex.ParseNumber(v.s)
	if err != nil {
		return 0, false
	}
	return floatValue(f), true
}

func (v stringValue) toInteger() (integerValue, bool) {
	if i, err := lualex.ParseInt(v.s); err == nil {
		return integerValue(i), true
	}
	f, err := lualex.ParseNumber(v.s)
	if err != nil {
		return 0, false
	}
	i, ok := luacode.FloatToInteger(f, luacode.OnlyIntegral)
	return integerValue(i), ok
}

type userdata struct {
	id         uint64
	x          any
	meta       *table
	userValues []value
}

func newUserdata(x any, numUserValues int) *userdata {
	return &userdata{
		id:         nextID(),
		x:          x,
		userValues: make([]value, numUserValues),
	}
}

func (u *userdata) valueType() Type {
	return TypeUserdata
}

var globalIDs struct {
	mu sync.Mutex
	n  uint64
}

func nextID() uint64 {
	globalIDs.mu.Lock()
	defer globalIDs.mu.Unlock()
	globalIDs.n++
	return globalIDs.n
}
