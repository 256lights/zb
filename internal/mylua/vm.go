// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package mylua

import (
	"errors"
	"fmt"

	"zb.256lights.llc/pkg/internal/luacode"
)

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

func (frame callFrame) registerStart() int {
	return frame.functionIndex + 1
}

func (frame callFrame) extraArgumentsRange() (start, end int) {
	end = frame.functionIndex
	start = frame.functionIndex - frame.numExtraArguments
	return
}

func (l *State) loadFrame(frame *callFrame) (f luaFunction, registers []any, err error) {
	v := l.stack[frame.functionIndex]
	f, ok := v.(luaFunction)
	if !ok {
		return luaFunction{}, nil, fmt.Errorf("internal error: call frame function is a %T", v)
	}
	registerStart := frame.registerStart()
	registerEnd := registerStart + int(f.proto.MaxStackSize)
	if !l.grow(registerEnd) {
		return f, nil, errStackOverflow
	}
	registers = l.stack[registerStart:registerEnd]
	return f, registers, nil
}

func (l *State) exec() error {
	startFrame := len(l.callStack) - 1
	frame := &l.callStack[startFrame]
	f, registers, err := l.loadFrame(frame)
	if err != nil {
		return err
	}

	for len(l.callStack) > startFrame {
		if frame.pc >= len(f.proto.Code) {
			return fmt.Errorf("jumped out of bounds")
		}
		nextPC := frame.pc + 1

		switch i := f.proto.Code[frame.pc]; i.OpCode() {
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
		case luacode.OpReturn:
			resultStackStart := frame.registerStart() + int(i.ArgA())
			numResults := int(i.ArgB()) - 1
			if numResults < 0 {
				numResults = len(l.stack) - resultStackStart
			}
			if i.K() {
				return errors.New("TODO(soon): close upvalues")
			}

			l.setTop(resultStackStart + numResults)
			if c := i.ArgC(); c > 0 {
				// Function is variadic.
				// Restore function index to its original position.
				numNamedParameters := int(c - 1)
				frame.functionIndex -= frame.numExtraArguments + numNamedParameters + 1
			}
			frame = l.finishCall(numResults)
			if len(l.callStack) <= startFrame {
				return nil
			}
			f, registers, err = l.loadFrame(frame)
			if err != nil {
				return err
			}
		case luacode.OpReturn0:
			frame = l.finishCall(0)
			f, registers, err = l.loadFrame(frame)
			if err != nil {
				return err
			}
		case luacode.OpReturn1:
			l.setTop(frame.registerStart() + int(i.ArgA()) + 1)
			frame = l.finishCall(1)
			f, registers, err = l.loadFrame(frame)
			if err != nil {
				return err
			}
		case luacode.OpVarargPrep:
			if err := l.varargPrep(frame, int(i.ArgA()), int(f.proto.MaxStackSize)); err != nil {
				return err
			}
			f, registers, err = l.loadFrame(frame)
			if err != nil {
				return err
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

func (l *State) varargPrep(frame *callFrame, numFixedParameters, maxStackSize int) error {
	numArguments := len(l.stack) - (frame.functionIndex + 1)
	numExtraArguments := numArguments - numFixedParameters
	frame.numExtraArguments = numExtraArguments

	if !l.grow(len(l.stack) + maxStackSize + 1) {
		return errStackOverflow
	}
	l.stack = append(l.stack, l.stack[frame.functionIndex])
	fixedArguments := l.stack[frame.functionIndex+1 : frame.functionIndex+1+numFixedParameters]
	l.stack = append(l.stack, fixedArguments...)
	clear(fixedArguments)
	frame.functionIndex += numArguments + 1
	return nil
}

// finishCall moves the top numResults stack values
// to where the caller expects them.
func (l *State) finishCall(numResults int) *callFrame {
	frame := l.frame()
	results := l.stack[len(l.stack)-numResults:]
	dest := l.stack[frame.functionIndex:cap(l.stack)]
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
	l.setTop(frame.functionIndex + numWantedResults)

	l.popCallStack()
	return &l.callStack[len(l.callStack)-1]
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
