// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

//go:generate stringer -type=ComparisonOperator -linecomment -output=lua_string.go

package mylua

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"zb.256lights.llc/pkg/internal/luacode"
	"zb.256lights.llc/pkg/sets"
)

const (
	// minStack is the minimum number of elements a Go function can push onto the stack.
	minStack = 20

	maxStack = 1_000_000
)

const maxUpvalues = 256

const maxMetaDepth = 200

// MultipleReturns is the sentinel
// that indicates that an arbitrary number of result values are accepted.
const MultipleReturns = luacode.MultiReturn

// RegistryIndex is a pseudo-index to the [registry],
// a predefined table that can be used by any Go code
// to store whatever Lua values it needs to store.
//
// [registry]: https://www.lua.org/manual/5.4/manual.html#4.3
const RegistryIndex int = -maxStack - 1000

// Predefined values in the registry.
const (
	RegistryIndexGlobals int64 = 2
)

// UpvalueIndex returns the pseudo-index that represents the i-th upvalue
// of the running function.
// If i is outside the range [1, 256], UpvalueIndex panics.
func UpvalueIndex(i int) int {
	if i < 1 || i > maxUpvalues {
		panic("UpvalueIndex out of range")
	}
	return RegistryIndex - i
}

func isUpvalueIndex(idx int) bool {
	_, ok := upvalueFromIndex(idx)
	return ok
}

func upvalueFromIndex(idx int) (upvalue int, ok bool) {
	if idx >= RegistryIndex || idx < RegistryIndex-maxUpvalues {
		return 0, false
	}
	return RegistryIndex - idx, true
}

func isPseudo(i int) bool {
	return i <= RegistryIndex
}

// A State is a Lua execution environment.
// The zero value is a ready-to-use environment
// with an empty stack and and an empty global table.
//
// # Error Handling
//
// If the msgHandler argument to a [State] method is 0,
// then errors are returned as a Go error value.
// (This is in contrast to the C Lua implementation which pushes an error object onto the stack.)
// Otherwise, msgHandler is the stack index of a message handler.
// (This index cannot be a pseudo-index.)
// In case of runtime errors, this handler will be called with the error object
// and its return value will be returned on the stack by the [State] method being called.
// The return value's string value will be used as a Go error returned by the [State] method.
// Typically, the message handler is used to add more debug information to the error object,
// such as a stack traceback.
// Such information cannot be gathered after the return of a [State] method,
// since by then the stack will have been unwound.
type State struct {
	stack            []value
	registry         table
	callStack        []callFrame
	typeMetatables   [9]*table
	pendingVariables []*upvalue
	tbc              sets.Bit
}

func (l *State) init() {
	if cap(l.stack) < minStack {
		l.stack = slices.Grow(l.stack, minStack*2-len(l.stack))
	}
	if l.registry.id == 0 {
		l.registry = *newTable(1)
		l.registry.set(integerValue(RegistryIndexGlobals), newTable(0))
	}
	if len(l.callStack) == 0 {
		l.stack = append(l.stack, goFunction{
			id: nextID(),
		})
		l.callStack = append(l.callStack, callFrame{
			functionIndex: len(l.stack) - 1,
		})
	}
}

func (l *State) Close() error {
	return nil
}

// frame returns the top [callFrame] from the stack.
func (l *State) frame() *callFrame {
	return &l.callStack[len(l.callStack)-1]
}

func (l *State) stackIndex(idx int) (int, error) {
	if isPseudo(idx) {
		return -1, errors.New("pseudo-index not allowed")
	}
	if idx == 0 {
		return -1, errors.New("invalid index 0")
	}
	if idx < 0 {
		if idx < -l.Top() {
			return -1, fmt.Errorf("invalid index %d (top = %d)", idx, l.Top())
		}
		return len(l.stack) + idx, nil
	}
	i := l.frame().registerStart() + idx - 1
	if i >= cap(l.stack) {
		return i, fmt.Errorf("unacceptable index %d (capacity = %d)", idx, cap(l.stack)-l.frame().registerStart())
	}
	return i, nil
}

func (l *State) valueByIndex(idx int) (v value, valid bool, err error) {
	switch {
	case idx == RegistryIndex:
		return &l.registry, true, nil
	case isUpvalueIndex(idx):
		fv := l.stack[l.frame().functionIndex]
		f, ok := fv.(function)
		if !ok {
			return nil, false, fmt.Errorf("internal error: call frame missing function (found %T)", fv)
		}
		upvalues := f.upvaluesSlice()
		i, _ := upvalueFromIndex(idx)
		if i > len(upvalues) {
			return nil, false, nil
		}
		return *l.resolveUpvalue(upvalues[i-1]), true, nil
	case isPseudo(idx):
		return nil, false, fmt.Errorf("invalid pseudo-index (%d)", idx)
	default:
		i, err := l.stackIndex(idx)
		if err != nil {
			return nil, false, err
		}
		if i >= len(l.stack) {
			return nil, false, nil
		}
		return l.stack[i], true, nil
	}
}

// AbsIndex converts the acceptable index idx
// into an equivalent absolute index
// (that is, one that does not depend on the stack size).
// AbsIndex panics if idx is not an acceptable index.
func (l *State) AbsIndex(idx int) int {
	if isPseudo(idx) {
		return idx
	}
	l.init()
	i, err := l.stackIndex(idx)
	if err != nil {
		panic(err)
	}
	return i - l.frame().registerStart() + 1
}

// Top returns the index of the top element in the stack.
// Because indices start at 1,
// this result is equal to the number of elements in the stack;
// in particular, 0 means an empty stack.
func (l *State) Top() int {
	if len(l.callStack) == 0 {
		return 0
	}
	return max(len(l.stack)-l.frame().registerStart(), 0)
}

// SetTop accepts any index, or 0, and sets the stack top to this index.
// If the new top is greater than the old one,
// then the new elements are filled with nil.
// If idx is 0, then all stack elements are removed.
func (l *State) SetTop(idx int) {
	if isPseudo(idx) {
		panic("invalid new top")
	}
	l.init()
	base := l.frame().registerStart()
	var newTop int
	if idx >= 0 {
		newTop = base + idx
		if newTop > cap(l.stack) {
			panic("new top too large")
		}
	} else {
		newTop = len(l.stack) + idx + 1
		if newTop < base {
			panic("invalid new top")
		}
	}
	l.setTop(newTop)
}

// setTop sets the top of the stack to i.
// It does not close any upvalues or close any to-be-closed variables.
func (l *State) setTop(i int) {
	if i < len(l.stack) {
		clear(l.stack[i:])
	}
	l.stack = l.stack[:i]
}

// Pop pops n elements from the stack.
func (l *State) Pop(n int) {
	l.SetTop(-n - 1)
}

// Rotate rotates the stack elements
// between the valid index idx and the top of the stack.
// The elements are rotated n positions in the direction of the top, for a positive n,
// or -n positions in the direction of the bottom, for a negative n.
// If the absolute value of n is greater than the size of the slice being rotated,
// or if idx is a pseudo-index,
// Rotate panics.
func (l *State) Rotate(idx, n int) {
	l.init()
	i, err := l.stackIndex(idx)
	if err != nil {
		panic(err)
	}
	absN := n
	if n < 0 {
		absN = -n
	}
	if absN > len(l.stack)-i {
		panic("invalid rotation")
	}
	rotate(l.stack[i:], n)
}

// rotate rotates the elements of a slice
// n positions toward the end of the slice.
// n may be negative.
// If the absolute value of n is greater than len(s),
// then rotate panics.
func rotate[S ~[]E, E any](s S, n int) {
	var m int
	if n >= 0 {
		m = len(s) - n
	} else {
		m = -n
	}
	slices.Reverse(s[:m])
	slices.Reverse(s[m:])
	slices.Reverse(s)
}

// Insert moves the top element into the given valid index,
// shifting up the elements above this index to open space.
// If idx is a pseudo-index, Insert panics.
func (l *State) Insert(idx int) {
	l.Rotate(idx, 1)
}

// Remove removes the element at the given valid index,
// shifting down the elements above this index to fill the gap.
// This function cannot be called with a pseudo-index,
// because a pseudo-index is not an actual stack position.
func (l *State) Remove(idx int) {
	l.Rotate(idx, -1)
	l.Pop(1)
}

// CheckStack ensures that the stack has space for at least n extra elements,
// that is, that you can safely push up to n values into it.
// It returns false if it cannot fulfill the request,
// either because it would cause the stack to be greater than a fixed maximum size
// (typically at least several thousand elements)
// or because it cannot allocate memory for the extra space.
// This function never shrinks the stack;
// if the stack already has space for the extra elements, it is left unchanged.
func (l *State) CheckStack(n int) bool {
	l.init()
	return l.grow(len(l.stack) + n)
}

// grow ensures that the capacity of the stack is at least the given value,
// or returns false if it could not be fulfilled.
func (l *State) grow(wantTop int) bool {
	if wantTop <= cap(l.stack) {
		return true
	}
	if wantTop > maxStack {
		return false
	}
	l.stack = slices.Grow(l.stack, wantTop-len(l.stack))
	if cap(l.stack) > maxStack {
		l.stack = l.stack[:len(l.stack):maxStack]
	}
	return true
}

// IsNumber reports if the value at the given index is a number
// or a string convertible to a number.
func (l *State) IsNumber(idx int) bool {
	l.init()
	v, _, err := l.valueByIndex(idx)
	if err != nil {
		return false
	}
	_, ok := toNumber(v)
	return ok
}

// IsString reports if the value at the given index is a string
// or a number (which is always convertible to a string).
func (l *State) IsString(idx int) bool {
	l.init()
	v, _, err := l.valueByIndex(idx)
	if err != nil {
		return false
	}
	t := valueType(v)
	return t == TypeString || t == TypeNumber
}

// IsGoFunction reports if the value at the given index is a Go function.
func (l *State) IsGoFunction(idx int) bool {
	l.init()
	v, _, err := l.valueByIndex(idx)
	if err != nil {
		return false
	}
	_, ok := v.(goFunction)
	return ok
}

// IsInteger reports if the value at the given index is an integer
// (that is, the value is a number and is represented as an integer).
func (l *State) IsInteger(idx int) bool {
	l.init()
	v, _, err := l.valueByIndex(idx)
	if err != nil {
		return false
	}
	_, ok := v.(integerValue)
	return ok
}

// IsUserdata reports if the value at the given index is a userdata (either full or light).
func (l *State) IsUserdata(idx int) bool {
	l.init()
	// TODO(soon)
	return false
}

// Type returns the type of the value in the given valid index,
// or [TypeNone] for a non-valid but acceptable index.
func (l *State) Type(idx int) Type {
	l.init()
	v, valid, err := l.valueByIndex(idx)
	if err != nil {
		panic(err)
	}
	if !valid {
		return TypeNone
	}
	return valueType(v)
}

// IsFunction reports if the value at the given index is a function (either Go or Lua).
func (l *State) IsFunction(idx int) bool {
	l.init()
	v, _, err := l.valueByIndex(idx)
	return err == nil && valueType(v) == TypeFunction
}

// IsTable reports if the value at the given index is a table.
func (l *State) IsTable(idx int) bool {
	l.init()
	v, _, err := l.valueByIndex(idx)
	return err == nil && valueType(v) == TypeTable
}

// IsNil reports if the value at the given index is nil.
func (l *State) IsNil(idx int) bool {
	l.init()
	v, valid, err := l.valueByIndex(idx)
	return err == nil && valid && v == nil
}

// IsBoolean reports if the value at the given index is a boolean.
func (l *State) IsBoolean(idx int) bool {
	l.init()
	v, _, err := l.valueByIndex(idx)
	return err == nil && valueType(v) == TypeBoolean
}

// IsNone reports if the index is not valid.
func (l *State) IsNone(idx int) bool {
	l.init()
	_, valid, err := l.valueByIndex(idx)
	return err == nil && !valid
}

// IsNoneOrNil reports if the index is not valid or the value at this index is nil.
func (l *State) IsNoneOrNil(idx int) bool {
	l.init()
	v, _, err := l.valueByIndex(idx)
	return err == nil && v == nil
}

// ToNumber converts the Lua value at the given index to a floating point number.
// The Lua value must be a number or a [string convertible to a number];
// otherwise, ToNumber returns (0, false).
// ok is true if the operation succeeded.
//
// [string convertible to a number]: https://www.lua.org/manual/5.4/manual.html#3.4.3
func (l *State) ToNumber(idx int) (n float64, ok bool) {
	l.init()
	v, _, err := l.valueByIndex(idx)
	if err != nil {
		return 0, false
	}
	var fv floatValue
	fv, ok = toNumber(v)
	return float64(fv), ok
}

// ToInteger converts the Lua value at the given index to a signed 64-bit integer.
// The Lua value must be an integer, a number, or a [string convertible to an integer];
// otherwise, ToInteger returns (0, false).
// ok is true if the operation succeeded.
//
// [string convertible to an integer]: https://www.lua.org/manual/5.4/manual.html#3.4.3
func (l *State) ToInteger(idx int) (n int64, ok bool) {
	l.init()
	v, _, err := l.valueByIndex(idx)
	if err != nil {
		return 0, false
	}
	nv, ok := v.(numericValue)
	if !ok {
		return 0, false
	}
	i, ok := nv.toInteger()
	return int64(i), ok
}

// ToBoolean converts the Lua value at the given index to a boolean value.
// Like all tests in Lua,
// ToBoolean returns true for any Lua value different from false and nil;
// otherwise it returns false.
func (l *State) ToBoolean(idx int) bool {
	l.init()
	v, _, err := l.valueByIndex(idx)
	if err != nil {
		return false
	}
	return toBoolean(v)
}

// ToString converts the Lua value at the given index to a Go string.
// The Lua value must be a string or a number; otherwise, the function returns ("", false).
// If the value is a number, then ToString also changes the actual value in the stack to a string.
// (This change confuses [State.Next]
// when ToString is applied to keys during a table traversal.)
func (l *State) ToString(idx int) (s string, ok bool) {
	l.init()
	var p *value
	switch {
	case idx == RegistryIndex:
		return "", false
	case isUpvalueIndex(idx):
		fv := l.stack[l.frame().functionIndex]
		f, ok := fv.(function)
		if !ok {
			return "", false
		}
		upvalues := f.upvaluesSlice()
		i, _ := upvalueFromIndex(idx)
		if i > len(upvalues) {
			return "", false
		}
		p = l.resolveUpvalue(upvalues[i-1])
	case isPseudo(idx):
		return "", false
	default:
		i, err := l.stackIndex(idx)
		if err != nil || i >= len(l.stack) {
			return "", false
		}
		p = &l.stack[i]
	}

	switch v := (*p).(type) {
	case stringValue:
		return v.s, true
	case valueStringer:
		*p = v.stringValue()
		return s, true
	default:
		return "", false
	}
}

// StringContext returns any context values associated with the string at the given index.
// If the Lua value is not a string, the function returns nil.
func (l *State) StringContext(idx int) sets.Set[string] {
	l.init()
	v, _, err := l.valueByIndex(idx)
	if err != nil {
		return nil
	}
	if v, ok := v.(stringValue); ok {
		return v.context.Clone()
	}
	return nil
}

// RawLen returns the raw "length" of the value at the given index:
// for strings, this is the string length;
// for tables, this is the result of the length operator ('#') with no metamethods;
// For other values, RawLen returns 0.
func (l *State) RawLen(idx int) uint64 {
	l.init()
	v, _, err := l.valueByIndex(idx)
	if err != nil {
		panic(err)
	}
	lv, ok := v.(lenValue)
	if !ok {
		return 0
	}
	return uint64(lv.len())
}

// RawEqual reports whether the two values in the given indices
// are primitively equal (that is, equal without calling the __eq metamethod).
// If either index is invalid, then RawEqual reports false.
func (l *State) RawEqual(idx1, idx2 int) bool {
	l.init()
	v1, _, err := l.valueByIndex(idx1)
	if err != nil {
		return false
	}
	v2, _, err := l.valueByIndex(idx2)
	if err != nil {
		return false
	}
	return valuesEqual(v1, v2)
}

// ComparisonOperator is an enumeration of operators
// that can be used with [*State.Compare].
type ComparisonOperator int

// Defined [ComparisonOperator] values.
const (
	Equal       ComparisonOperator = iota // ==
	Less                                  // <
	LessOrEqual                           // <=
)

func (l *State) Compare(idx1, idx2 int, op ComparisonOperator, msgHandler int) (bool, error) {
	l.init()
	v1, _, err := l.valueByIndex(idx1)
	if err != nil {
		return false, err
	}
	v2, _, err := l.valueByIndex(idx2)
	if err != nil {
		return false, err
	}
	if msgHandler != 0 {
		return false, fmt.Errorf("TODO(someday): support message handlers")
	}
	return l.compare(op, v1, v2)
}

// compare returns the result of comparing v1 and v2 with the given operator
// according to Lua's full comparison rules (including metamethods).
func (l *State) compare(op ComparisonOperator, v1, v2 value) (bool, error) {
	switch op {
	case Equal:
		return l.equal(v1, v2)
	case Less, LessOrEqual:
		t1, t2 := valueType(v1), valueType(v2)
		if t1 == TypeNumber && t2 == TypeNumber || t1 == TypeString && t2 == TypeString {
			result := compareValues(v1, v2)
			return result < 0 || result == 0 && op == LessOrEqual, nil
		}
		var event luacode.TagMethod
		switch op {
		case Less:
			event = luacode.TagMethodLT
		case LessOrEqual:
			event = luacode.TagMethodLE
		default:
			panic("unreachable")
		}
		f := l.binaryMetamethod(v1, v2, event)
		if f == nil {
			// Neither value has the needed metamethod.
			tn1 := l.typeName(v1)
			tn2 := l.typeName(v2)
			if tn1 == tn2 {
				return false, fmt.Errorf("attempt to compare two %s values", tn1)
			}
			return false, fmt.Errorf("attempt to compare %s with %s", tn1, tn2)
		}
		result, err := l.call1(f, v1, v2)
		if err != nil {
			return false, err
		}
		return toBoolean(result), nil
	default:
		return false, fmt.Errorf("invalid %v", op)
	}
}

// equal reports whether v1 == v2 according to Lua's full equality rules (including metamethods).
func (l *State) equal(v1, v2 value) (bool, error) {
	// Values of different types are never equal.
	t1, t2 := valueType(v1), valueType(v2)
	if t1 != t2 {
		return false, nil
	}
	// If the values are primitively equal, then it's equal.
	if valuesEqual(v1, v2) {
		return true, nil
	}
	// Check __eq metamethod for types with individual metatables.
	if !(t1 == TypeTable || t1 == TypeUserdata) {
		return false, nil
	}
	f := l.binaryMetamethod(v1, v2, luacode.TagMethodEQ)
	if f == nil {
		// Neither value has an __eq metamethod.
		return false, nil
	}
	result, err := l.call1(f, v1, v2)
	if err != nil {
		return false, err
	}
	return toBoolean(result), nil
}

func (l *State) push(x value) {
	if len(l.stack) == cap(l.stack) {
		panic(errStackOverflow)
	}
	l.stack = append(l.stack, x)
}

// PushValue pushes a copy of the element at the given index onto the stack.
func (l *State) PushValue(idx int) {
	l.init()
	v, _, err := l.valueByIndex(idx)
	if err != nil {
		panic(err)
	}
	l.push(v)
}

// PushNil pushes a nil value onto the stack.
func (l *State) PushNil() {
	l.init()
	l.push(nil)
}

// PushNumber pushes a floating point number onto the stack.
func (l *State) PushNumber(n float64) {
	l.init()
	l.push(floatValue(n))
}

// PushInteger pushes an integer onto the stack.
func (l *State) PushInteger(i int64) {
	l.init()
	l.push(integerValue(i))
}

// PushString pushes a string onto the stack.
func (l *State) PushString(s string) {
	l.PushStringContext(s, nil)
}

// PushString pushes a string onto the stack
// with the given context arguments.
func (l *State) PushStringContext(s string, context sets.Set[string]) {
	l.init()
	l.push(stringValue{
		s:       s,
		context: context.Clone(),
	})
}

// PushBoolean pushes a boolean onto the stack.
func (l *State) PushBoolean(b bool) {
	l.init()
	l.push(booleanValue(b))
}

// A Function is a callback for a Lua function implemented in Go.
// A Go function receives its arguments from Lua in its stack in direct order
// (the first argument is pushed first).
// So, when the function starts,
// [State.Top] returns the number of arguments received by the function.
// The first argument (if any) is at index 1 and its last argument is at index [State.Top].
// To return values to Lua, a Go function just pushes them onto the stack,
// in direct order (the first result is pushed first),
// and returns in Go the number of results.
// Any other value in the stack below the results will be properly discarded by Lua.
// Like a Lua function, a Go function called by Lua can also return many results.
// To raise an error, return a Go error
// and the string result of its Error() method will be used as the error object.
type Function func(*State) (int, error)

// PushClosure pushes a Go closure onto the stack.
// n is how many upvalues this function will have,
// popped off the top of the stack.
// (When there are multiple upvalues, the first value is pushed first.)
// If n is negative or greater than 256, then PushClosure panics.
func (l *State) PushClosure(n int, f Function) {
	l.init()
	if n > maxUpvalues || n > l.Top() {
		panic("too many upvalues")
	}
	upvalueStart := len(l.stack) - n
	upvalues := make([]*upvalue, 0, n)
	for _, v := range l.stack[upvalueStart:] {
		upvalues = append(upvalues, closedUpvalue(v))
	}
	l.setTop(upvalueStart)
	l.push(goFunction{
		id:       nextID(),
		cb:       f,
		upvalues: upvalues,
	})
}

// Global pushes onto the stack the value of the global with the given name,
// returning the type of that value.
//
// As in Lua, this function may trigger a metamethod on the globals table
// for the "index" event.
// If there is any error, Global catches it,
// pushes nil or the error object (see Error Handling in [State]) onto the stack,
// and returns an error with [TypeNil].
func (l *State) Global(name string, msgHandler int) (Type, error) {
	if msgHandler != 0 {
		return TypeNil, fmt.Errorf("TODO(someday): support message handlers")
	}
	l.init()
	v, err := l.index(l.registry.get(integerValue(RegistryIndexGlobals)), stringValue{s: name})
	if err != nil {
		l.push(nil)
		return TypeNil, err
	}
	l.push(v)
	return valueType(v), nil
}

// Table pushes onto the stack the value t[k],
// where t is the value at the given index
// and k is the value on the top of the stack.
// Returns the type of the pushed value.
//
// This function pops the key from the stack,
// pushing the resulting value in its place.
//
// As in Lua, this function may trigger a metamethod for the "index" event.
// If there is any error, Table catches it,
// pushes nil or the error object (see Error Handling in [State]) onto the stack,
// and returns an error with [TypeNil].
// Table always removes the key from the stack.
func (l *State) Table(idx, msgHandler int) (Type, error) {
	if msgHandler != 0 {
		return TypeNil, fmt.Errorf("TODO(someday): support message handlers")
	}
	l.init()
	if l.Top() == 0 {
		return TypeNil, errors.New("empty stack")
	}
	t, _, err := l.valueByIndex(idx)
	k := l.stack[len(l.stack)-1]
	l.setTop(len(l.stack) - 1) // Always pop key.
	if err != nil {
		l.push(nil)
		return TypeNil, err
	}
	v, err := l.index(t, k)
	if err != nil {
		l.push(nil)
		return TypeNil, err
	}
	l.push(v)
	return valueType(v), nil
}

// index gets the value from a table for the given key,
// calling an __index metamethod if present.
func (l *State) index(t, k value) (value, error) {
	if t, ok := t.(*table); ok {
		if v := t.get(k); v != nil {
			return v, nil
		}
	}
	for range maxMetaDepth {
		tm := l.metamethod(t, luacode.TagMethodIndex)
		switch tm := tm.(type) {
		case nil:
			if _, isValueTable := t.(*table); !isValueTable {
				return nil, fmt.Errorf("attempt to index a %s", l.typeName(t))
			}
			return nil, nil
		case *table:
			if v := tm.get(k); v != nil {
				return v, nil
			}
		case luaFunction, goFunction:
			return l.call1(tm, t, k)
		}

		t = tm
	}

	return nil, fmt.Errorf("'%v' chain too long; possible loop", luacode.TagMethodIndex)
}

// Field pushes onto the stack the value t[k],
// where t is the value at the given index.
// See [State.Table] for further information.
func (l *State) Field(idx int, k string, msgHandler int) (Type, error) {
	if msgHandler != 0 {
		return TypeNil, fmt.Errorf("TODO(someday): support message handlers")
	}
	l.init()
	t, _, err := l.valueByIndex(idx)
	if err != nil {
		l.push(nil)
		return TypeNil, err
	}
	v, err := l.index(t, stringValue{s: k})
	if err != nil {
		l.push(nil)
		return TypeNil, err
	}
	l.push(v)
	return valueType(v), nil
}

// RawGet pushes onto the stack t[k],
// where t is the value at the given index
// and k is the value on the top of the stack.
// This function pops the key from the stack,
// pushing the resulting value in its place.
//
// RawGet does a raw access (i.e. without metamethods).
// The value at idx must be a table.
func (l *State) RawGet(idx int) Type {
	l.init()
	t, _, err := l.valueByIndex(idx)
	if err != nil {
		panic(err)
	}
	k, _, err := l.valueByIndex(-1)
	if err != nil {
		panic(err)
	}
	l.setTop(len(l.stack) - 1)

	v := t.(*table).get(k)
	l.push(v)
	return valueType(v)
}

// RawIndex pushes onto the stack the value t[n],
// where t is the table at the given index.
// The access is raw, that is, it does not use the __index metavalue.
// Returns the type of the pushed value.
func (l *State) RawIndex(idx int, n int64) Type {
	l.init()
	t, _, err := l.valueByIndex(idx)
	if err != nil {
		panic(err)
	}

	v := t.(*table).get(integerValue(n))
	l.push(v)
	return valueType(v)
}

// RawField pushes onto the stack t[k],
// where t is the value at the given index.
//
// RawField does a raw access (i.e. without metamethods).
// The value at idx must be a table.
func (l *State) RawField(idx int, k string) Type {
	l.init()
	t, _, err := l.valueByIndex(idx)
	if err != nil {
		panic(err)
	}

	v := t.(*table).get(stringValue{s: k})
	l.push(v)
	return valueType(v)
}

// CreateTable creates a new empty table and pushes it onto the stack.
// nArr is a hint for how many elements the table will have as a sequence;
// nRec is a hint for how many other elements the table will have.
// Lua may use these hints to preallocate memory for the new table.
func (l *State) CreateTable(nArr, nRec int) {
	l.init()
	l.push(newTable(nArr + nRec))
}

// Metatable reports whether the value at the given index has a metatable
// and if so, pushes that metatable onto the stack.
func (l *State) Metatable(idx int) bool {
	l.init()
	v, _, err := l.valueByIndex(idx)
	if err != nil {
		panic(err)
	}
	mt := l.metatable(v)
	if mt == nil {
		return false
	}
	l.push(mt)
	return true
}

func (l *State) metatable(v value) *table {
	switch v := v.(type) {
	case *table:
		return v.meta
	default:
		return l.typeMetatables[valueType(v)]
	}
}

// metamethod returns a field from v's metatable
// or nil if no such field (or metatable) exists.
func (l *State) metamethod(v value, tm luacode.TagMethod) value {
	return l.metatable(v).get(stringValue{s: tm.String()})
}

// binaryMetamethod returns a field from v1's or v2's metatable
// or nil if neither v1 nor v2 have such a field (or metatable).
// If both v1 and v2 have a field, v1's field will be returned.
func (l *State) binaryMetamethod(v1, v2 value, tm luacode.TagMethod) value {
	eventName := stringValue{s: tm.String()}
	if mm := l.metatable(v1).get(eventName); mm != nil {
		return mm
	}
	if mm := l.metatable(v2).get(eventName); mm != nil {
		return mm
	}
	return nil
}

// SetGlobal pops a value from the stack
// and sets it as the new value of the global with the given name.
//
// As in Lua, this function may trigger a metamethod on the globals table
// for the "newindex" event.
// If there is an error and msgHandler is not 0,
// SetGlobal pushes the error onto the stack and returns an error.
// (See Error Handling in [State].)
// SetGlobal always removes the value from the stack.
func (l *State) SetGlobal(name string, msgHandler int) error {
	if msgHandler != 0 {
		return fmt.Errorf("TODO(someday): support message handlers")
	}
	l.init()
	if l.Top() == 0 {
		return errors.New("stack empty")
	}
	v := l.stack[len(l.stack)-1]
	l.setTop(len(l.stack) - 1)
	if err := l.setIndex(l.registry.get(integerValue(RegistryIndexGlobals)), stringValue{s: name}, v); err != nil {
		return err
	}
	return nil
}

// SetTable does the equivalent to t[k] = v,
// where t is the value at the given index,
// v is the value on the top of the stack,
// and k is the value just below the top.
// This function pops both the key and the value from the stack.
//
// As in Lua, this function may trigger a metamethod for the "newindex" event.
// If there is an error and msgHandler is not 0,
// SetTable pushes the error onto the stack and returns an error.
// (See Error Handling in [State].)
// SetTable always removes the key and value from the stack.
func (l *State) SetTable(idx, msgHandler int) error {
	if msgHandler != 0 {
		return fmt.Errorf("TODO(someday): support message handlers")
	}
	l.init()
	if l.Top() < 2 {
		return errors.New("stack underflow")
	}
	t, _, err := l.valueByIndex(idx)
	k := l.stack[len(l.stack)-2]
	v := l.stack[len(l.stack)-1]
	l.setTop(len(l.stack) - 2) // Always pop key and value.
	if err != nil {
		return err
	}
	if err := l.setIndex(t, k, v); err != nil {
		return err
	}
	return nil
}

// setIndex sets the value in a table for the given key,
// calling a __newindex metamethod if appropriate.
func (l *State) setIndex(t, k, v value) error {
	// If there's an existing table entry, we don't search metatable.
	if t, _ := t.(*table); t.setExisting(k, v) {
		return nil
	}

	for range maxMetaDepth {
		tm := l.metamethod(t, luacode.TagMethodNewIndex)
		switch tm := tm.(type) {
		case nil:
			tab, _ := t.(*table)
			if tab == nil {
				return fmt.Errorf("attempt to index a %s", l.typeName(t))
			}
			return tab.set(k, v)
		case *table:
			if tm.setExisting(k, v) {
				return nil
			}
		case luaFunction, goFunction:
			if err := l.call(0, tm, t, k, v); err != nil {
				return err
			}
			return nil
		}

		t = tm
	}

	return fmt.Errorf("'%v' chain too long; possible loop", luacode.TagMethodNewIndex)
}

// SetField does the equivalent to t[k] = v,
// where t is the value at the given index,
// v is the value on the top of the stack,
// and k is the given string.
// This function pops the value from the stack.
// See [State.SetTable] for more information.
func (l *State) SetField(idx int, k string, msgHandler int) error {
	if msgHandler != 0 {
		return fmt.Errorf("TODO(someday): support message handlers")
	}
	l.init()
	if l.Top() < 1 {
		return errors.New("empty stack")
	}
	t, _, err := l.valueByIndex(idx)
	v := l.stack[len(l.stack)-1]
	l.setTop(len(l.stack) - 1) // Always pop value.
	if err != nil {
		return err
	}
	if err := l.setIndex(t, stringValue{s: k}, v); err != nil {
		return err
	}
	return nil
}

// RawSet does the equivalent to t[k] = v,
// where t is the value at the given index,
// v is the value on the top of the stack,
// and k is the value just below the top.
// This function pops both the key and the value from the stack.
func (l *State) RawSet(idx int) {
	l.init()
	if l.Top() < 2 {
		panic("stack underflow")
	}
	t, _, err := l.valueByIndex(idx)
	k := l.stack[len(l.stack)-2]
	v := l.stack[len(l.stack)-1]
	l.setTop(len(l.stack) - 2) // Always pop key and value.
	if err != nil {
		panic(err)
	}
	if err := t.(*table).set(k, v); err != nil {
		panic(err)
	}
}

// RawSetIndex does the equivalent of t[n] = v,
// where t is the table at the given index
// and v is the value on the top of the stack.
// This function pops the value from the stack.
// The assignment is raw, that is, it does not use the __newindex metavalue.
func (l *State) RawSetIndex(idx int, n int64) {
	l.init()
	if l.Top() < 1 {
		panic("stack underflow")
	}
	t, _, err := l.valueByIndex(idx)
	v := l.stack[len(l.stack)-1]
	l.setTop(len(l.stack) - 1) // Always pop value.
	if err != nil {
		panic(err)
	}
	if err := t.(*table).set(integerValue(n), v); err != nil {
		panic(err)
	}
}

// RawSetField does the equivalent to t[k] = v,
// where t is the value at the given index
// and v is the value on the top of the stack.
// This function pops the value from the stack.
func (l *State) RawSetField(idx int, k string) {
	l.init()
	if l.Top() < 1 {
		panic("stack underflow")
	}
	t, _, err := l.valueByIndex(idx)
	v := l.stack[len(l.stack)-1]
	l.setTop(len(l.stack) - 1) // Always pop value.
	if err != nil {
		panic(err)
	}
	if err := t.(*table).set(stringValue{s: k}, v); err != nil {
		panic(err)
	}
}

// Call calls a function (or callable object) in protected mode.
//
// To do a call you must use the following protocol:
// first, the function to be called is pushed onto the stack;
// then, the arguments to the call are pushed in direct order;
// that is, the first argument is pushed first.
// Finally you call Call;
// nArgs is the number of arguments that you pushed onto the stack.
// When the function returns,
// all arguments and the function value are popped
// and the call results are pushed onto the stack.
// The number of results is adjusted to nResults,
// unless nResults is [MultipleReturns].
// In this case, all results from the function are pushed;
// Lua takes care that the returned values fit into the stack space,
// but it does not ensure any extra space in the stack.
// The function results are pushed onto the stack in direct order
// (the first result is pushed first),
// so that after the call the last result is on the top of the stack.
//
// Call always removes the function and its arguments from the stack.
// If an error occurs and msgHandler is not zero,
// then Call will push an error object to the stack.
// (See Error Handling in [State] for details.)
func (l *State) Call(nArgs, nResults, msgHandler int) error {
	l.init()
	if l.Top() < nArgs+1 {
		return fmt.Errorf("not enough elements in the stack")
	}
	if nResults != MultipleReturns && cap(l.stack)-len(l.stack) < nResults-nArgs {
		return fmt.Errorf("results from function overflow current stack size")
	}
	if msgHandler != 0 {
		return fmt.Errorf("TODO(someday): support message handlers")
	}

	isLua, err := l.prepareCall(nArgs, nResults)
	if err != nil {
		return err
	}
	if isLua {
		if err := l.exec(); err != nil {
			return err
		}
	}
	return nil
}

// call calls a function directly.
// f and args are temporarily pushed onto the stack,
// thus placing an upper bound on recursion.
// Results will be pushed on the stack.
func (l *State) call(numResults int, f value, args ...value) error {
	if !l.grow(len(l.stack) + max(1+len(args), numResults)) {
		return errStackOverflow
	}
	l.stack = append(l.stack, f)
	l.stack = append(l.stack, args...)
	isLua, err := l.prepareCall(len(args), numResults)
	if err != nil {
		return err
	}
	if isLua {
		if err := l.exec(); err != nil {
			return err
		}
	}
	return nil
}

// call1 calls a function and returns its single result.
// f and args are temporarily pushed onto the stack,
// thus placing an upper bound on recursion.
func (l *State) call1(f value, args ...value) (value, error) {
	if err := l.call(1, f, args...); err != nil {
		return nil, err
	}
	v := l.stack[len(l.stack)-1]
	l.setTop(len(l.stack) - 1)
	return v, nil
}

func (l *State) prepareCall(numArgs, numResults int) (isLua bool, err error) {
	functionIndex := len(l.stack) - numArgs - 1
	l.callStack = append(l.callStack, callFrame{
		functionIndex: functionIndex,
		numResults:    numResults,
	})
	for range maxMetaDepth {
		switch f := l.stack[functionIndex].(type) {
		case luaFunction:
			if err := l.checkUpvalues(f.upvalues); err != nil {
				l.popCallStack()
				l.setTop(functionIndex - 1)
				return true, err
			}
			if !l.grow(len(l.stack) + int(f.proto.MaxStackSize) - numArgs) {
				l.popCallStack()
				l.setTop(functionIndex - 1)
				return true, errStackOverflow
			}
			return true, nil
		case goFunction:
			if err := l.checkUpvalues(f.upvalues); err != nil {
				l.popCallStack()
				l.setTop(functionIndex - 1)
				return false, err
			}
			if !l.grow(len(l.stack) + minStack) {
				l.popCallStack()
				l.setTop(functionIndex - 1)
				return false, errStackOverflow
			}
			n, err := f.cb(l)
			if err != nil {
				l.popCallStack()
				l.setTop(functionIndex - 1)
				return false, err
			}
			l.finishCall(n)
			return false, nil
		default:
			tm := l.metamethod(f, luacode.TagMethodCall)
			if tm == nil {
				l.popCallStack()
				l.setTop(functionIndex - 1)
				return false, fmt.Errorf("function is a %v", valueType(f))
			}
			if !l.grow(len(l.stack) + 1) {
				l.popCallStack()
				l.setTop(functionIndex - 1)
				return false, errStackOverflow
			}
			// Move original function object into first argument position.
			l.stack = slices.Insert(l.stack, functionIndex, tm)
		}
	}

	l.popCallStack()
	l.setTop(functionIndex - 1)
	return false, fmt.Errorf("exceeded depth for %v", luacode.TagMethodCall)
}

func (l *State) popCallStack() {
	l.callStack[len(l.callStack)-1] = callFrame{}
	l.callStack = l.callStack[:len(l.callStack)-1]
}

// Load loads a Lua chunk without running it.
// If there are no errors,
// Load pushes the compiled chunk as a Lua function on top of the stack.
// Otherwise, it pushes an error message.
//
// The chunkName argument gives a name to the chunk,
// which is used for error messages and in [debug information].
//
// The string mode controls whether the chunk can be text or binary
// (that is, a precompiled chunk).
// It may be the string "b" (only binary chunks),
// "t" (only text chunks),
// or "bt" (both binary and text).
//
// [debug information]: https://www.lua.org/manual/5.4/manual.html#4.7
func (l *State) Load(r io.Reader, chunkName luacode.Source, mode string) (err error) {
	l.init()
	defer func() {
		if err != nil {
			l.PushString(err.Error())
		}
	}()

	var p *luacode.Prototype
	switch mode {
	case "bt":
		br := bufio.NewReader(r)
		if prefix, _ := br.Peek(len(luacode.Signature)); string(prefix) == luacode.Signature {
			p = new(luacode.Prototype)
			data, err := io.ReadAll(br)
			if err != nil {
				return err
			}
			if err := p.UnmarshalBinary(data); err != nil {
				return err
			}
		} else {
			var err error
			p, err = luacode.Parse(chunkName, br)
			if err != nil {
				return err
			}
		}
	case "b":
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		p = new(luacode.Prototype)
		if err := p.UnmarshalBinary(data); err != nil {
			return err
		}
	case "t":
		br := bufio.NewReader(r)
		var err error
		p, err = luacode.Parse(chunkName, br)
		if err != nil {
			return err
		}
	}

	l.push(luaFunction{
		id:    nextID(),
		proto: p,
		upvalues: []*upvalue{
			closedUpvalue(l.registry.get(integerValue(RegistryIndexGlobals))),
		},
	})
	return nil
}

// Concat concatenates the n values at the top of the stack, pops them,
// and leaves the result on the top.
// If n is 1, the result is the single value on the stack
// (that is, the function does nothing);
// if n is 0, the result is the empty string.
// Concatenation is performed following the usual semantics of Lua.
//
// If there is any error, Concat catches it,
// leaves nil or the error object (see Error Handling in [State]) on the top of the stack,
// and returns an error.
func (l *State) Concat(n, msgHandler int) error {
	if n < 0 {
		return errors.New("lua concat: negative argument length")
	}
	if n > l.Top() {
		return errors.New("lua concat: not enough arguments on the stack")
	}
	if msgHandler != 0 {
		return fmt.Errorf("TODO(someday): support message handlers")
	}

	if err := l.concat(n); err != nil {
		l.push(nil)
		return err
	}
	return nil
}

func (l *State) concat(n int) error {
	if n == 0 {
		l.push(stringValue{})
		return nil
	}
	firstArg := len(l.stack) - n
	if firstArg < l.frame().registerStart() {
		return errors.New("concat: stack underflow")
	}

	isEmptyString := func(v value) bool {
		sv, ok := v.(stringValue)
		return ok && sv.isEmpty()
	}

	for len(l.stack) > firstArg+1 {
		v1 := l.stack[len(l.stack)-2]
		_, isStringer1 := v1.(valueStringer)
		v2 := l.stack[len(l.stack)-1]
		vs2, isStringer2 := v2.(valueStringer)
		switch {
		case !isStringer1 || !isStringer2:
			if err := l.concatMetamethod(); err != nil {
				l.setTop(firstArg)
				return err
			}
		case isEmptyString(v1):
			l.stack[len(l.stack)-2] = vs2.stringValue()
			fallthrough
		case isEmptyString(v2):
			l.setTop(len(l.stack) - 1)
		default:
			// The end of the slice has two or more non-empty strings.
			// Find the longest run of values that can be coerced to a string,
			// and perform raw string concatenation.
			concatStart := firstArg + stringerTailStart(l.stack[firstArg:len(l.stack)-2])
			initialCapacity, hasContext := minConcatSize(l.stack[concatStart:])
			sb := new(strings.Builder)
			sb.Grow(initialCapacity)
			var sctx sets.Set[string]
			if hasContext {
				sctx = make(sets.Set[string])
			}

			for _, v := range l.stack[concatStart:] {
				sv := v.(valueStringer).stringValue()
				sb.WriteString(sv.s)
				sctx.AddSeq(sv.context.All())
			}

			l.stack[concatStart] = stringValue{
				s:       sb.String(),
				context: sctx,
			}
			l.setTop(concatStart + 1)
		}
	}
	return nil
}

// concatMetamethod attempts to call the __concat metamethod
// with the two values on the top of the stack.
func (l *State) concatMetamethod() error {
	arg1 := l.stack[len(l.stack)-2]
	arg2 := l.stack[len(l.stack)-1]
	f := l.binaryMetamethod(arg1, arg2, luacode.TagMethodConcat)
	if f == nil {
		badArg := arg1
		if _, isStringer := badArg.(valueStringer); isStringer {
			badArg = arg2
		}
		return fmt.Errorf("attempt to concatenate a %s value", l.typeName(badArg))
	}

	// Insert metamethod before two arguments.
	l.push(f)
	rotate(l.stack[len(l.stack)-3:], 1)

	// Call metamethod.
	isLua, err := l.prepareCall(2, 1)
	if err != nil {
		return err
	}
	if isLua {
		if err := l.exec(); err != nil {
			return err
		}
	}
	return nil
}

// stringerTailStart returns the first index i
// where every element of values[i:] implements [valueStringer].
func stringerTailStart(values []value) int {
	for ; len(values) > 0; values = values[:len(values)-1] {
		_, isStringer := values[len(values)-1].(valueStringer)
		if !isStringer {
			break
		}
	}
	return len(values)
}

// minConcatSize returns the minimum buffer size necessary
// to concatenate the given values.
func minConcatSize(values []value) (n int, hasContext bool) {
	for _, v := range values {
		if sv, ok := v.(stringValue); ok {
			n += len(sv.s)
			hasContext = hasContext || len(sv.context) > 0
		} else {
			// Numbers are non-empty, so add 1.
			n++
		}
	}
	return
}

// Len pushes the length of the value at the given index to the stack.
// It is equivalent to the ['#' operator in Lua]
// and may trigger a [metamethod] for the "length" event.
//
// If there is any error, Len catches it,
// pushes nil or the error object (see Error Handling in [State]) onto the stack,
// and returns an error.
//
// ['#' operator in Lua]: https://www.lua.org/manual/5.4/manual.html#3.4.7
// [metamethod]: https://www.lua.org/manual/5.4/manual.html#2.4
func (l *State) Len(idx, msgHandler int) error {
	l.init()
	v, _, err := l.valueByIndex(idx)
	if err != nil {
		panic(err)
	}
	if msgHandler != 0 {
		return fmt.Errorf("TODO(someday): support message handlers")
	}

	result, err := l.len(v)
	if err != nil {
		return err
	}
	l.push(result)
	return nil
}

func (l *State) len(v value) (value, error) {
	// Strings always return their byte length.
	if v, ok := v.(stringValue); ok {
		return v.len(), nil
	}

	if mm := l.metamethod(v, luacode.TagMethodLen); mm != nil {
		return l.call1(mm, v)
	}
	lv, ok := v.(lenValue)
	if !ok {
		return nil, fmt.Errorf("attempt to get length of a %s value", l.typeName(v))
	}
	return lv.len(), nil
}

func (l *State) typeName(v value) string {
	switch v := v.(type) {
	case *table:
		if s, ok := v.get(stringValue{s: "__name"}).(stringValue); ok {
			return s.s
		}
	}
	return valueType(v).String()
}

var errStackOverflow = errors.New("stack overflow")
