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
