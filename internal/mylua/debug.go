// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package mylua

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
	name := f.proto.LocalName(uint8(i), frame.pc)
	if name == "" {
		name = "(temporary)"
	}
	return name
}
