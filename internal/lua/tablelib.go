// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"zb.256lights.llc/pkg/internal/luacode"
	"zb.256lights.llc/pkg/sets"
)

// TableLibraryName is the conventional identifier for the [table manipulation library].
//
// [table manipulation library]: https://www.lua.org/manual/5.4/manual.html#6.6
const TableLibraryName = "table"

// OpenTable is a [Function] that loads the [table manipulation library].
// This function is intended to be used as an argument to [Require].
//
// [table manipulation library]: https://www.lua.org/manual/5.4/manual.html#6.6
func OpenTable(ctx context.Context, l *State) (int, error) {
	NewLib(l, map[string]Function{
		"concat": tableConcat,
		"insert": tableInsert,
		"move":   tableMove,
		"pack":   tablePack,
		"remove": tableRemove,
		"sort":   tableSort,
		"unpack": tableUnpack,
	})
	return 1, nil
}

func tableConcat(ctx context.Context, l *State) (int, error) {
	if err := checkTable(l, 1, luacode.TagMethodIndex, luacode.TagMethodLen); err != nil {
		return 0, err
	}
	last, err := Len(ctx, l, 1)
	if err != nil {
		return 0, err
	}
	separator := ""
	var separatorContext sets.Set[string]
	if !l.IsNoneOrNil(2) {
		var err error
		separator, err = CheckString(l, 2)
		if err != nil {
			return 0, err
		}
		separatorContext = l.StringContext(2)
	}
	first := int64(1)
	if !l.IsNoneOrNil(3) {
		var err error
		first, err = CheckInteger(l, 3)
		if err != nil {
			return 0, err
		}
	}
	if !l.IsNoneOrNil(4) {
		var err error
		last, err = CheckInteger(l, 4)
		if err != nil {
			return 0, err
		}
	}

	resultContext := make(sets.Set[string])
	if first < last {
		resultContext.AddSeq(separatorContext.All())
	}
	sb := new(strings.Builder)
	add := func(i int64) error {
		defer l.SetTop(l.Top())
		tp, err := l.Index(ctx, 1, i)
		if err != nil {
			return err
		}
		if tp != TypeString && tp != TypeNumber {
			return fmt.Errorf("%sinvalid value (%s) at index %d in table for 'concat'",
				Where(l, 1), tp.String(), i)
		}
		s, _ := l.ToString(-1)
		sb.WriteString(s)
		resultContext.AddSeq(l.StringContext(-1).All())
		return nil
	}

	var i int64
	for i = first; i < last; i++ {
		if err := add(i); err != nil {
			return 0, err
		}
		sb.WriteString(separator)
	}
	// We split the loop so we can't overflow i
	// and don't have to branch for separators.
	if i == last {
		if err := add(i); err != nil {
			return 0, err
		}
	}

	l.PushStringContext(sb.String(), resultContext)
	return 1, nil
}

func tableInsert(ctx context.Context, l *State) (int, error) {
	err := checkTable(l, 1, luacode.TagMethodIndex, luacode.TagMethodNewIndex, luacode.TagMethodLen)
	if err != nil {
		return 0, err
	}
	last, err := Len(ctx, l, 1)
	if err != nil {
		return 0, err
	}
	firstEmpty := last + 1

	var position int64
	switch l.Top() {
	case 2:
		position = firstEmpty
	case 3:
		position, err = CheckInteger(l, 2)
		if err != nil {
			return 0, err
		}
		if position-1 >= firstEmpty {
			return 0, NewArgError(l, 2, "position out of bounds")
		}
		// Move up elements.
		for i := firstEmpty; i > position; i-- {
			if _, err := l.Index(ctx, 1, i-1); err != nil {
				return 0, err
			}
			if err := l.SetIndex(ctx, 1, i); err != nil {
				return 0, err
			}
		}
	default:
		return 0, fmt.Errorf("%swrong number of arguments to 'insert'", Where(l, 1))
	}

	if err := l.SetIndex(ctx, 1, position); err != nil {
		return 0, err
	}
	return 0, nil
}

func tableMove(ctx context.Context, l *State) (int, error) {
	f, err := CheckInteger(l, 2)
	if err != nil {
		return 0, err
	}
	e, err := CheckInteger(l, 3)
	if err != nil {
		return 0, err
	}
	t, err := CheckInteger(l, 4)
	if err != nil {
		return 0, err
	}
	dstTableArg := 1
	if !l.IsNoneOrNil(5) {
		dstTableArg = 5
	}
	if err := checkTable(l, 1, luacode.TagMethodIndex); err != nil {
		return 0, err
	}
	if err := checkTable(l, dstTableArg, luacode.TagMethodNewIndex); err != nil {
		return 0, err
	}

	if e >= f {
		if f <= 0 && e >= math.MaxInt64+f {
			return 0, NewArgError(l, 3, "too many elements to move")
		}
		n := e - f + 1
		if t > math.MaxInt64-n+1 {
			return 0, NewArgError(l, 3, "destination wrap around")
		}
		useAscending, err := tableMoveCanUseAscendingLoop(ctx, l, f, e, t, dstTableArg)
		if err != nil {
			return 0, err
		}
		if useAscending {
			for i := range n {
				if _, err := l.Index(ctx, 1, f+i); err != nil {
					return 0, err
				}
				if err := l.SetIndex(ctx, dstTableArg, t+i); err != nil {
					return 0, err
				}
			}
		} else {
			for i := n - 1; i >= 0; i-- {
				if _, err := l.Index(ctx, 1, f+i); err != nil {
					return 0, err
				}
				if err := l.SetIndex(ctx, dstTableArg, t+i); err != nil {
					return 0, err
				}
			}
		}
	}

	l.PushValue(dstTableArg)
	return 1, nil
}

func tableMoveCanUseAscendingLoop(ctx context.Context, l *State, f, e, t int64, dstTableArg int) (bool, error) {
	if t > e || t <= f {
		return true, nil
	}
	if dstTableArg != 1 {
		return false, nil
	}
	eq, err := l.Compare(ctx, 1, dstTableArg, Equal)
	if err != nil {
		return false, err
	}
	return !eq, nil
}

func tablePack(ctx context.Context, l *State) (int, error) {
	n := l.Top()
	l.CreateTable(n, 1)
	l.Insert(1)
	for i := n; i >= 1; i-- {
		l.RawSetIndex(1, int64(i))
	}
	l.PushInteger(int64(n))
	l.RawSetField(1, "n")
	return 1, nil
}

func tableRemove(ctx context.Context, l *State) (int, error) {
	err := checkTable(l, 1, luacode.TagMethodIndex, luacode.TagMethodNewIndex, luacode.TagMethodLen)
	if err != nil {
		return 0, err
	}
	size, err := Len(ctx, l, 1)
	if err != nil {
		return 0, err
	}
	position := size
	if !l.IsNoneOrNil(2) {
		var err error
		position, err = CheckInteger(l, 2)
		if err != nil {
			return 0, err
		}
	}
	if position != size && uint64(position)-1 > uint64(size) {
		return 0, NewArgError(l, 2, "position out of bounds")
	}

	// Push the removed element onto the stack to be returned later.
	if _, err := l.Index(ctx, 1, position); err != nil {
		return 0, err
	}
	// Move elements downward.
	for ; position < size; position++ {
		if _, err := l.Index(ctx, 1, position+1); err != nil {
			return 0, err
		}
		if err := l.SetIndex(ctx, 1, position); err != nil {
			return 0, err
		}
	}
	// Clear last element.
	l.PushNil()
	if err := l.SetIndex(ctx, 1, position); err != nil {
		return 0, nil
	}
	// Return the removed element (pushed at the beginning).
	return 1, nil
}

func tableUnpack(ctx context.Context, l *State) (int, error) {
	i := int64(1)
	if !l.IsNoneOrNil(2) {
		var err error
		i, err = CheckInteger(l, 2)
		if err != nil {
			return 0, err
		}
	}
	var e int64
	if !l.IsNoneOrNil(3) {
		var err error
		e, err = CheckInteger(l, 3)
		if err != nil {
			return 0, err
		}
	} else {
		var err error
		e, err = Len(ctx, l, 1)
		if err != nil {
			return 0, err
		}
	}
	if i > e {
		return 0, nil
	}
	n := uint64(e) - uint64(i)
	if n >= math.MaxInt || !l.CheckStack(int(n+1)) {
		return 0, fmt.Errorf("%stoo many results to unpack", Where(l, 1))
	}

	for ; i < e; i++ {
		if _, err := l.Index(ctx, 1, i); err != nil {
			return 0, err
		}
	}
	// Split last iteration of loop to avoid overflows.
	if _, err := l.Index(ctx, 1, e); err != nil {
		return 0, err
	}

	return int(n + 1), nil
}

func tableSort(ctx context.Context, l *State) (int, error) {
	err := checkTable(l, 1, luacode.TagMethodIndex, luacode.TagMethodNewIndex, luacode.TagMethodLen)
	if err != nil {
		return 0, err
	}
	n, err := Len(ctx, l, 1)
	if err != nil {
		return 0, err
	}
	if n < 1 {
		return 0, err
	}
	if n > math.MaxInt {
		return 0, NewArgError(l, 1, "array too big")
	}
	if tp := l.Type(2); tp != TypeNone && tp != TypeNil && tp != TypeFunction {
		return 0, NewTypeError(l, 2, TypeFunction.String())
	}
	l.SetTop(2)

	sorter := &tableSorter{
		ctx: ctx,
		l:   l,
		n:   int(n),
	}
	sort.Sort(sorter)
	return 0, sorter.err
}

// tableSorter is the helper type that implements [sort.Interface]
// for [tableSort].
type tableSorter struct {
	ctx context.Context
	l   *State
	n   int
	err error
}

func (ts *tableSorter) Len() int {
	return ts.n
}

func (ts *tableSorter) Less(i, j int) bool {
	if ts.err != nil {
		// If we errored out, pretend everything is sorted.
		return i < j
	}
	defer ts.l.SetTop(ts.l.Top())
	hasCompareFunction := !ts.l.IsNoneOrNil(2)
	if hasCompareFunction {
		ts.l.PushValue(2)
	}
	if _, ts.err = ts.l.Index(ts.ctx, 1, int64(1+i)); ts.err != nil {
		return i < j
	}
	if _, ts.err = ts.l.Index(ts.ctx, 1, int64(1+j)); ts.err != nil {
		return i < j
	}
	if hasCompareFunction {
		ts.err = ts.l.Call(ts.ctx, 2, 1)
		if ts.err != nil {
			return i < j
		}
		return ts.l.ToBoolean(-1)
	}
	var less bool
	less, ts.err = ts.l.Compare(ts.ctx, -2, -1, Less)
	if ts.err != nil {
		return i < j
	}
	return less
}

func (ts *tableSorter) Swap(i, j int) {
	if ts.err != nil {
		return
	}
	defer ts.l.SetTop(ts.l.Top())
	if _, ts.err = ts.l.Index(ts.ctx, 1, int64(1+i)); ts.err != nil {
		return
	}
	if _, ts.err = ts.l.Index(ts.ctx, 1, int64(1+j)); ts.err != nil {
		return
	}
	if ts.err = ts.l.SetIndex(ts.ctx, 1, int64(1+i)); ts.err != nil {
		return
	}
	if ts.err = ts.l.SetIndex(ts.ctx, 1, int64(1+j)); ts.err != nil {
		return
	}
}

func checkTable(l *State, arg int, methods ...luacode.TagMethod) error {
	if l.Type(arg) == TypeTable {
		return nil
	}
	defer l.SetTop(l.Top())
	if !l.Metatable(arg) {
		return NewTypeError(l, arg, TypeTable.String())
	}
	for _, m := range methods {
		if l.RawField(-1, m.String()) == TypeNil {
			return NewTypeError(l, arg, m.String())
		}
	}
	return nil
}
