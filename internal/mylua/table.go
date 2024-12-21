// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package mylua

import (
	"errors"
	"math"
	"slices"
	"sort"

	"zb.256lights.llc/pkg/internal/luacode"
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

// clear removes all entries from the table,
// but retains the space allocated for the table.
// It does not remove the table's metatable association.
func (tab *table) clear() {
	clear(tab.entries)
	tab.entries = tab.entries[:0]
}

type tableEntry struct {
	key, value value
}

func findEntry(entries []tableEntry, key value) (int, bool) {
	return slices.BinarySearchFunc(entries, key, func(e tableEntry, key value) int {
		return compareValues(e.key, key)
	})
}
