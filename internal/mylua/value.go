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

func valueType(v any) Type {
	switch v.(type) {
	case nil:
		return TypeNil
	case bool:
		return TypeBoolean
	case float64, int64:
		return TypeNumber
	case stringValue:
		return TypeString
	case *table:
		return TypeTable
	case function:
		return TypeFunction
	default:
		panic("unhandled type")
	}
}

func importConstant(v luacode.Value) any {
	switch {
	case v.IsNil():
		return nil
	case v.IsBoolean():
		b, _ := v.Bool()
		return b
	case v.IsInteger():
		i, _ := v.Int64(luacode.OnlyIntegral)
		return i
	case v.IsNumber():
		f, _ := v.Float64()
		return f
	case v.IsString():
		s, _ := v.Unquoted()
		return stringValue{s: s}
	default:
		panic("unreachable")
	}
}

func compareValues(v1, v2 any) int {
	switch v1 := v1.(type) {
	case nil:
		return cmp.Compare(TypeNil, valueType(v2))
	case bool:
		b2, ok := v2.(bool)
		switch {
		case !ok:
			return cmp.Compare(TypeBoolean, valueType(v2))
		case v1 && !b2:
			return 1
		case !v1 && b2:
			return -1
		default:
			return 0
		}
	case float64:
		switch v2.(type) {
		case int64, float64:
			f2, _ := toNumber(v2)
			return cmp.Compare(v1, f2)
		default:
			return cmp.Compare(TypeNumber, valueType(v2))
		}
	case int64:
		switch v2 := v2.(type) {
		case int64:
			return cmp.Compare(v1, v2)
		case float64:
			return cmp.Compare(float64(v1), v2)
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

func toNumber(v any) (_ float64, isNumber bool) {
	switch v := v.(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	case stringValue:
		f, err := lualex.ParseNumber(v.s)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func toBoolean(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	default:
		return true
	}
}

type table struct {
	id      uint64
	entries []tableEntry
}

func newTable() *table {
	return &table{id: nextID()}
}

// len returns a [border in the table].
// This is equivalent to the Lua length ("#") operator.
//
// [border in the table]: https://lua.org/manual/5.4/manual.html#3.4.7
func (tab *table) len() int64 {
	start, ok := findEntry(tab.entries, int64(1))
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
		case int64:
			return k > int64(maxKey)
		case float64:
			return k > float64(maxKey)
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
		_, found := findEntry(searchSpace, int64(i)+2)
		return !found
	})
	return int64(i) + 1
}

func (tab *table) get(key any) any {
	i, found := findEntry(tab.entries, key)
	if !found {
		return nil
	}
	return tab.entries[i].value
}

func (tab *table) set(key, value any) error {
	switch k := key.(type) {
	case nil:
		return errors.New("table index is nil")
	case float64:
		if math.IsNaN(k) {
			return errors.New("table index is NaN")
		}
		if start, ok := luacode.FloatToInteger(k, luacode.OnlyIntegral); ok {
			key = start
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

type tableEntry struct {
	key, value any
}

func findEntry(entries []tableEntry, key any) (int, bool) {
	return slices.BinarySearchFunc(entries, key, func(e tableEntry, key any) int {
		return compareValues(e.key, key)
	})
}

type stringValue struct {
	s       string
	context sets.Set[string]
}

type goFunction struct {
	id       uint64
	cb       Function
	upvalues []any
}

func (f goFunction) functionID() uint64 {
	return f.id
}

func (f goFunction) upvaluesSlice() []any {
	return f.upvalues
}

type luaFunction struct {
	id       uint64
	proto    *luacode.Prototype
	upvalues []any
}

func (f luaFunction) functionID() uint64 {
	return f.id
}

func (f luaFunction) upvaluesSlice() []any {
	return f.upvalues
}

type function interface {
	functionID() uint64
	upvaluesSlice() []any
}

var (
	_ function = goFunction{}
	_ function = luaFunction{}
)

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
