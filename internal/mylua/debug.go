// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package mylua

import (
	"fmt"

	"zb.256lights.llc/pkg/internal/luacode"
)

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

func sourceToString(source luacode.Source) string {
	if source == "" {
		return "?"
	}
	return source.String()
}
