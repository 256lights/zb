// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package mylua

import (
	"errors"
	"fmt"

	"zb.256lights.llc/pkg/internal/luacode"
)

// callFrame represents an activation record of a Lua or Go function.
//
// The calling convention in this virtual machine
// is to place the function followed by its arguments on the [State] value stack.
// Registers are the N locations in the stack after the function,
// where N is the MaxStackSize declared in the [luacode.Prototype].
// Therefore, the first argument to a function will be in register 0.
// If the first instruction of a Lua function is [luacode.OpVarargPrep],
// then the extra arguments will be rotated before the function on the value stack.
type callFrame struct {
	// functionIndex is the index on State.stack where the function is located.
	functionIndex int
	// numExtraArguments is the number of arguments that were passed to the function
	// beyond the named parameters.
	numExtraArguments int
	// numResults is the number of expected results.
	numResults int
	// pc is the current instruction index in Prototype.Code
	// (the program counter).
	pc int
}

// framePointer returns the top of the value stack for the calling function.
func (frame callFrame) framePointer() int {
	return frame.functionIndex - frame.numExtraArguments
}

func (frame callFrame) registerStart() int {
	return frame.functionIndex + 1
}

func (frame callFrame) extraArgumentsRange() (start, end int) {
	return frame.framePointer(), frame.functionIndex
}

// loadLuaFrame obtains the top [callFrame] of the call stack,
// reads the [luaFunction] from the value stack,
// and returns a slice of the value stack to be used as the function's registers.
// loadLuaFrame returns an error if the value stack is not large enough
// for the function's local registers.
func (l *State) loadLuaFrame() (frame *callFrame, f luaFunction, registers []any, err error) {
	frame = l.frame()
	v := l.stack[frame.functionIndex]
	f, ok := v.(luaFunction)
	if !ok {
		return frame, luaFunction{}, nil, fmt.Errorf("internal error: call frame function is a %T", v)
	}
	registerStart := frame.registerStart()
	registerEnd := registerStart + int(f.proto.MaxStackSize)
	if !l.grow(registerEnd) {
		return frame, f, nil, errStackOverflow
	}
	registers = l.stack[registerStart:registerEnd]
	return frame, f, registers, nil
}

func (l *State) exec() error {
	if len(l.callStack) == 0 {
		panic("exec called on empty call stack")
	}
	callerDepth := len(l.callStack) - 1
	defer func() {
		clear(l.callStack[callerDepth:])
		l.callStack = l.callStack[:callerDepth]
	}()

	frame, f, registers, err := l.loadLuaFrame()
	callerValueTop := frame.framePointer()
	if err != nil {
		l.setTop(callerValueTop)
		return err
	}

	rkC := func(i luacode.Instruction) any {
		if i.K() {
			return importConstant(f.proto.Constants[i.ArgC()])
		} else {
			return registers[i.ArgC()]
		}
	}

	for len(l.callStack) > callerDepth {
		if frame.pc >= len(f.proto.Code) {
			l.setTop(callerValueTop)
			return fmt.Errorf("jumped out of bounds")
		}
		i := f.proto.Code[frame.pc]
		if !i.IsInTop() {
			// For instructions that don't read the stack top,
			// use the end of the registers.
			l.setTop(frame.registerStart() + int(f.proto.MaxStackSize))
		}
		nextPC := frame.pc + 1

		switch i.OpCode() {
		case luacode.OpMove:
			registers[i.ArgA()] = registers[i.ArgB()]
		case luacode.OpLoadI:
			registers[i.ArgA()] = int64(i.ArgBx())
		case luacode.OpLoadF:
			registers[i.ArgA()] = float64(i.ArgBx())
		case luacode.OpLoadK:
			registers[i.ArgA()] = importConstant(f.proto.Constants[i.ArgBx()])
		case luacode.OpLoadKX:
			arg, err := decodeExtraArg(frame, f.proto)
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
			registers[i.ArgA()] = importConstant(f.proto.Constants[arg])
		case luacode.OpLoadFalse:
			registers[i.ArgA()] = false
		case luacode.OpLFalseSkip:
			registers[i.ArgA()] = false
			nextPC = frame.pc + 2
		case luacode.OpLoadTrue:
			registers[i.ArgA()] = true
		case luacode.OpLoadNil:
			clear(registers[i.ArgA() : i.ArgA()+i.ArgB()])
		case luacode.OpGetUpval:
			registers[i.ArgA()] = f.upvalues[i.ArgB()]
		case luacode.OpSetUpval:
			f.upvalues[i.ArgB()] = registers[i.ArgA()]
		case luacode.OpGetTabUp:
			var err error
			registers[i.ArgA()], err = l.index(f.upvalues[i.ArgB()], importConstant(f.proto.Constants[i.ArgC()]))
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
		case luacode.OpGetTable:
			var err error
			registers[i.ArgA()], err = l.index(registers[i.ArgB()], registers[i.ArgC()])
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
		case luacode.OpGetI:
			var err error
			registers[i.ArgA()], err = l.index(registers[i.ArgB()], int64(i.ArgC()))
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
		case luacode.OpGetField:
			var err error
			registers[i.ArgA()], err = l.index(registers[i.ArgB()], importConstant(f.proto.Constants[i.ArgC()]))
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
		case luacode.OpSetTabUp:
			err := l.setIndex(
				f.upvalues[i.ArgA()],
				importConstant(f.proto.Constants[i.ArgB()]),
				rkC(i),
			)
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
		case luacode.OpSetTable:
			err := l.setIndex(
				registers[i.ArgA()],
				registers[i.ArgB()],
				rkC(i),
			)
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
		case luacode.OpSetI:
			err := l.setIndex(
				registers[i.ArgA()],
				int64(i.ArgB()),
				rkC(i),
			)
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
		case luacode.OpSetField:
			err := l.setIndex(
				registers[i.ArgA()],
				importConstant(f.proto.Constants[i.ArgB()]),
				rkC(i),
			)
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
		case luacode.OpNewTable:
			hashSizeLog2 := i.ArgB()
			hashSize := 0
			if hashSizeLog2 != 0 {
				hashSize = 1 << (hashSizeLog2 - 1)
			}
			arraySize := int(i.ArgC())
			if i.K() {
				arg, err := decodeExtraArg(frame, f.proto)
				if err != nil {
					l.setTop(callerValueTop)
					return err
				}
				arraySize += int(arg) * (1 << 8)
			}
			registers[i.ArgA()] = newTable(hashSize + arraySize)
		case luacode.OpSelf:
			rb := registers[i.ArgB()]
			registers[int(i.ArgA())+1] = rb
			var err error
			registers[i.ArgA()], err = l.index(rb, rkC(i))
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
		case luacode.OpReturn:
			resultStackStart := frame.registerStart() + int(i.ArgA())
			numResults := int(i.ArgB()) - 1
			if numResults < 0 {
				numResults = len(l.stack) - resultStackStart
			}
			if i.K() {
				l.setTop(callerValueTop)
				return errors.New("TODO(soon): close upvalues")
			}

			l.setTop(resultStackStart + numResults)
			l.finishCall(numResults)
			if len(l.callStack) <= callerDepth {
				return nil
			}
			frame, f, registers, err = l.loadLuaFrame()
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
		case luacode.OpReturn0:
			l.finishCall(0)
			frame, f, registers, err = l.loadLuaFrame()
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
		case luacode.OpReturn1:
			l.setTop(frame.registerStart() + int(i.ArgA()) + 1)
			l.finishCall(1)
			frame, f, registers, err = l.loadLuaFrame()
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
		case luacode.OpVarargPrep:
			if frame.pc != 0 {
				l.setTop(callerValueTop)
				return fmt.Errorf("%v must be first instruction in function", luacode.OpVarargPrep)
			}
			if frame.numExtraArguments != 0 {
				l.setTop(callerValueTop)
				return fmt.Errorf("cannot run %v multiple times", luacode.OpVarargPrep)
			}
			numArguments := len(l.stack) - frame.registerStart()
			numFixedParameters := int(i.ArgA())
			numExtraArguments := numArguments - numFixedParameters
			if numExtraArguments > 0 {
				rotate(l.stack[frame.functionIndex:], numExtraArguments)
				frame.functionIndex += numExtraArguments
				frame.numExtraArguments = numExtraArguments

				// Reload frame to update register slice.
				frame, f, registers, err = l.loadLuaFrame()
				if err != nil {
					l.setTop(callerValueTop)
					return err
				}
			}
		case luacode.OpExtraArg:
			return fmt.Errorf("unexpected %v at pc %d", luacode.OpExtraArg, frame.pc)
		default:
			return fmt.Errorf("unhandled instruction %v", i.OpCode())
		}

		frame.pc = nextPC
	}

	return nil
}

// finishCall moves the top numResults stack values
// to where the caller expects them.
func (l *State) finishCall(numResults int) {
	frame := l.frame()
	results := l.stack[len(l.stack)-numResults:]
	dest := l.stack[frame.framePointer():cap(l.stack)]
	numWantedResults := frame.numResults
	if numWantedResults == MultipleReturns {
		numWantedResults = numResults
	}

	n := copy(dest, results)
	if numWantedResults > n {
		clear(dest[n:numWantedResults])
	}
	// Clear out any registers the function was using.
	clear(dest[numWantedResults:])
	l.setTop(frame.framePointer() + numWantedResults)

	l.popCallStack()
}

func decodeExtraArg(frame *callFrame, proto *luacode.Prototype) (uint32, error) {
	argPC := frame.pc + 1
	if argPC >= len(proto.Code) {
		return 0, fmt.Errorf("%v (last instruction) expects extra argument", proto.Code[frame.pc].OpCode())
	}
	i := proto.Code[argPC]
	if got := i.OpCode(); got != luacode.OpExtraArg {
		return 0, fmt.Errorf("%v expects extra argument (found %v)", proto.Code[frame.pc].OpCode(), got)
	}
	return i.ArgAx(), nil
}
