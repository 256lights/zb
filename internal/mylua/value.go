// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package mylua

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
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
// For [floatValue], a NaN is considered less than any non-NaN,
// a NaN is considered equal to a NaN,
// and -0.0 is equal to 0.0.
func compareValues(v1, v2 value) int {
	switch v1 := v1.(type) {
	case nil:
		return cmp.Compare(TypeNil, valueType(v2))
	case booleanValue:
		b2, ok := v2.(booleanValue)
		switch {
		case !ok:
			return cmp.Compare(TypeBoolean, valueType(v2))
		case bool(v1 && !b2):
			return 1
		case bool(!v1 && b2):
			return -1
		default:
			return 0
		}
	case floatValue:
		switch v2.(type) {
		case integerValue, floatValue:
			f2, _ := toNumber(v2)
			return cmp.Compare(v1, f2)
		default:
			return cmp.Compare(TypeNumber, valueType(v2))
		}
	case integerValue:
		switch v2 := v2.(type) {
		case integerValue:
			return cmp.Compare(v1, v2)
		case floatValue:
			return cmp.Compare(floatValue(v1), v2)
		default:
			return cmp.Compare(TypeNumber, valueType(v2))
		}
	case stringValue:
		s2, ok := v2.(stringValue)
		if !ok {
			return cmp.Compare(TypeString, valueType(v2))
		}
		return cmp.Compare(v1.s, s2.s)
	case *table:
		t2, ok := v2.(*table)
		if !ok {
			return cmp.Compare(TypeTable, valueType(v2))
		}
		return cmp.Compare(v1.id, t2.id)
	case function:
		f2, ok := v2.(function)
		if !ok {
			return cmp.Compare(TypeFunction, valueType(v2))
		}
		return cmp.Compare(v1.functionID(), f2.functionID())
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

type table struct {
	id      uint64
	entries []tableEntry
	meta    *table
}

func newTable(capacity int) *table {
	tab := &table{id: nextID()}
	if capacity > 0 {
		tab.entries = make([]tableEntry, 0, capacity)
	}
	return tab
}

func (tab *table) valueType() Type {
	return TypeTable
}

// len returns a [border in the table].
// This is equivalent to the Lua length ("#") operator.
//
// [border in the table]: https://lua.org/manual/5.4/manual.html#3.4.7
func (tab *table) len() integerValue {
	if tab == nil {
		return 0
	}
	start, ok := findEntry(tab.entries, integerValue(1))
	if !ok {
		return 0
	}

	// Find the last entry with a numeric key in the possible range.
	// For example, if len(tab.entries) - start == 3,
	// then we can ignore any values greater than 3
	// because there necessarily must be a border before any of those values.
	maxKey := len(tab.entries) - start
	searchSpace := tab.entries[start+1:] // Can skip 1.
	n := sort.Search(len(searchSpace), func(i int) bool {
		switch k := searchSpace[i].key.(type) {
		case integerValue:
			return k > integerValue(maxKey)
		case floatValue:
			return k > floatValue(maxKey)
		default:
			return true
		}
	})
	searchSpace = searchSpace[:n]
	// Maximum key cannot be larger than the number of elements
	// (plus one, because we excluded the 1 entry).
	maxKey = n + 1

	// Instead of searching over slice indices,
	// we binary search over the key space to find the first i
	// for which table[i + 1] == nil.
	i := sort.Search(maxKey, func(i int) bool {
		_, found := findEntry(searchSpace, integerValue(i)+2)
		return !found
	})
	return integerValue(i) + 1
}

func (tab *table) get(key value) value {
	if tab == nil {
		return nil
	}
	i, found := findEntry(tab.entries, key)
	if !found {
		return nil
	}
	return tab.entries[i].value
}

func (tab *table) set(key, value value) error {
	switch k := key.(type) {
	case nil:
		return errors.New("table index is nil")
	case floatValue:
		if math.IsNaN(float64(k)) {
			return errors.New("table index is NaN")
		}
		if i, ok := luacode.FloatToInteger(float64(k), luacode.OnlyIntegral); ok {
			key = integerValue(i)
		}
	}

	i, found := findEntry(tab.entries, key)
	switch {
	case found && value != nil:
		tab.entries[i].value = value
	case found && value == nil:
		tab.entries = slices.Delete(tab.entries, i, i+1)
	case !found && value != nil:
		tab.entries = slices.Insert(tab.entries, i, tableEntry{
			key:   key,
			value: value,
		})
	}
	return nil
}

// setExisting looks up a key in the table
// and changes or removes the value for the key as appropriate
// if the key was found and returns true.
// Otherwise, if the key was not found,
// then setExisting does nothing and returns false.
func (tab *table) setExisting(k, v value) bool {
	if tab == nil {
		return false
	}
	i, found := findEntry(tab.entries, k)
	if !found {
		return false
	}
	if v == nil {
		tab.entries = slices.Delete(tab.entries, i, i+1)
	} else {
		tab.entries[i].value = v
	}
	return true
}

type tableEntry struct {
	key, value value
}

func findEntry(entries []tableEntry, key value) (int, bool) {
	return slices.BinarySearchFunc(entries, key, func(e tableEntry, key value) int {
		return compareValues(e.key, key)
	})
}

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
	i, err := lualex.ParseInt(v.s)
	if err != nil {
		return 0, false
	}
	return integerValue(i), true
}

type function interface {
	value
	functionID() uint64
	upvaluesSlice() []upvalue
}

var (
	_ function = goFunction{}
	_ function = luaFunction{}
)

type goFunction struct {
	id       uint64
	cb       Function
	upvalues []upvalue
}

func (f goFunction) valueType() Type          { return TypeFunction }
func (f goFunction) functionID() uint64       { return f.id }
func (f goFunction) upvaluesSlice() []upvalue { return f.upvalues }

type luaFunction struct {
	id       uint64
	proto    *luacode.Prototype
	upvalues []upvalue
}

func (f luaFunction) valueType() Type          { return TypeFunction }
func (f luaFunction) functionID() uint64       { return f.id }
func (f luaFunction) upvaluesSlice() []upvalue { return f.upvalues }

type upvalue struct {
	p          *value
	stackIndex int
}

func stackUpvalue(i int) upvalue {
	return upvalue{stackIndex: i}
}

func standaloneUpvalue(v value) upvalue {
	return upvalue{
		p:          &v,
		stackIndex: -1,
	}
}

func (l *State) resolveUpvalue(uv upvalue) *value {
	if uv.p == nil {
		return &l.stack[uv.stackIndex]
	}
	return uv.p
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
