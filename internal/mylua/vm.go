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
	if err := l.checkUpvalues(f.upvalues); err != nil {
		return frame, f, nil, err
	}
	registerStart := frame.registerStart()
	registerEnd := registerStart + int(f.proto.MaxStackSize)
	if !l.grow(registerEnd) {
		return frame, f, nil, errStackOverflow
	}
	registers = l.stack[registerStart:registerEnd]
	return frame, f, registers, nil
}

func (l *State) checkUpvalues(upvalues []upvalue) error {
	frame := l.frame()
	for i, uv := range upvalues {
		if uv.stackIndex >= frame.framePointer() {
			return fmt.Errorf("internal error: function upvalue [%d] inside current frame", i)
		}
	}
	return nil
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
			// This makes it safe to call metamethods.
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
			nextPC++
		case luacode.OpLoadTrue:
			registers[i.ArgA()] = true
		case luacode.OpLoadNil:
			clear(registers[i.ArgA() : i.ArgA()+i.ArgB()])
		case luacode.OpGetUpval:
			registers[i.ArgA()] = f.upvalues[i.ArgB()]
		case luacode.OpSetUpval:
			p := l.resolveUpvalue(f.upvalues[i.ArgB()])
			*p = registers[i.ArgA()]
		case luacode.OpGetTabUp:
			u := l.resolveUpvalue(f.upvalues[i.ArgB()])
			var err error
			registers[i.ArgA()], err = l.index(*u, importConstant(f.proto.Constants[i.ArgC()]))
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
			u := l.resolveUpvalue(f.upvalues[i.ArgA()])
			err := l.setIndex(
				*u,
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
		case luacode.OpAddI, luacode.OpSHRI:
			c := luacode.IntegerValue(int64(luacode.SignedArg(i.ArgC())))
			if rb, isNumber := exportNumericConstant(registers[i.ArgB()]); isNumber {
				op, ok := i.OpCode().ArithmeticOperator()
				if !ok {
					panic("operator should always be defined")
				}
				result, err := luacode.Arithmetic(op, rb, c)
				if err != nil {
					l.setTop(callerValueTop)
					return err
				}
				registers[i.ArgA()] = importConstant(result)
				// The next instruction is a fallback metamethod invocation.
				nextPC++
			}
		case luacode.OpSHLI:
			// Separate case because SHLI's arguments are in the opposite order.
			c := luacode.IntegerValue(int64(luacode.SignedArg(i.ArgC())))
			if rb, isNumber := exportNumericConstant(registers[i.ArgB()]); isNumber {
				result, err := luacode.Arithmetic(luacode.ShiftLeft, c, rb)
				if err != nil {
					l.setTop(callerValueTop)
					return err
				}
				registers[i.ArgA()] = importConstant(result)
				// The next instruction is a fallback metamethod invocation.
				nextPC++
			}
		case luacode.OpAddK,
			luacode.OpSubK,
			luacode.OpMulK,
			luacode.OpModK,
			luacode.OpPowK,
			luacode.OpDivK,
			luacode.OpIDivK,
			luacode.OpBAndK,
			luacode.OpBOrK,
			luacode.OpBXORK:
			kc := f.proto.Constants[i.ArgC()]
			if !kc.IsNumber() {
				l.setTop(callerValueTop)
				return fmt.Errorf("%v on non-numeric constant %v", i.OpCode(), kc)
			}
			if rb, isNumber := exportNumericConstant(registers[i.ArgB()]); isNumber {
				op, ok := i.OpCode().ArithmeticOperator()
				if !ok {
					panic("operator should always be defined")
				}
				result, err := luacode.Arithmetic(op, rb, kc)
				if err != nil {
					l.setTop(callerValueTop)
					return err
				}
				registers[i.ArgA()] = importConstant(result)
				// The next instruction is a fallback metamethod invocation.
				nextPC++
			}
		case luacode.OpAdd,
			luacode.OpSub,
			luacode.OpMul,
			luacode.OpMod,
			luacode.OpPow,
			luacode.OpDiv,
			luacode.OpIDiv,
			luacode.OpBAnd,
			luacode.OpBOr,
			luacode.OpBXOR,
			luacode.OpSHL,
			luacode.OpSHR:
			if rb, isNumber := exportNumericConstant(registers[i.ArgB()]); isNumber {
				if rc, isNumber := exportNumericConstant(registers[i.ArgC()]); isNumber {
					op, ok := i.OpCode().ArithmeticOperator()
					if !ok {
						panic("operator should always be defined")
					}
					result, err := luacode.Arithmetic(op, rb, rc)
					if err != nil {
						l.setTop(callerValueTop)
						return err
					}
					registers[i.ArgA()] = importConstant(result)
					// The next instruction is a fallback metamethod invocation.
					nextPC++
				}
			}
		case luacode.OpMMBin:
			prev, prevOperator, err := decodeBinaryMetamethod(frame, f.proto)
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
			registers[prev.ArgA()], err = l.arithmeticMetamethod(
				prevOperator.TagMethod(),
				registers[i.ArgA()],
				registers[i.ArgB()],
			)
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
		case luacode.OpMMBinI:
			prev, prevOperator, err := decodeBinaryMetamethod(frame, f.proto)
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
			registers[prev.ArgA()], err = l.arithmeticMetamethod(
				prevOperator.TagMethod(),
				registers[i.ArgA()],
				int64(luacode.SignedArg(i.ArgB())),
			)
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
		case luacode.OpMMBinK:
			prev, prevOperator, err := decodeBinaryMetamethod(frame, f.proto)
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
			registers[prev.ArgA()], err = l.arithmeticMetamethod(
				prevOperator.TagMethod(),
				registers[i.ArgA()],
				f.proto.Constants[i.ArgB()],
			)
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
		case luacode.OpUNM:
			rb := registers[i.ArgB()]
			if ib, ok := rb.(int64); ok {
				registers[i.ArgA()] = -ib
			} else if nb, ok := toNumber(rb); ok {
				registers[i.ArgA()] = -nb
			} else {
				var err error
				registers[i.ArgA()], err = l.arithmeticMetamethod(luacode.TagMethodUNM, rb, rb)
				if err != nil {
					l.setTop(callerValueTop)
					return err
				}
			}
		case luacode.OpBNot:
			rb := registers[i.ArgB()]
			if ib, ok := rb.(int64); ok {
				registers[i.ArgA()] = int64(^uint64(ib))
			} else {
				var err error
				registers[i.ArgA()], err = l.arithmeticMetamethod(luacode.TagMethodBNot, rb, rb)
				if err != nil {
					l.setTop(callerValueTop)
					return err
				}
			}
		case luacode.OpNot:
			registers[i.ArgA()] = !toBoolean(registers[i.ArgB()])
		case luacode.OpJMP:
			nextPC += int(i.J())
		case luacode.OpTest:
			cond := toBoolean(registers[i.ArgA()])
			if cond != i.K() {
				nextPC++
			}
		case luacode.OpTestSet:
			rb := registers[i.ArgB()]
			cond := toBoolean(rb)
			if cond != i.K() {
				nextPC++
			} else {
				registers[i.ArgA()] = rb
			}
		case luacode.OpCall:
			numArguments := int(i.ArgB())
			numResults := int(i.ArgC()) - 1
			l.setTop(frame.registerStart() + int(i.ArgA()) + 1 + numArguments)
			isLua, err := l.prepareCall(numArguments, numResults)
			if err != nil {
				l.setTop(callerValueTop)
				return err
			}
			if isLua {
				frame, f, registers, err = l.loadLuaFrame()
				if err != nil {
					l.setTop(callerValueTop)
					return err
				}
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
		case luacode.OpSetList:
			t := registers[i.ArgA()]
			for idx := range i.ArgB() {
				err := l.setIndex(t, int64(idx)+1, registers[i.ArgC()+idx+1])
				if err != nil {
					l.setTop(callerValueTop)
					return err
				}
			}
		case luacode.OpClosure:
			p := f.proto.Functions[i.ArgBx()]
			upvalues := make([]upvalue, len(p.Upvalues))
			for i, uv := range p.Upvalues {
				if uv.InStack {
					upvalues[i] = stackUpvalue(frame.registerStart() + int(uv.Index))
				} else {
					upvalues[i] = f.upvalues[uv.Index]
				}
			}
			registers[i.ArgA()] = luaFunction{
				id:       nextID(),
				proto:    p,
				upvalues: upvalues,
			}
		case luacode.OpVararg:
			numWanted := int(i.ArgC()) - 1
			if numWanted == MultipleReturns {
				numWanted = frame.numExtraArguments
			}
			a := frame.registerStart() + int(i.ArgA())
			if !l.grow(a + numWanted) {
				l.setTop(callerValueTop)
				return errStackOverflow
			}
			l.setTop(a + numWanted)
			varargStart, varargEnd := frame.extraArgumentsRange()
			n := copy(l.stack[a:], l.stack[varargStart:varargEnd])
			clear(l.stack[a+n:])
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

func (l *State) arithmeticMetamethod(event luacode.TagMethod, arg1, arg2 any) (any, error) {
	op, isArithmetic := event.ArithmeticOperator()
	if !isArithmetic {
		return nil, fmt.Errorf("%v is not an arithmetic metamethod", event)
	}

	eventName := event.String()
	if f := l.metatable(arg1).get(stringValue{s: eventName}); f != nil {
		return l.call1(f, arg1, arg2)
	}
	if f := l.metatable(arg2).get(stringValue{s: eventName}); f != nil {
		return l.call1(f, arg1, arg2)
	}

	kind := "arithmetic"
	if op.IsIntegral() {
		if valueType(arg1) == TypeNumber && valueType(arg2) == TypeNumber {
			return nil, luacode.ErrNotInteger
		}
		kind = "bitwise operation"
	}
	var tname string
	if valueType(arg1) == TypeNumber {
		tname = l.typeName(arg2)
	} else {
		tname = l.typeName(arg1)
	}
	return nil, fmt.Errorf("attempt to perform %s on a %s value", kind, tname)
}

func decodeBinaryMetamethod(frame *callFrame, proto *luacode.Prototype) (luacode.Instruction, luacode.ArithmeticOperator, error) {
	i := proto.Code[frame.pc]
	if frame.pc <= 0 {
		return 0, 0, fmt.Errorf("decode error: %v must be preceded by binary arithmetic instruction", i.OpCode())
	}
	prev := proto.Code[frame.pc-1]
	prevOpCode := prev.OpCode()
	prevOperator, isArithmetic := prevOpCode.ArithmeticOperator()
	if !isArithmetic || !prevOperator.IsBinary() {
		return 0, 0, fmt.Errorf("decode error: %v must be preceded by binary arithmetic instruction (found %v)", i.OpCode(), prevOpCode)
	}
	if got, want := luacode.TagMethod(i.ArgC()), prevOperator.TagMethod(); got != want {
		err := fmt.Errorf("decode error: found metamethod %v in %v after %v (expected %v)",
			got, i.OpCode(), prev.OpCode(), want)
		return prev, prevOperator, err
	}
	return prev, prevOperator, nil
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
