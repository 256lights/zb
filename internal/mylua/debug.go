// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package mylua

import (
	"fmt"

	"zb.256lights.llc/pkg/internal/luacode"
)

// Source is a description of Lua source.
// The zero value describes an empty literal string.
type Source = luacode.Source

// UnknownSource is a placeholder for an unknown [Source].
const UnknownSource = luacode.UnknownSource

// FilenameSource returns a [Source] for a filesystem path.
// The path can be retrieved later using [Source.Filename].
//
// The underlying string in a filename source starts with "@".
func FilenameSource(path string) Source {
	return luacode.FilenameSource(path)
}

// AbstractSource returns a [Source] from a user-dependent description.
// The description can be retrieved later using [Source.Abstract].
//
// The underlying string in an abstract source starts with "=".
func AbstractSource(description string) Source {
	return luacode.AbstractSource(description)
}

// LiteralSource returns a [Source] for the given literal string.
// Because the type for a [Source] is determined by the first byte,
// if s starts with one of those symbols
// (which cannot occur in a syntactically valid Lua source file),
// then LiteralSource returns an [AbstractSource]
// with a condensed version of the string.
func LiteralSource(s string) Source {
	return luacode.LiteralSource(s)
}

// Debug holds information about a function or an activation record.
type Debug struct {
	// Name is a reasonable name for the given function.
	// Because functions in Lua are first-class values, they do not have a fixed name:
	// some functions can be the value of multiple global variables,
	// while others can be stored only in a table field.
	// The [*State.Info] function checks how the function was called to find a suitable name.
	// If they cannot find a name, then Name is set to the empty string.
	Name string
	// NameWhat explains the Name field.
	// The value of NameWhat can be
	// "global", "local", "method", "field", "upvalue", or the empty string,
	// according to how the function was called.
	// (Lua uses the empty string when no other option seems to apply.)
	NameWhat string
	// What is the string "Lua" if the function is a Lua function,
	// "Go" if it is a Go function,
	// "main" if it is the main part of a chunk.
	What string
	// Source is the source of the chunk that created the function.
	Source Source
	// CurrentLine is the current line where the given function is executing.
	// When no line information is available, CurrentLine is set to -1.
	CurrentLine int
	// LineDefined is the line number where the definition of the function starts.
	LineDefined int
	// LastLineDefined is the line number where the definition of the function ends.
	LastLineDefined int
	// NumUpvalues is the number of upvalues of the function.
	NumUpvalues uint8
	// NumParams is the number of parameters of the function
	// (always 0 for Go functions).
	NumParams uint8
	// IsVararg is true if the function is a variadic function
	// (always true for Go functions).
	IsVararg bool
	// IsTailCall is true if this function invocation was called by a tail call.
	// In this case, the caller of this level is not in the stack.
	IsTailCall bool
}

func newDebug(f function, info *callFrame) *Debug {
	db := &Debug{
		Source:          UnknownSource,
		CurrentLine:     -1,
		LineDefined:     -1,
		LastLineDefined: -1,
		IsVararg:        true,
		NumUpvalues:     uint8(len(f.upvaluesSlice())),
		IsTailCall:      info != nil && info.isTailCall,
	}
	// TODO(soon): Fill in Name/NameWhat.
	switch f := f.(type) {
	case luaFunction:
		db.Source = f.proto.Source
		db.LineDefined = f.proto.LineDefined
		db.LastLineDefined = f.proto.LastLineDefined
		if f.proto.IsMainChunk() {
			db.What = "main"
		} else {
			db.What = "Lua"
		}
		if info != nil {
			if pc := info.pc - 1; 0 <= pc && pc < f.proto.LineInfo.Len() {
				db.CurrentLine = f.proto.LineInfo.At(pc)
			}
		}
		db.IsVararg = f.proto.IsVararg
		db.NumParams = f.proto.NumParams
	case goFunction:
		db.Source = "=[Go]"
		db.What = "Go"
	}
	return db
}

// Info returns debug information
// about the function executing at the given level.
// Level 0 is the current running function,
// whereas level n+1 is the function that has called level n
// (except for tail calls, which do not count in the stack).
// When called with a level greater than the stack depth, Stack returns nil.
// The special level -1 indicates the function value pushed onto the top of the stack.
func (l *State) Info(level int) *Debug {
	if level < -1 {
		return nil
	}
	l.init()

	var v value
	var frame *callFrame
	if level == -1 {
		if l.Top() == 0 {
			panic(errMissingArguments)
		}
		v = l.stack[len(l.stack)-1]
	} else {
		level = len(l.callStack) - 1 - level
		if level < 0 {
			return nil
		}
		frame = &l.callStack[level]
		v = l.stack[frame.functionIndex]
	}

	f, ok := v.(function)
	if !ok {
		return nil
	}
	return newDebug(f, frame)
}

// Upvalue gets information about the i'th upvalue of the closure at funcIndex.
// Upvalue pushes the upvalue's value onto the stack
// and returns its name.
// Returns ("", false) and pushes nothing when i is greater than the number of upvalues.
// The first upvalue is accessed with an i of 1.
func (l *State) Upvalue(funcIndex int, i int) (upvalueName string, ok bool) {
	l.init()
	upvalueName, ptr := l.upvalue(funcIndex, i)
	if ptr == nil {
		return "", false
	}
	l.push(*ptr)
	return upvalueName, true
}

func (l *State) upvalue(funcIndex int, i int) (upvalueName string, upvalue *value) {
	i-- // Convert to 0-based.
	if i < 0 {
		return "", nil
	}

	v, _, err := l.valueByIndex(funcIndex)
	if err != nil {
		return "", nil
	}
	switch f := v.(type) {
	case luaFunction:
		if i >= len(f.upvalues) {
			return "", nil
		}
		if i-1 < len(f.proto.Upvalues) {
			upvalueName = f.proto.Upvalues[i].Name
		}
		return upvalueName, l.resolveUpvalue(f.upvalues[i])
	case function:
		upvalues := f.upvaluesSlice()
		if i >= len(upvalues) {
			return "", nil
		}
		return "", l.resolveUpvalue(upvalues[i])
	default:
		return "", nil
	}
}

// SetUpvalue sets the value of a closure's upvalue.
// SetUpvalue assigns the value on the top of the stack to the upvalue,
// returns the upvalue's name,
// and also pops the value from the stack.
// Returns ("", false) when i is greater than the number of upvalues.
// The first upvalue is accessed with an i of 1.
func (l *State) SetUpvalue(funcIndex int, i int) (upvalueName string, ok bool) {
	if l.Top() < 1 {
		panic(errMissingArguments)
	}
	l.init()
	upvalueName, ptr := l.upvalue(funcIndex, i)
	if ptr == nil {
		return "", false
	}
	top := len(l.stack) - 1
	v := l.stack[top]
	l.setTop(top)
	*ptr = v
	return upvalueName, true
}

func (l *State) localVariableName(frame *callFrame, i int) string {
	if start, end := frame.extraArgumentsRange(); start <= i && i < end {
		return "(vararg)"
	}
	registerStart := frame.registerStart()
	if i < registerStart {
		return ""
	}
	f, isLua := l.stack[frame.functionIndex].(luaFunction)
	if !isLua {
		return "(Go temporary)"
	}
	if i >= int(f.proto.MaxStackSize) {
		return ""
	}
	name := f.proto.LocalName(uint8(i), frame.pc-1)
	if name == "" {
		name = "(temporary)"
	}
	return name
}

func sourceLocation(proto *luacode.Prototype, pc int) string {
	if pc >= proto.LineInfo.Len() {
		return functionLocation(proto)
	}
	return fmt.Sprintf("%s:%d", sourceToString(proto.Source), proto.LineInfo.At(pc))
}

func functionLocation(proto *luacode.Prototype) string {
	return fmt.Sprintf("function defined at %s:%d", sourceToString(proto.Source), proto.LineDefined)
}

func sourceToString(source Source) string {
	if source == "" {
		return "?"
	}
	return source.String()
}
