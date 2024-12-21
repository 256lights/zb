// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package mylua

import (
	"errors"
	"fmt"
	"math"

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
func (l *State) loadLuaFrame() (frame *callFrame, f luaFunction, err error) {
	frame = l.frame()
	v := l.stack[frame.functionIndex]
	f, ok := v.(luaFunction)
	if !ok {
		return frame, luaFunction{}, fmt.Errorf("internal error: call frame function is a %T", v)
	}
	if err := l.checkUpvalues(f.upvalues); err != nil {
		return frame, f, err
	}
	registerEnd := frame.registerStart() + int(f.proto.MaxStackSize)
	if !l.grow(registerEnd) {
		return frame, f, errStackOverflow
	}
	return frame, f, nil
}

func (l *State) exec() (err error) {
	if len(l.callStack) == 0 {
		panic("exec called on empty call stack")
	}
	callerDepth := len(l.callStack) - 1
	frame, f, firstLoadError := l.loadLuaFrame()
	defer func() {
		if err != nil {
			// TODO(someday): Message handler.
		}

		// Unwind stack.
		for len(l.callStack) > callerDepth {
			base := l.frame().registerStart()
			l.closeUpvalues(base)
			err = l.closeTBCSlots(base, false, err)
			fp := l.frame().framePointer()
			for i := uint(fp); i < uint(base); i++ {
				if l.tbc.Has(i) {
					panic("TBC between activation records")
				}
			}
			l.setTop(fp)

			n := len(l.callStack) - 1
			l.callStack[n] = callFrame{}
			l.callStack = l.callStack[:n]
		}
	}()
	if firstLoadError != nil {
		return firstLoadError
	}

	// registers returns the slice of l.stack
	// that represents the register file for the function at the top of the call stack.
	registers := func() []value {
		start := frame.registerStart()
		return l.stack[start : start+int(f.proto.MaxStackSize)]
	}

	// register returns a pointer to the element of l.stack
	// for the i'th register of the function at the top of the call stack.
	register := func(r []value, i uint8) (*value, error) {
		if int(i) >= len(r) {
			return nil, fmt.Errorf("%s: decode instruction: register %d out-of-bounds (stack is %d slots)",
				sourceLocation(f.proto, frame.pc-1), i, len(r))
		}
		return &r[i], nil
	}

	numericForLoopRegisters := func(r []value, i uint8) (idx, limit, step, control *value, err error) {
		if int(i)+4 > len(r) {
			return nil, nil, nil, nil, fmt.Errorf("%s: decode instruction: for loop registers [%d,%d] out-of-bounds (stack is %d slots)",
				sourceLocation(f.proto, frame.pc-1), i, i+3, len(r))
		}
		return &r[i], &r[i+1], &r[i+2], &r[i+3], nil
	}

	const genericForLoopStateSize = 4
	genericForLoopRegisters := func(r []value, i uint8) (state *[genericForLoopStateSize]value, err error) {
		if int(i)+genericForLoopStateSize > len(r) {
			return nil, fmt.Errorf("%s: decode instruction: for loop registers [%d,%d] out-of-bounds (stack is %d slots)",
				sourceLocation(f.proto, frame.pc-1), i, i+(genericForLoopStateSize-1), len(r))
		}
		return (*[genericForLoopStateSize]value)(r[i : i+genericForLoopStateSize]), nil
	}

	constant := func(i uint32) (luacode.Value, error) {
		if int64(i) >= int64(len(f.proto.Constants)) {
			return luacode.Value{}, fmt.Errorf("%s: decode instruction: constant %d out-of-bounds (table has %d entries)", sourceLocation(f.proto, frame.pc-1), i, len(f.proto.Constants))
		}
		return f.proto.Constants[i], nil
	}

	fUpvalue := func(i uint8) (*value, error) {
		if int(i) >= len(f.upvalues) {
			return nil, fmt.Errorf("%s: decode instruction: upvalue %d out-of-bounds (function has %d upvalues)", sourceLocation(f.proto, frame.pc-1), i, len(f.upvalues))
		}
		return l.resolveUpvalue(f.upvalues[i]), nil
	}

	rkC := func(r []value, i luacode.Instruction) (value, error) {
		c := i.ArgC()
		if i.K() {
			kc, err := constant(uint32(c))
			if err != nil {
				return nil, err
			}
			return importConstant(kc), nil
		} else {
			rc, err := register(r, c)
			if err != nil {
				return nil, err
			}
			return *rc, nil
		}
	}

	for {
		if frame.pc < 0 || frame.pc >= len(f.proto.Code) {
			return fmt.Errorf("%s: jumped out of bounds", functionLocation(f.proto))
		}
		i := f.proto.Code[frame.pc]
		frame.pc++
		if !i.IsInTop() {
			// For instructions that don't read the stack top,
			// use the end of the registers.
			// This makes it safe to call metamethods.
			l.setTop(frame.registerStart() + int(f.proto.MaxStackSize))
		}

		switch opCode := i.OpCode(); opCode {
		case luacode.OpMove:
			r := registers()
			ra, err := register(r, i.ArgA())
			if err != nil {
				return err
			}
			rb, err := register(r, i.ArgB())
			if err != nil {
				return err
			}
			*ra = *rb
		case luacode.OpLoadI:
			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			*ra = integerValue(i.ArgBx())
		case luacode.OpLoadF:
			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			*ra = floatValue(i.ArgBx())
		case luacode.OpLoadK:
			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			kb, err := constant(uint32(i.ArgBx()))
			if err != nil {
				return err
			}
			*ra = importConstant(kb)
		case luacode.OpLoadKX:
			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			arg, err := decodeExtraArg(frame, f.proto)
			if err != nil {
				return err
			}
			frame.pc++ // Skip extra arg.
			karg, err := constant(arg)
			if err != nil {
				return err
			}
			*ra = importConstant(karg)
		case luacode.OpLoadFalse:
			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			*ra = booleanValue(false)
		case luacode.OpLFalseSkip:
			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			*ra = booleanValue(false)
			frame.pc++
		case luacode.OpLoadTrue:
			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			*ra = booleanValue(true)
		case luacode.OpLoadNil:
			start := i.ArgA()
			end := start + i.ArgB()
			if end > start {
				r := registers()
				if _, err := register(r, end-1); err != nil {
					return err
				}
				clear(r[start:end])
			}
		case luacode.OpGetUpval:
			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			ub, err := fUpvalue(i.ArgB())
			if err != nil {
				return err
			}
			*ra = *ub
		case luacode.OpSetUpval:
			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			ub, err := fUpvalue(i.ArgB())
			if err != nil {
				return err
			}
			*ub = *ra
		case luacode.OpGetTabUp:
			if _, err := register(registers(), i.ArgA()); err != nil {
				return err
			}
			ub, err := fUpvalue(i.ArgB())
			if err != nil {
				return err
			}
			kc, err := constant(uint32(i.ArgC()))
			if err != nil {
				return err
			}
			result, err := l.index(*ub, importConstant(kc))
			if err != nil {
				return err
			}
			// index may call a metamethod and grow the stack,
			// so get register address afterward to avoid referencing an old array.
			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			*ra = result
		case luacode.OpGetTable:
			r := registers()
			if _, err := register(r, i.ArgA()); err != nil {
				return err
			}
			rb, err := register(r, i.ArgB())
			if err != nil {
				return err
			}
			rc, err := register(r, i.ArgC())
			if err != nil {
				return err
			}
			result, err := l.index(*rb, *rc)
			if err != nil {
				return err
			}
			// index may call a metamethod and grow the stack,
			// so get register address afterward to avoid referencing an old array.
			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			*ra = result
		case luacode.OpGetI:
			r := registers()
			if _, err := register(r, i.ArgA()); err != nil {
				return err
			}
			rb, err := register(r, i.ArgB())
			if err != nil {
				return err
			}
			result, err := l.index(*rb, integerValue(i.ArgC()))
			if err != nil {
				return err
			}
			// index may call a metamethod and grow the stack,
			// so get register address afterward to avoid referencing an old array.
			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			*ra = result
		case luacode.OpGetField:
			r := registers()
			if _, err := register(r, i.ArgA()); err != nil {
				return err
			}
			rb, err := register(r, i.ArgB())
			if err != nil {
				return err
			}
			kc, err := constant(uint32(i.ArgC()))
			if err != nil {
				return err
			}
			result, err := l.index(*rb, importConstant(kc))
			if err != nil {
				return err
			}
			// index may call a metamethod and grow the stack,
			// so get register address afterward to avoid referencing an old array.
			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			*ra = result
		case luacode.OpSetTabUp:
			ua, err := fUpvalue(i.ArgA())
			if err != nil {
				return err
			}
			kb, err := constant(uint32(i.ArgB()))
			if err != nil {
				return err
			}
			c, err := rkC(registers(), i)
			if err != nil {
				return err
			}
			if err := l.setIndex(*ua, importConstant(kb), c); err != nil {
				return err
			}
		case luacode.OpSetTable:
			r := registers()
			ra, err := register(r, i.ArgA())
			if err != nil {
				return err
			}
			rb, err := register(r, i.ArgA())
			if err != nil {
				return err
			}
			c, err := rkC(registers(), i)
			if err != nil {
				return err
			}
			if err := l.setIndex(*ra, *rb, c); err != nil {
				return err
			}
		case luacode.OpSetI:
			r := registers()
			ra, err := register(r, i.ArgA())
			if err != nil {
				return err
			}
			c, err := rkC(registers(), i)
			if err != nil {
				return err
			}
			if err := l.setIndex(*ra, integerValue(i.ArgB()), c); err != nil {
				return err
			}
		case luacode.OpSetField:
			r := registers()
			ra, err := register(r, i.ArgA())
			if err != nil {
				return err
			}
			kb, err := constant(uint32(i.ArgB()))
			if err != nil {
				return err
			}
			c, err := rkC(registers(), i)
			if err != nil {
				return err
			}
			if err := l.setIndex(*ra, importConstant(kb), c); err != nil {
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
					return err
				}
				arraySize += int(arg) * (1 << 8)
			}
			frame.pc++ // Extra arg is always present even if unused.

			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			*ra = newTable(hashSize + arraySize)
		case luacode.OpSelf:
			r := registers()
			a := i.ArgA()
			ra1, err := register(r, a+1)
			if err != nil {
				return err
			}
			rb, err := register(r, i.ArgB())
			if err != nil {
				return err
			}
			c, err := rkC(r, i)
			if err != nil {
				return err
			}

			*ra1 = *rb
			result, err := l.index(*rb, c)
			if err != nil {
				return err
			}
			// index may call a metamethod and grow the stack,
			// so get register address afterward to avoid referencing an old array.
			ra, err := register(registers(), a)
			if err != nil {
				return err
			}
			*ra = result
		case luacode.OpAddI, luacode.OpSHRI:
			r := registers()
			ra, err := register(r, i.ArgA())
			if err != nil {
				return err
			}
			rb, err := register(r, i.ArgB())
			if err != nil {
				return err
			}
			c := luacode.IntegerValue(int64(luacode.SignedArg(i.ArgC())))
			if kb, isNumber := exportNumericConstant(*rb); isNumber {
				op, ok := opCode.ArithmeticOperator()
				if !ok {
					panic("operator should always be defined")
				}
				result, err := luacode.Arithmetic(op, kb, c)
				if err != nil {
					return err
				}
				*ra = importConstant(result)
				// The next instruction is a fallback metamethod invocation.
				frame.pc++
			}
		case luacode.OpSHLI:
			// Separate case because SHLI's arguments are in the opposite order.
			r := registers()
			ra, err := register(r, i.ArgA())
			if err != nil {
				return err
			}
			rb, err := register(r, i.ArgB())
			if err != nil {
				return err
			}
			c := luacode.IntegerValue(int64(luacode.SignedArg(i.ArgC())))
			if kb, isNumber := exportNumericConstant(*rb); isNumber {
				result, err := luacode.Arithmetic(luacode.ShiftLeft, c, kb)
				if err != nil {
					return err
				}
				*ra = importConstant(result)
				// The next instruction is a fallback metamethod invocation.
				frame.pc++
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
			r := registers()
			ra, err := register(r, i.ArgA())
			if err != nil {
				return err
			}
			rb, err := register(r, i.ArgB())
			if err != nil {
				return err
			}
			kc, err := constant(uint32(i.ArgC()))
			if err != nil {
				return err
			}
			if !kc.IsNumber() {
				return fmt.Errorf("%s: decode instruction: %v on non-numeric constant %v", sourceLocation(f.proto, frame.pc-1), opCode, kc)
			}
			if rb, isNumber := exportNumericConstant(*rb); isNumber {
				op, ok := opCode.ArithmeticOperator()
				if !ok {
					panic("operator should always be defined")
				}
				result, err := luacode.Arithmetic(op, rb, kc)
				if err != nil {
					return err
				}
				*ra = importConstant(result)
				// The next instruction is a fallback metamethod invocation.
				frame.pc++
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
			r := registers()
			ra, err := register(r, i.ArgA())
			if err != nil {
				return err
			}
			rb, err := register(r, i.ArgB())
			if err != nil {
				return err
			}
			rc, err := register(r, i.ArgC())
			if err != nil {
				return err
			}
			if kb, isNumber := exportNumericConstant(*rb); isNumber {
				if kc, isNumber := exportNumericConstant(*rc); isNumber {
					op, ok := opCode.ArithmeticOperator()
					if !ok {
						panic("operator should always be defined")
					}
					result, err := luacode.Arithmetic(op, kb, kc)
					if err != nil {
						return err
					}
					*ra = importConstant(result)
					// The next instruction is a fallback metamethod invocation.
					frame.pc++
				}
			}
		case luacode.OpMMBin:
			resultRegister, prevOperator, err := decodeBinaryMetamethod(frame, f.proto)
			if err != nil {
				return err
			}
			r := registers()
			if _, err := register(r, resultRegister); err != nil {
				return err
			}
			ra, err := register(r, i.ArgA())
			if err != nil {
				return err
			}
			rb, err := register(r, i.ArgB())
			if err != nil {
				return err
			}

			result, err := l.arithmeticMetamethod(prevOperator.TagMethod(), *ra, *rb)
			if err != nil {
				return err
			}
			// Calling a metamethod may grow the stack,
			// so get register address afterward to avoid referencing an old array.
			prevRA, err := register(registers(), resultRegister)
			if err != nil {
				return err
			}
			*prevRA = result
		case luacode.OpMMBinI:
			resultRegister, prevOperator, err := decodeBinaryMetamethod(frame, f.proto)
			if err != nil {
				return err
			}
			r := registers()
			if _, err := register(r, resultRegister); err != nil {
				return err
			}
			ra, err := register(r, i.ArgA())
			if err != nil {
				return err
			}
			result, err := l.arithmeticMetamethod(
				prevOperator.TagMethod(),
				*ra,
				integerValue(luacode.SignedArg(i.ArgB())),
			)
			if err != nil {
				return err
			}
			// Calling a metamethod may grow the stack,
			// so get register address afterward to avoid referencing an old array.
			prevRA, err := register(registers(), resultRegister)
			if err != nil {
				return err
			}
			*prevRA = result
		case luacode.OpMMBinK:
			resultRegister, prevOperator, err := decodeBinaryMetamethod(frame, f.proto)
			if err != nil {
				return err
			}
			r := registers()
			if _, err := register(r, resultRegister); err != nil {
				return err
			}
			ra, err := register(r, i.ArgA())
			if err != nil {
				return err
			}
			kb, err := constant(uint32(i.ArgB()))
			if err != nil {
				return err
			}
			result, err := l.arithmeticMetamethod(
				prevOperator.TagMethod(),
				*ra,
				importConstant(kb),
			)
			if err != nil {
				return err
			}
			// Calling a metamethod may grow the stack,
			// so get register address afterward to avoid referencing an old array.
			prevRA, err := register(registers(), resultRegister)
			if err != nil {
				return err
			}
			*prevRA = result
		case luacode.OpUNM:
			r := registers()
			a := i.ArgA()
			ra, err := register(r, a)
			if err != nil {
				return err
			}
			rb, err := register(r, i.ArgB())
			if err != nil {
				return err
			}

			if ib, ok := (*rb).(integerValue); ok {
				*ra = -ib
			} else if nb, ok := toNumber(*rb); ok {
				*ra = -nb
			} else {
				result, err := l.arithmeticMetamethod(luacode.TagMethodUNM, *rb, *rb)
				if err != nil {
					return err
				}
				// Calling a metamethod may grow the stack,
				// so get register address afterward to avoid referencing an old array.
				ra, err = register(registers(), a)
				if err != nil {
					return err
				}
				*ra = result
			}
		case luacode.OpBNot:
			r := registers()
			a := i.ArgA()
			ra, err := register(r, a)
			if err != nil {
				return err
			}
			rb, err := register(r, i.ArgB())
			if err != nil {
				return err
			}

			if ib, ok := (*rb).(integerValue); ok {
				kb := luacode.IntegerValue(int64(ib))
				result, err := luacode.Arithmetic(luacode.BitwiseNot, kb, luacode.Value{})
				if err != nil {
					return err
				}
				*ra = importConstant(result)
			} else {
				result, err := l.arithmeticMetamethod(luacode.TagMethodBNot, *rb, *rb)
				if err != nil {
					return err
				}
				// Calling a metamethod may grow the stack,
				// so get register address afterward to avoid referencing an old array.
				ra, err = register(registers(), a)
				if err != nil {
					return err
				}
				*ra = result
			}
		case luacode.OpNot:
			r := registers()
			ra, err := register(r, i.ArgA())
			if err != nil {
				return err
			}
			rb, err := register(r, i.ArgB())
			if err != nil {
				return err
			}
			*ra = booleanValue(!toBoolean(*rb))
		case luacode.OpLen:
			a := i.ArgA()
			if _, err := register(registers(), a); err != nil {
				return err
			}
			rb, err := register(registers(), i.ArgB())
			if err != nil {
				return err
			}
			result, err := l.len(*rb)
			if err != nil {
				return err
			}
			// Calling a metamethod may grow the stack,
			// so get register address afterward to avoid referencing an old array.
			ra, err := register(registers(), a)
			if err != nil {
				return err
			}
			*ra = result
		case luacode.OpConcat:
			a, b := i.ArgA(), i.ArgB()
			top := int(a) + int(b)
			if top > int(f.proto.MaxStackSize) {
				return fmt.Errorf("%s: decode instruction: concat: register %d out-of-bounds (stack is %d slots)",
					sourceLocation(f.proto, frame.pc-1), top-1, f.proto.MaxStackSize)
			}
			l.setTop(frame.registerStart() + top)
			if err := l.concat(int(b)); err != nil {
				return err
			}
		case luacode.OpClose:
			a := i.ArgA()
			if a >= f.proto.MaxStackSize {
				return fmt.Errorf("%s: decode instruction: register %d out-of-bounds (stack is %d slots)",
					sourceLocation(f.proto, frame.pc-1), a, f.proto.MaxStackSize)
			}
			bottom := frame.registerStart() + int(a)
			l.closeUpvalues(bottom)
			if err := l.closeTBCSlots(bottom, true, nil); err != nil {
				return err
			}
		case luacode.OpTBC:
			a := i.ArgA()
			if a >= f.proto.MaxStackSize {
				return fmt.Errorf("%s: decode instruction: register %d out-of-bounds (stack is %d slots)",
					sourceLocation(f.proto, frame.pc-1), a, f.proto.MaxStackSize)
			}
			if err := l.markTBC(frame.registerStart() + int(a)); err != nil {
				return fmt.Errorf("%s: %v", sourceLocation(f.proto, frame.pc-1), err)
			}
		case luacode.OpJMP:
			frame.pc += int(i.J())
		case luacode.OpEQ:
			r := registers()
			ra, err := register(r, i.ArgA())
			if err != nil {
				return err
			}
			rb, err := register(r, i.ArgB())
			if err != nil {
				return err
			}
			result, err := l.equal(*ra, *rb)
			if err != nil {
				return err
			}
			if result != i.K() {
				frame.pc++
			}
		case luacode.OpEQK:
			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			kb, err := constant(uint32(i.ArgB()))
			if err != nil {
				return err
			}
			result, err := l.equal(*ra, importConstant(kb))
			if err != nil {
				return err
			}
			if result != i.K() {
				frame.pc++
			}
		case luacode.OpEQI:
			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			result, err := l.equal(*ra, integerValue(luacode.SignedArg(i.ArgB())))
			if err != nil {
				return err
			}
			if result != i.K() {
				frame.pc++
			}
		case luacode.OpLT, luacode.OpLE:
			r := registers()
			ra, err := register(r, i.ArgA())
			if err != nil {
				return err
			}
			rb, err := register(r, i.ArgB())
			if err != nil {
				return err
			}
			op := Less
			if opCode == luacode.OpLE {
				op = LessOrEqual
			}
			result, err := l.compare(op, *ra, *rb)
			if err != nil {
				return err
			}
			if result != i.K() {
				frame.pc++
			}
		case luacode.OpLTI, luacode.OpLEI:
			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			op := Less
			if opCode == luacode.OpLEI {
				op = LessOrEqual
			}
			result, err := l.compare(op, *ra, integerValue(luacode.SignedArg(i.ArgB())))
			if err != nil {
				return err
			}
			if result != i.K() {
				frame.pc++
			}
		case luacode.OpGTI, luacode.OpGEI:
			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			// According to the Lua reference manual,
			// "A comparison a > b is translated to b < a and a >= b is translated to b <= a."
			// https://www.lua.org/manual/5.4/manual.html#3.4.4
			op := Less
			if opCode == luacode.OpGEI {
				op = LessOrEqual
			}
			result, err := l.compare(op, integerValue(luacode.SignedArg(i.ArgB())), *ra)
			if err != nil {
				return err
			}
			if result != i.K() {
				frame.pc++
			}
		case luacode.OpTest:
			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			cond := toBoolean(*ra)
			if cond != i.K() {
				frame.pc++
			}
		case luacode.OpTestSet:
			r := registers()
			ra, err := register(r, i.ArgA())
			if err != nil {
				return err
			}
			rb, err := register(r, i.ArgB())
			if err != nil {
				return err
			}
			cond := toBoolean(*rb)
			if cond != i.K() {
				frame.pc++
			} else {
				*ra = *rb
			}
		case luacode.OpCall:
			numArguments := int(i.ArgB()) - 1
			numResults := int(i.ArgC()) - 1
			// TODO(soon): Validate ArgA.
			functionIndex := frame.registerStart() + int(i.ArgA())
			if numArguments < 0 {
				// Varargs: read from top.
				numArguments = len(l.stack) - (functionIndex + 1)
			} else {
				l.setTop(functionIndex + 1 + numArguments)
			}
			isLua, err := l.prepareCall(numArguments, numResults)
			if err != nil {
				return err
			}
			if isLua {
				frame, f, err = l.loadLuaFrame()
				if err != nil {
					return err
				}
			}
		case luacode.OpTailCall:
			if maxTBC, hasTBC := l.tbc.Max(); hasTBC && maxTBC >= uint(frame.registerStart()) {
				return fmt.Errorf("%s: internal error: cannot make tail call when block has to-be-closed variables in scope",
					sourceLocation(f.proto, frame.pc-1))
			}

			numArguments := int(i.ArgB()) - 1
			numResults := l.frame().numResults
			// TODO(soon): Validate ArgA.
			functionIndex := frame.registerStart() + int(i.ArgA())

			l.closeUpvalues(frame.registerStart())
			if numArguments < 0 {
				// Varargs: read from top.
				numArguments = len(l.stack) - (functionIndex + 1)
			} else {
				l.setTop(functionIndex + 1 + numArguments)
			}
			fp := frame.framePointer()
			copy(l.stack[fp:], l.stack[functionIndex:])
			l.setTop(fp + 1 + numArguments)
			l.callStack = l.callStack[:len(l.callStack)-1]
			isLua, err := l.prepareCall(numArguments, numResults)
			if err != nil {
				return err
			}
			if isLua {
				frame, f, err = l.loadLuaFrame()
				if err != nil {
					return err
				}
			}
		case luacode.OpReturn:
			// TODO(soon): Validate ArgA+numResults.
			resultStackStart := frame.registerStart() + int(i.ArgA())
			numResults := int(i.ArgB()) - 1
			if numResults < 0 {
				numResults = len(l.stack) - resultStackStart
			}
			// We ignore the K hint and close locals regardless.
			l.closeUpvalues(frame.registerStart())
			if err := l.closeTBCSlots(frame.registerStart(), true, nil); err != nil {
				return err
			}

			l.setTop(resultStackStart + numResults)
			l.finishCall(numResults)
			if len(l.callStack) <= callerDepth {
				return nil
			}
			frame, f, err = l.loadLuaFrame()
			if err != nil {
				return err
			}
		case luacode.OpReturn0:
			// The RETURN0 instruction shouldn't be generated if we need to close locals,
			// but for safety, we do it anyway.
			l.closeUpvalues(frame.registerStart())
			if err := l.closeTBCSlots(frame.registerStart(), false, nil); err != nil {
				return err
			}

			l.finishCall(0)
			if len(l.callStack) <= callerDepth {
				return nil
			}
			frame, f, err = l.loadLuaFrame()
			if err != nil {
				return err
			}
		case luacode.OpReturn1:
			// The RETURN1 instruction shouldn't be generated if we need to close locals,
			// but for safety, we do it anyway.
			l.closeUpvalues(frame.registerStart())
			if err := l.closeTBCSlots(frame.registerStart(), true, nil); err != nil {
				return err
			}

			// TODO(soon): Validate ArgA.

			l.setTop(frame.registerStart() + int(i.ArgA()) + 1)
			l.finishCall(1)
			if len(l.callStack) <= callerDepth {
				return nil
			}
			frame, f, err = l.loadLuaFrame()
			if err != nil {
				return err
			}
		case luacode.OpForLoop:
			idx, limit, step, control, err := numericForLoopRegisters(registers(), i.ArgA())
			if err != nil {
				return err
			}
			switch step := (*step).(type) {
			case integerValue:
				indexInteger, ok := (*idx).(integerValue)
				if !ok {
					return fmt.Errorf("%s: internal error: bad 'for' index (integer expected, got %s)",
						sourceLocation(f.proto, frame.pc-1), l.typeName(*idx))
				}
				limitInteger, ok := (*limit).(integerValue)
				if !ok {
					return fmt.Errorf("%s: internal error: bad 'for' counter (integer expected, got %s)",
						sourceLocation(f.proto, frame.pc-1), l.typeName(*limit))
				}

				if count := uint64(limitInteger); count > 0 {
					*limit = integerValue(count - 1)
					nextIndex := indexInteger + step
					*idx = nextIndex
					*control = nextIndex
					frame.pc -= int(i.ArgBx())
				}
			case floatValue:
				indexFloat, ok := (*idx).(floatValue)
				if !ok {
					return fmt.Errorf("%s: internal error: bad 'for' index (number expected, got %s)",
						sourceLocation(f.proto, frame.pc-1), l.typeName(*idx))
				}
				limitFloat, ok := (*limit).(floatValue)
				if !ok {
					return fmt.Errorf("%s: internal error: bad 'for' counter (number expected, got %s)",
						sourceLocation(f.proto, frame.pc-1), l.typeName(*limit))
				}

				nextIndex := indexFloat + step
				if continueForLoop(nextIndex, limitFloat, step) {
					*idx = nextIndex
					*control = nextIndex
					frame.pc -= int(i.ArgBx())
				}
			default:
				return fmt.Errorf("%s: internal error: bad 'for' step (number expected, got %s)",
					sourceLocation(f.proto, frame.pc-1), l.typeName(step))
			}
		case luacode.OpForPrep:
			idx, limit, step, control, err := numericForLoopRegisters(registers(), i.ArgA())
			if err != nil {
				return err
			}
			if err := l.forPrep(idx, limit, step, control); err == errSkipLoop {
				frame.pc += int(i.ArgBx() + 1)
			} else if err != nil {
				return err
			}
		case luacode.OpTForPrep:
			a := i.ArgA()
			if _, err := genericForLoopRegisters(registers(), a); err != nil {
				return err
			}

			// Validate jump destination.
			callPC := frame.pc + int(i.ArgBx())
			if callPC < 0 || callPC >= len(f.proto.Code) {
				return fmt.Errorf("%s: decode instruction: %v instruction jumps out-of-bounds",
					sourceLocation(f.proto, frame.pc-1), opCode)
			}
			callInstruction := f.proto.Code[callPC]
			callOpCode := callInstruction.OpCode()
			if want := luacode.OpTForCall; callOpCode != want {
				return fmt.Errorf("%s: decode instruction: %v instruction jumps to %v (must be %v)",
					sourceLocation(f.proto, frame.pc-1), opCode, callOpCode, want)
			}
			if got := callInstruction.ArgA(); got != a {
				return fmt.Errorf("%s: decode instruction: %v instruction jumps to instruction with A=%d (must be %d)",
					sourceLocation(f.proto, frame.pc-1), opCode, got, a)
			}

			// Mark control variable as to-be-closed.
			if err := l.markTBC(frame.registerStart() + int(a) + 3); err != nil {
				return err
			}
			// Jump to the call instruction and fallthrough.
			i = callInstruction
			opCode = callOpCode
			frame.pc = callPC + 1
			fallthrough
		case luacode.OpTForCall:
			a := i.ArgA()
			if _, err := genericForLoopRegisters(registers(), a); err != nil {
				return err
			}
			c := int(i.ArgC())
			if c < 1 {
				return fmt.Errorf("%s: decode %v instruction: generic 'for' loop call must return at least 1 value",
					sourceLocation(f.proto, frame.pc-1), opCode)
			}

			stateStart := frame.registerStart() + int(a)
			stateEnd := stateStart + genericForLoopStateSize
			const numArgs = 2
			newTop := stateEnd + 1 + numArgs
			if !l.grow(newTop) {
				return errStackOverflow
			}
			l.setTop(newTop)
			copy(l.stack[stateEnd:], l.stack[stateStart:stateStart+1+numArgs])
			isLua, err := l.prepareCall(numArgs, c)
			if err != nil {
				return err
			}
			if isLua {
				if err := l.exec(); err != nil {
					return err
				}
			}
		case luacode.OpTForLoop:
			r := registers()
			a := i.ArgA()
			state, err := genericForLoopRegisters(r, a)
			if err != nil {
				return err
			}

			// An [luacode.OpTForCall] instructions will place the results
			// on the stack after the for loop state.
			newControlIndex := frame.registerStart() + int(a) + genericForLoopStateSize
			if newControlIndex >= cap(l.stack) {
				return fmt.Errorf("%s: decode %v instruction: 'for' loop call results out-of-bounds",
					sourceLocation(f.proto, frame.pc-1), opCode)
			}
			newControl := l.stack[:newControlIndex+1][newControlIndex]
			if newControl != nil {
				state[2] = newControl
				frame.pc -= int(i.ArgBx())
			}
		case luacode.OpSetList:
			a := i.ArgA()
			ra, err := register(registers(), a)
			if err != nil {
				return err
			}
			t, isTable := (*ra).(*table)
			if !isTable {
				return fmt.Errorf("%s: set list: value in register %d is a %s (need table)", sourceLocation(f.proto, frame.pc-1), i.ArgA(), l.typeName(*ra))
			}
			n := int(i.ArgB())
			stackBase := frame.registerStart() + int(a) + 1
			if n == 0 {
				n = len(l.stack) - stackBase
			} else if int(a)+1+n > int(f.proto.MaxStackSize) {
				return fmt.Errorf("%s: decode instruction: set list (a=%d n=%d) overflows stack (size=%d)",
					sourceLocation(f.proto, frame.pc-1), a, n, f.proto.MaxStackSize)
			}
			indexBase := integerValue(i.ArgC()) + 1

			for idx := range n {
				// TODO(soon): We can do a much more efficient bulk insert here.
				err := t.set(indexBase+integerValue(idx), l.stack[stackBase+idx])
				if err != nil {
					return err
				}
			}
		case luacode.OpClosure:
			ra, err := register(registers(), i.ArgA())
			if err != nil {
				return err
			}
			bx := i.ArgBx()
			if int(bx) >= len(f.proto.Functions) {
				return fmt.Errorf("%s: decode instruction: closure %d out of range", sourceLocation(f.proto, frame.pc-1), bx)
			}
			p := f.proto.Functions[i.ArgBx()]

			upvalues := make([]*upvalue, len(p.Upvalues))
			for i, uv := range p.Upvalues {
				if uv.InStack {
					upvalues[i] = l.stackUpvalue(frame.registerStart() + int(uv.Index))
				} else {
					upvalues[i] = f.upvalues[uv.Index]
				}
			}
			*ra = luaFunction{
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
				return errStackOverflow
			}
			l.setTop(a + numWanted)
			varargStart, varargEnd := frame.extraArgumentsRange()
			n := copy(l.stack[a:], l.stack[varargStart:varargEnd])
			clear(l.stack[a+n:])
		case luacode.OpVarargPrep:
			if frame.pc != 1 {
				return fmt.Errorf("%v must be first instruction in function", luacode.OpVarargPrep)
			}
			if frame.numExtraArguments != 0 {
				return fmt.Errorf("cannot run %v multiple times", luacode.OpVarargPrep)
			}
			// TODO(soon): Run this upon entering the function.
			numArguments := len(l.stack) - frame.registerStart()
			numFixedParameters := int(i.ArgA())
			numExtraArguments := numArguments - numFixedParameters
			if numExtraArguments > 0 {
				rotate(l.stack[frame.functionIndex:], numExtraArguments)
				frame.functionIndex += numExtraArguments
				frame.numExtraArguments = numExtraArguments
			}
		default:
			return fmt.Errorf("%s: decode instruction: unhandled instruction %v",
				sourceLocation(f.proto, frame.pc-1), opCode)
		}
	}
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

func (l *State) arithmeticMetamethod(event luacode.TagMethod, arg1, arg2 value) (value, error) {
	op, isArithmetic := event.ArithmeticOperator()
	if !isArithmetic {
		return nil, fmt.Errorf("%v is not an arithmetic metamethod", event)
	}

	if f := l.binaryMetamethod(arg1, arg2, event); f != nil {
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

func decodeBinaryMetamethod(frame *callFrame, proto *luacode.Prototype) (uint8, luacode.ArithmeticOperator, error) {
	pc := frame.pc - 1
	i := proto.Code[pc]
	if pc == 0 {
		return 0, 0, fmt.Errorf("%s: decode instruction: %v must be preceded by binary arithmetic instruction",
			sourceLocation(proto, pc), i.OpCode())
	}
	prev := proto.Code[pc-1]
	prevOpCode := prev.OpCode()
	prevOperator, isArithmetic := prevOpCode.ArithmeticOperator()
	if !isArithmetic || !prevOperator.IsBinary() {
		return 0, 0, fmt.Errorf("%s: decode instruction: %v must be preceded by binary arithmetic instruction (found %v)",
			sourceLocation(proto, pc), i.OpCode(), prevOpCode)
	}
	if got, want := luacode.TagMethod(i.ArgC()), prevOperator.TagMethod(); got != want {
		err := fmt.Errorf("%s: decode instruction: found metamethod %v in %v after %v (expected %v)",
			sourceLocation(proto, pc), got, i.OpCode(), prev.OpCode(), want)
		return prev.ArgA(), prevOperator, err
	}
	return prev.ArgA(), prevOperator, nil
}

// forPrep initializes the numeric for loop state
// during an [luacode.OpForPrep] instruction.
// forPrep returns [errSkipLoop] if initialization succeeded
// but the loop should not be entered.
func (l *State) forPrep(idx, limit, step, control *value) error {
	initInteger, isInitInteger := (*idx).(integerValue)
	stepInteger, isStepInteger := (*step).(integerValue)
	if isInitInteger && isStepInteger {
		limitInteger, err := l.forLoopLimitToInteger(initInteger, *limit, stepInteger)
		if err != nil {
			return err
		}
		var count uint64
		if stepInteger > 0 {
			count = uint64(limitInteger) - uint64(initInteger)
			if stepInteger != 1 { // Avoid division in the default case.
				count /= uint64(stepInteger)
			}
		} else {
			// stepInteger+1 avoids negating [math.MinInt64].
			positiveStep := uint64(-(stepInteger + 1)) + 1
			count = (uint64(initInteger) - uint64(limitInteger)) / positiveStep
		}
		*limit = integerValue(count)
		*control = initInteger
		return nil
	}

	limitNumber, ok := toNumber(*limit)
	if !ok {
		return fmt.Errorf("bad 'for' limit (number expected, got %s)", l.typeName(*limit))
	}
	stepNumber, ok := toNumber(*step)
	if !ok {
		return fmt.Errorf("bad 'for' step (number expected, got %s)", l.typeName(*step))
	}
	initNumber, ok := toNumber(*idx)
	if !ok {
		return fmt.Errorf("bad 'for' initial value (number expected, got %s)", l.typeName(*idx))
	}
	if stepNumber == 0 {
		return errZeroStep
	}
	if !continueForLoop(initNumber, limitNumber, stepNumber) {
		return errSkipLoop
	}
	// Coerce all registers to floatValue.
	*idx = initNumber
	*limit = limitNumber
	*step = stepNumber
	*control = initNumber
	return nil
}

// forLoopLimitToInteger converts the given “for” loop limit value to an integer.
// forLoopLimitToInteger will return [errSkipLoop] if the loop should not be entered.
func (l *State) forLoopLimitToInteger(init integerValue, limit value, step integerValue) (limitInteger integerValue, err error) {
	if step == 0 {
		return 0, errZeroStep
	}
	switch limit := limit.(type) {
	case integerValue:
		limitInteger = limit
	case stringValue:
		var ok bool
		limitInteger, ok = limit.toInteger()
		if !ok {
			limitFloat, ok := limit.toNumber()
			if !ok {
				return 0, fmt.Errorf("bad 'for' limit (number expected, got %s)", l.typeName(limit))
			}
			limitInteger, ok = floatToIntegerForLoopLimit(limitFloat, step)
			if !ok {
				return 0, errSkipLoop
			}
		}
	case floatValue:
		var ok bool
		limitInteger, ok = floatToIntegerForLoopLimit(limit, step)
		if !ok {
			return 0, errSkipLoop
		}
	default:
		return 0, fmt.Errorf("bad 'for' limit (number expected, got %s)", l.typeName(limit))
	}

	if !continueForLoop(init, limitInteger, step) {
		return limitInteger, errSkipLoop
	}
	return limitInteger, nil
}

var (
	errSkipLoop = errors.New("'for' limit prevents loop execution")
	errZeroStep = errors.New("'for' step is zero")
)

// floatToIntegerForLoopLimit converts a floating-point number to an integer
// for use as a “for” loop limit.
// If the floating-point value is out of range of an integer,
// then the limit is clipped to an integer.
// ok is true if and only if there exists any initial value that could satisfy the limit.
func floatToIntegerForLoopLimit(limit floatValue, step integerValue) (limitInteger integerValue, ok bool) {
	if math.IsNaN(float64(limit)) || step == 0 {
		return 0, false
	}
	mode := luacode.Floor
	if step < 0 {
		mode = luacode.Ceil
	}
	i, ok := luacode.FloatToInteger(float64(limit), mode)
	switch {
	case !ok && limit > 0:
		return math.MaxInt64, step > 0
	case !ok && limit < 0:
		return math.MinInt64, step < 0
	default:
		return integerValue(i), true
	}
}

// continueForLoop reports whether a for loop with the given limit and step
// should start another iteration with the given index.
func continueForLoop[T integerValue | floatValue](idx, limit, step T) bool {
	if step > 0 {
		return idx <= limit
	} else {
		return limit <= idx
	}
}

func decodeExtraArg(frame *callFrame, proto *luacode.Prototype) (uint32, error) {
	pc := frame.pc - 1
	argPC := pc + 1
	if argPC >= len(proto.Code) {
		return 0, fmt.Errorf("%s: decode instruction: %v expects extra argument",
			sourceLocation(proto, pc), proto.Code[pc].OpCode())
	}
	i := proto.Code[argPC]
	if got := i.OpCode(); got != luacode.OpExtraArg {
		return 0, fmt.Errorf("%s: decode instruction: %v expects extra argument (found %v)",
			sourceLocation(proto, pc), proto.Code[pc].OpCode(), got)
	}
	return i.ArgAx(), nil
}
