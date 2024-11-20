// Copyright (C) 1994-2024 Lua.org, PUC-Rio.
// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import (
	"errors"
	"fmt"
)

type funcState struct {
	f *Prototype
	// prev is the enclosing function.
	prev *funcState
	// blocks is the chain of current blocks.
	blocks *blockControl

	// lastTarget is the last returned value from [funcState.label].
	lastTarget int
	// previousLine is the last line number saved in f.LineInfo.
	previousLine int
	// firstLocal is the index of the first local variable
	// in [parser].activeVars.
	firstLocal int
	// firstLabel is the index of the first label
	// in [parser].labels.
	firstLabel int
	// numActiveVariables is the number of active local variables.
	numActiveVariables uint8
	// firstFreeRegister is the first free register.
	firstFreeRegister registerIndex
	// instructionsSinceLastAbsLineInfo is a counter
	// of instructions added since the last [absLineInfo].
	instructionsSinceLastAbsLineInfo uint8
	// needClose is true if the function needs to close upvalues when returning.
	needClose bool
}

type blockControl struct {
	prev       *blockControl
	firstLabel int
	firstGoto  int
	// numActiveVariables is the number of active locals outside the block.
	numActiveVariables uint8

	// upval is true if some variable in the block is an upvalue.
	upval     bool
	isLoop    bool
	insideTBC bool
}

// finish perfoms a final peephole optimization pass over the code of a function.
func (fs *funcState) finish() error {
	for i, instruction := range fs.f.Code {
		if i > 0 && fs.f.Code[i-1].IsOutTop() != instruction.IsInTop() {
			return fmt.Errorf("internal error: instruction %d: %v follows %v",
				i, instruction.OpCode(), fs.f.Code[i-1].OpCode())
		}

		switch instruction.OpCode() {
		case OpReturn0, OpReturn1:
			if !(fs.needClose || fs.f.IsVararg) {
				break
			}
			instruction = ABCInstruction(
				OpReturn,
				instruction.ArgA(),
				instruction.ArgB(),
				instruction.ArgC(),
				instruction.K(),
			)
			fallthrough
		case OpReturn, OpTailCall:
			if fs.needClose {
				instruction, _ = instruction.WithK(true)
			}
			if fs.f.IsVararg {
				instruction, _ = instruction.WithArgC(fs.f.NumParams + 1)
			}
			fs.f.Code[i] = instruction
		case OpJmp:
			target := i
			for count := 0; count < 100; count++ {
				curr := fs.f.Code[target]
				if curr.OpCode() != OpJmp {
					break
				}
				target += int(curr.J()) + 1
			}
			if err := fs.fixJump(i, target); err != nil {
				return err
			}
		}
	}
	return nil
}

func (fs *funcState) removeLastInstruction() {
	fs.removeLastLineInfo()
	fs.f.Code = fs.f.Code[:len(fs.f.Code)-1]
}

// label marks the next instruction to be added as a jump target
// (to avoid wrong optimizations with consecutive instructions
// not in the same basic block)
// and returns its index.
func (fs *funcState) label() int {
	pc := len(fs.f.Code)
	fs.lastTarget = pc
	return pc
}

// saveLineInfo save the line information for a new instruction.
// If difference from last line does not fit in a byte,
// of after that many instructions,
// save a new absolute line info;
// (in that case, the special value 'ABSLINEINFO' in 'lineinfo'
// signals the existence of this absolute information.)
// Otherwise, store the difference from last line in 'lineinfo'.
func (fs *funcState) saveLineInfo(line int) {
	const deltaLimit = 1 << 7
	delta := line - fs.previousLine
	absDelta := delta
	if delta < 0 {
		absDelta = -delta
	}

	pc := len(fs.f.Code) - 1 // last instruction coded

	if absDelta >= deltaLimit || fs.instructionsSinceLastAbsLineInfo >= maxInstructionsWithoutAbsLineInfo {
		fs.f.LineInfo.abs = append(fs.f.LineInfo.abs, absLineInfo{
			pc:   pc,
			line: line,
		})
		delta = int(absMarker)
		fs.instructionsSinceLastAbsLineInfo = 1
	} else {
		fs.instructionsSinceLastAbsLineInfo++
	}

	fs.f.LineInfo.rel = append(fs.f.LineInfo.rel, int8(delta))
	fs.previousLine = line
}

// removeLastLineInfo remove line information from the last instruction.
func (fs *funcState) removeLastLineInfo() {
	lineInfo := &fs.f.LineInfo

	if lastDelta := lineInfo.rel[len(lineInfo.rel)-1]; lastDelta == absMarker {
		lineInfo.abs = lineInfo.abs[:len(lineInfo.abs)-1]
		// Force next line info to be absolute.
		fs.instructionsSinceLastAbsLineInfo = maxInstructionsWithoutAbsLineInfo + 1
	} else {
		fs.previousLine -= int(lastDelta)
		fs.instructionsSinceLastAbsLineInfo--
	}
}

// fixLineInfo changes the line information associated with the last instruction.
func (fs *funcState) fixLineInfo(line int) {
	fs.removeLastLineInfo()
	fs.saveLineInfo(line)
}

// reserveRegister reserves a register in the stack and returns it.
func (fs *funcState) reserveRegister() (registerIndex, error) {
	if err := fs.checkStack(1); err != nil {
		return noRegister, err
	}
	reg := fs.firstFreeRegister
	fs.firstFreeRegister++
	return reg, nil
}

// reserveRegisters reserves n additional registers in the stack.
func (fs *funcState) reserveRegisters(n int) error {
	if err := fs.checkStack(n); err != nil {
		return err
	}
	fs.firstFreeRegister += registerIndex(n)
	return nil
}

// checkStack determines whether is sufficient room to add n more registers.
// The high watermark will be recorded in the [Prototype] as MaxStackSize.
func (fs *funcState) checkStack(n int) error {
	newStack := int(fs.firstFreeRegister) + n
	if newStack <= int(fs.f.MaxStackSize) {
		return nil
	}
	if newStack > maxRegisters {
		return errors.New("function or expression needs too many registers")
	}
	fs.f.MaxStackSize = uint8(newStack)
	return nil
}

// concatJumpList concatenates l2 to jump-list l1.
func (fs *funcState) concatJumpList(l1, l2 int) (int, error) {
	switch {
	case l2 == noJump:
		return l1, nil
	case l1 == noJump:
		return l2, nil
	default:
		list := l1
		for {
			next, ok := fs.jumpDestination(list)
			if !ok {
				break
			}
			list = next
		}
		err := fs.fixJump(list, l2)
		return l1, err
	}
}

// patchList traverses a list of tests,
// patching their destination address and registers.
// Tests producing values jump to vtarget
// (and put their values in reg),
// other tests jump to dtarget.
func (fs *funcState) patchList(list, vtarget int, reg registerIndex, dtarget int) error {
	if vtarget > len(fs.f.Code) || dtarget > len(fs.f.Code) {
		return errors.New("patchList target cannot be a forward address")
	}

	for list != noJump {
		next, hasNext := fs.jumpDestination(list)

		var target int
		if fs.patchTestRegister(list, reg) {
			target = vtarget
		} else {
			target = dtarget
		}
		if err := fs.fixJump(list, target); err != nil {
			return err
		}

		if !hasNext {
			break
		}
		list = next
	}
	return nil
}

// patchTestRegister patches the destination register for an [OpTestSet] instruction.
// If 'reg' is not [noRegister],
// patchTestRegister sets it as the destination register.
// Otherwise, patchTestRegister changes the instruction to a simple [OpTest]
// (produces no register value).
// patchTestRegister returns false and no-ops if and only if
// the instruction in position 'node' is not an [OpTestSet].
func (fs *funcState) patchTestRegister(node int, reg registerIndex) bool {
	i := fs.findJumpControl(node)
	if i.OpCode() != OpTestSet {
		return false
	}
	if reg != noRegister && reg != registerIndex(i.ArgB()) {
		*i = ABCInstruction(OpTestSet, uint8(reg), i.ArgB(), i.ArgC(), i.K())
	} else {
		*i = ABCInstruction(OpTest, i.ArgB(), 0, 0, i.K())
	}
	return true
}

// jumpDestination returns the destination address of a jump instruction.
func (fs *funcState) jumpDestination(pc int) (newPC int, ok bool) {
	offset := fs.f.Code[pc].J()
	if offset == noJump {
		// A cyclic jump represents end of list.
		return noJump, false
	}
	return pc + 1 + int(offset), true
}

// findJumpControl returns a pointer to the instruction "controlling" a given jump
// (i.e. a jump's condition),
// or the jump itself if it is unconditional.
func (fs *funcState) findJumpControl(pc int) *Instruction {
	if pc < 1 || !fs.f.Code[pc-1].OpCode().IsTest() {
		return &fs.f.Code[pc]
	}
	return &fs.f.Code[pc-1]
}

// fixJump changes the jump instruction at pc to jump to the given destination.
func (fs *funcState) fixJump(pc int, dest int) error {
	jmp := &fs.f.Code[pc]
	offset := dest - (pc + 1)
	if dest == noJump {
		return errors.New("invalid jump destination")
	}
	if !(-offsetJ <= offset && offset <= maxJArg-offsetJ) {
		return errors.New("control structure too long")
	}
	op := jmp.OpCode()
	if op != OpJmp {
		return fmt.Errorf("fixJump called on %v", op)
	}
	*jmp = JInstruction(op, int32(offset))
	return nil
}

func (fs *funcState) negateCondition(pc int) error {
	i := fs.findJumpControl(pc)
	op := i.OpCode()
	if !op.IsTest() || op == OpTestSet || op == OpTest {
		return fmt.Errorf("instruction at %d is not a comparison (got %v)", pc, op)
	}
	var ok bool
	*i, ok = i.WithK(!i.K())
	if !ok {
		return fmt.Errorf("instruction at %d (%v) does not have K argument", pc, op)
	}
	return nil
}

// previousInstruction returns a pointer into the [Prototype] Code array
// to the last added instruction.
// If there may be a jump target between the current instruction
// and the previous one,
// returns nil (to avoid wrong optimizations).
func (fs *funcState) previousInstruction() *Instruction {
	if len(fs.f.Code) == 0 || fs.lastTarget <= len(fs.f.Code) {
		return nil
	}
	return &fs.f.Code[len(fs.f.Code)-1]
}

func (fs *funcState) searchUpvalue(name string) (upvalueIndex, bool) {
	upvals := fs.f.Upvalues
	upvals = upvals[:min(len(upvals), maxUpvalues)]
	for i := range upvals {
		if upvals[i].Name == name {
			return upvalueIndex(i), true
		}
	}
	return 0, false
}

// markUpval marks the block where the variable at the given level was defined
// (to emit close instructions later).
func (fs *funcState) markUpval(level int) {
	bl := fs.blocks
	for int(bl.numActiveVariables) > level {
		bl = bl.prev
	}
	bl.upval = true
	fs.needClose = true
}
