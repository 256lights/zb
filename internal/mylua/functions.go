// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package mylua

import (
	"fmt"
	"slices"

	"zb.256lights.llc/pkg/internal/luacode"
)

type function interface {
	value
	functionID() uint64
	upvaluesSlice() []*upvalue
}

var (
	_ function = goFunction{}
	_ function = luaFunction{}
)

type goFunction struct {
	id       uint64
	cb       Function
	upvalues []*upvalue
}

func (f goFunction) valueType() Type           { return TypeFunction }
func (f goFunction) functionID() uint64        { return f.id }
func (f goFunction) upvaluesSlice() []*upvalue { return f.upvalues }

type luaFunction struct {
	id       uint64
	proto    *luacode.Prototype
	upvalues []*upvalue
}

func (f luaFunction) valueType() Type           { return TypeFunction }
func (f luaFunction) functionID() uint64        { return f.id }
func (f luaFunction) upvaluesSlice() []*upvalue { return f.upvalues }

// markTBC marks the given index in l.stack as “to be closed”.
// When the stack element is popped (or explicitly closed),
// then its “__close” metamethod will be invoked.
// If the value at l.stack[i] is false or nil,
// then markTBC does not mark the index and returns nil.
// Otherwise, markTBC returns an error if the value at l.stack[i]
// does not have a “__close” metamethod.
func (l *State) markTBC(i int) error {
	v := l.stack[i]
	if !toBoolean(v) {
		return nil
	}
	if l.metamethod(v, luacode.TagMethodClose) == nil {
		variableName := l.localVariableName(l.frame(), i)
		if variableName == "" {
			variableName = "?"
		}
		return fmt.Errorf("variable '%s' got a non-closable value", variableName)
	}
	l.tbc.Add(uint(i))
	return nil
}

// closeTBCSlots runs the “__close” metamethods of any to-be-closed variables
// previously marked by [*State.markTBC]
// from the top of the stack to the given bottom index in last-in first-out order.
// If preserveTop is false, then closeTBCSlots moves the stack's top
// before calling each “__close” metamethod to save on stack space
// and finally moves the stack's top to bottom before returning.
// closeTBCSlots returns the last error raised during execution of the metamethods,
// or the original error object if no errors were raised.
func (l *State) closeTBCSlots(bottom int, preserveTop bool, err error) error {
	for tbc := range l.tbc.Reversed() {
		if tbc < uint(bottom) {
			break
		}
		l.tbc.Delete(tbc)

		v := l.stack[tbc]
		if !preserveTop {
			newTop := tbc + 1
			clear(l.stack[newTop:])
			l.stack = l.stack[:newTop]
		}
		newError := l.call(0, l.metamethod(v, luacode.TagMethodClose), v, errorToValue(err))
		if newError != nil {
			err = newError
		}
	}
	if !preserveTop {
		l.setTop(bottom)
	}
	return err
}

// An upvalue is a variable defined in the lexical scope outside a function.
// An upvalue is "open" if it refers to the stack
// or "closed" if it has escaped the stack.
type upvalue struct {
	stackIndex int
	storage    value
}

// closedUpvalue returns an [upvalue] with the given value
// that is stored off the stack.
func closedUpvalue(v value) *upvalue {
	return &upvalue{
		storage:    v,
		stackIndex: -1,
	}
}

// isOpen reports whether the upvalue is stored on the stack.
func (uv *upvalue) isOpen() bool {
	return uv.stackIndex >= 0
}

// stackUpvalue returns an [*upvalue] for the given stack index.
// Until the upvalue is closed,
// multiple calls to stackUpvalue for the same index
// will return the same [*upvalue].
func (l *State) stackUpvalue(i int) *upvalue {
	uvIndex := slices.IndexFunc(l.pendingVariables, func(uv *upvalue) bool {
		return uv.stackIndex == i
	})
	if uvIndex != -1 {
		return l.pendingVariables[uvIndex]
	}
	uv := &upvalue{stackIndex: i}
	l.pendingVariables = append(l.pendingVariables, uv)
	return uv
}

// resolveUpvalue converts an [*upvalue] to a pointer to a [value],
// representing the upvalue's variable.
// If the upvalue is open, then the returned pointer is valid
// until the stack grows.
func (l *State) resolveUpvalue(uv *upvalue) *value {
	if uv.isOpen() {
		return &l.stack[uv.stackIndex]
	}
	return &uv.storage
}

// checkUpvalues ensures that the given set of upvalues
// are either closed or referring to variables in the calling function.
func (l *State) checkUpvalues(upvalues []*upvalue) error {
	frame := l.frame()
	for i, uv := range upvalues {
		if uv.stackIndex >= frame.framePointer() {
			return fmt.Errorf("internal error: function upvalue [%d] inside current frame", i)
		}
	}
	return nil
}

// closeUpvalues moves the values of any upvalues
// that refer to stack values at indices less than top
// off to the stack, thus “closing” them.
// This is distinct from calling the “__close” metamethods,
// but often happens at the same time.
func (l *State) closeUpvalues(top int) {
	n := 0
	for _, uv := range l.pendingVariables {
		if uv.isOpen() && uv.stackIndex >= top {
			// Close the upvalue.
			uv.storage = l.stack[uv.stackIndex]
			uv.stackIndex = -1
		} else {
			// Keep the upvalue in the list.
			l.pendingVariables[n] = uv
			n++
		}
	}
	clear(l.pendingVariables[n:])
	l.pendingVariables = l.pendingVariables[:n]
}
