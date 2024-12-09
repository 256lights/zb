// Copyright (C) 1994-2024 Lua.org, PUC-Rio.
// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import (
	"errors"
	"fmt"
	"math"
)

// code appends the given instruction to fs.Code and returns its address.
//
// Equivalent to `luaK_code` in upstream Lua.
func (p *parser) code(fs *funcState, i Instruction) int {
	fs.Code = append(fs.Code, i)
	fs.saveLineInfo(p.lastLine)
	return len(fs.Code) - 1
}

// codeNil appends an [OpLoadNil] instruction to fs.Code.
//
// Equivalent to `luaK_nil` in upstream Lua.
func (p *parser) codeNil(fs *funcState, from registerIndex, n uint8) {
	if previous := fs.previousInstruction(); previous != nil && previous.OpCode() == OpLoadNil {
		// Peephole optimization:
		// if the previous instruction is also OpLoadNil and ranges are compatible,
		// adjust range of previous instruction instead of emitting a new one.
		// (For instance, 'local a; local b' will generate a single opcode.)
		last := from + registerIndex(n) - 1
		prevFrom := registerIndex(previous.ArgA())
		prevLast := prevFrom + registerIndex(previous.ArgB())
		if prevFrom <= from && from <= prevLast+1 || from <= prevFrom && prevFrom <= last+1 {
			newFrom := min(from, prevFrom)
			*previous = ABCInstruction(
				OpLoadNil,
				uint8(newFrom),
				uint8(max(last, prevLast)-newFrom),
				previous.ArgC(),
				previous.K(),
			)
			return
		}
	}

	// No optimization.
	p.code(fs, ABCInstruction(OpLoadNil, uint8(from), n-1, 0, false))
}

// codeJump appends a jump instruction to fs.Code and returns its index.
// The destination can be fixed later with [funcState.fixJump].
//
// Equivalent to `luaK_jump` in upstream Lua.
func (p *parser) codeJump(fs *funcState) int {
	return p.code(fs, JInstruction(OpJMP, noJump))
}

// codeReturn appends a return instruction to fs.Code.
// codeReturn panics if nret is out of range.
//
// Equivalent to `luaK_ret` in upstream Lua.
func (p *parser) codeReturn(fs *funcState, first registerIndex, nret int) {
	b := nret + 1
	if !(0 <= b && b <= maxArgB) {
		panic("number of returns out of range")
	}
	op := OpReturn
	switch nret {
	case 0:
		op = OpReturn0
	case 1:
		op = OpReturn1
	}
	p.code(fs, ABCInstruction(op, uint8(first), uint8(b), 0, false))
}

// codeInt appends a "load constant" instruction to fs.Code
// that loads the given integer.
//
// Equivalent to `luaK_int` in upstream Lua.
func (p *parser) codeInt(fs *funcState, reg registerIndex, i int64) {
	if !fitsSignedBx(i) {
		k := fs.addConstant(IntegerValue(i))
		p.codeConstant(fs, reg, k)
		return
	}

	p.code(fs, ABxInstruction(OpLoadI, uint8(reg), int32(i)))
}

// codeFloat appends a "load constant" instruction to fs.Code
// that loads the given floating-point number.
//
// Equivalent to `luaK_float` in upstream Lua.
func (p *parser) codeFloat(fs *funcState, reg registerIndex, f float64) {
	if i := int64(f); float64(i) == f && fitsSignedBx(i) {
		p.code(fs, ABxInstruction(OpLoadF, uint8(reg), int32(i)))
		return
	}

	k := fs.addConstant(FloatValue(f))
	p.codeConstant(fs, reg, k)
}

// codeConstant appends a "load constant" instruction to fs.Code.
// The instruction will load the k'th constant from the [Prototype] Constants table.
//
// Equivalent to `luaK_codek` in upstream Lua.
func (p *parser) codeConstant(fs *funcState, reg registerIndex, k int) int {
	if k > maxArgBx {
		pc := p.code(fs, ABxInstruction(OpLoadKX, uint8(reg), 0))
		p.code(fs, ExtraArgument(uint32(k)))
		return pc
	}
	return p.code(fs, ABxInstruction(OpLoadK, uint8(reg), int32(k)))
}

// codeStoreVariable appends instructions to store the result of expr into variable v.
// expr is no longer valid after a call to codeStoreVariable.
//
// Equivalent to `luaK_storevar` in upstream Lua.
func (p *parser) codeStoreVariable(fs *funcState, v, expr expressionDescriptor) error {
	switch v.kind {
	case expressionKindLocal:
		p.freeExpression(fs, expr)
		_, err := p.toRegister(fs, expr, v.register())
		return err
	case expressionKindUpvalue:
		var e registerIndex
		var err error
		expr, e, err = p.toAnyRegister(fs, expr)
		if err != nil {
			return err
		}
		p.code(fs, ABCInstruction(OpSetUpval, uint8(e), uint8(v.upvalueIndex()), 0, false))
	case expressionKindIndexUpvalue:
		var err error
		expr, err = p.codeABRK(fs, OpSetTabUp, uint8(v.tableUpvalue()), uint8(v.constantIndex()), expr)
		if err != nil {
			return err
		}
	case expressionKindIndexInt:
		var err error
		expr, err = p.codeABRK(fs, OpSetI, uint8(v.tableRegister()), uint8(v.indexInt()), expr)
		if err != nil {
			return err
		}
	case expressionKindIndexString:
		var err error
		expr, err = p.codeABRK(fs, OpSetField, uint8(v.tableRegister()), uint8(v.constantIndex()), expr)
		if err != nil {
			return err
		}
	case expressionKindIndexed:
		var err error
		expr, err = p.codeABRK(fs, OpSetTable, uint8(v.tableRegister()), uint8(v.indexRegister()), expr)
		if err != nil {
			return err
		}
	default:
		p.freeExpression(fs, expr)
		return fmt.Errorf("invalid variable kind to store (%v)", v.kind)
	}

	p.freeExpression(fs, expr)
	return nil
}

// codeSelf appends an [OpSelf] instruction to fs.Code.
// This has the effect of converting expression e into "e:key(e,".
// Both e and key are invalid after a call to codeSelf.
// codeSelf returns a [expressionKindNonRelocatable] of the register containing the function.
// e will be placed at the next register.
//
// Equivalent to `luaK_self` in upstream Lua.
func (p *parser) codeSelf(fs *funcState, e, key expressionDescriptor) (expressionDescriptor, error) {
	e, ereg, err := p.toAnyRegister(fs, e)
	if err != nil {
		return voidExpression(), err
	}
	p.freeExpression(fs, e)

	// Reserve registers for function and self produced by OpSelf.
	baseRegister := fs.firstFreeRegister
	if err := fs.reserveRegisters(2); err != nil {
		return voidExpression(), err
	}

	key, err = p.codeABRK(fs, OpSelf, uint8(baseRegister), uint8(ereg), key)
	if err != nil {
		return voidExpression(), err
	}
	p.freeExpression(fs, key)

	return nonRelocatableExpression(baseRegister), nil
}

// codeGoIfTrue appends instructions to go through if e is true, jump otherwise.
//
// Equivalent to `luaK_goiftrue` in upstream Lua.
func (p *parser) codeGoIfTrue(fs *funcState, e expressionDescriptor) (expressionDescriptor, error) {
	e = p.dischargeVars(fs, e)
	var pc int
	switch e.kind {
	case expressionKindJump:
		pc = e.pc()
		if err := fs.negateCondition(pc); err != nil {
			return e, err
		}
	case expressionKindConstant, expressionKindFloatConstant, expressionKindIntConstant, expressionKindStringConstant, expressionKindTrue:
		// Always true; do nothing.
		pc = noJump
	default:
		var err error
		pc, err = p.jumpOnCond(fs, e, false)
		if err != nil {
			return e, err
		}
	}
	// Insert new jump in false list.
	var err error
	e.f, err = fs.concatJumpList(e.f, pc)
	if err != nil {
		return e, err
	}
	// True list jumps to here (to go through).
	if err := fs.patchToHere(e.t); err != nil {
		return e, err
	}
	e.t = noJump
	return e, nil
}

// codeGoIfFalse appends instructions to go through if e is false, jump otherwise.
//
// Equivalent to `luaK_goiffalse` in upstream Lua.
func (p *parser) codeGoIfFalse(fs *funcState, e expressionDescriptor) (expressionDescriptor, error) {
	e = p.dischargeVars(fs, e)
	var pc int
	switch e.kind {
	case expressionKindJump:
		pc = e.pc()
	case expressionKindNil, expressionKindFalse:
		// Always false; do nothing.
		pc = noJump
	default:
		var err error
		pc, err = p.jumpOnCond(fs, e, true)
		if err != nil {
			return e, err
		}
	}
	// Insert new jump in true list.
	var err error
	e.t, err = fs.concatJumpList(e.t, pc)
	if err != nil {
		return e, err
	}
	// False list jumps to here (to go through).
	if err := fs.patchToHere(e.f); err != nil {
		return e, err
	}
	e.f = noJump
	return e, nil
}

// jumpOnCond appends an instruction to jump if e is cond
// (that is, if cond is true, code will jump if e is true)
// and returns the jump position.
//
// Equivalent to `jumponcond` in upstream Lua.
func (p *parser) jumpOnCond(fs *funcState, e expressionDescriptor, cond bool) (int, error) {
	if e.kind == expressionKindRelocatable {
		if ie := fs.Code[e.pc()]; ie.OpCode() == OpNot {
			// Remove previous OpNot.
			fs.removeLastInstruction()
			p.code(fs, ABCInstruction(OpTest, ie.ArgB(), 0, 0, !cond))
			return p.codeJump(fs), nil
		}
	}

	e, err := p.dischargeToAnyRegister(fs, e)
	if err != nil {
		return 0, err
	}
	p.freeExpression(fs, e)
	p.code(fs, ABCInstruction(OpTestSet, uint8(noRegister), uint8(e.register()), 0, cond))
	return p.codeJump(fs), nil
}

// codeNot codes "not e", doing constant folding.
//
// Equivalent to `codenot` in upstream Lua.
func (p *parser) codeNot(fs *funcState, e expressionDescriptor) (expressionDescriptor, error) {
	switch e.kind {
	case expressionKindNil, expressionKindFalse:
		e.kind = expressionKindTrue
	case expressionKindConstant, expressionKindFloatConstant, expressionKindIntConstant, expressionKindStringConstant, expressionKindTrue:
		e.kind = expressionKindFalse
	case expressionKindJump:
		if err := fs.negateCondition(e.pc()); err != nil {
			return e, err
		}
	case expressionKindRelocatable, expressionKindNonRelocatable:
		var err error
		e, err = p.dischargeToAnyRegister(fs, e)
		if err != nil {
			return e, err
		}
		pc := p.code(fs, ABCInstruction(OpNot, 0, uint8(e.register()), 0, false))
		e = relocatableExpression(pc).withJumpLists(e)
	default:
		return e, fmt.Errorf("internal error: codeNot: unhandled expression (%v)", e.kind)
	}

	e.t, e.f = e.f, e.t
	// Values are useless when negated.
	// Traverse the list of tests to ensure none of them produce a value.
	for _, list := range [...]int{e.f, e.t} {
		for ; list != noJump; list, _ = fs.jumpDestination(list) {
			fs.patchTestRegister(list, noRegister)
		}
	}

	return e, nil
}

// codeIndexed appends instructions to fs.Code for the expression "t[k]".
// If t is not in a register or upvalue, codeIndexed returns an error.
//
// Equivalent to `luaK_indexed` in upstream Lua.
func (p *parser) codeIndexed(fs *funcState, t, k expressionDescriptor) (expressionDescriptor, error) {
	if t.hasJumps() {
		return voidExpression(), errors.New("internal error: codeIndexed: table expression has jumps")
	}

	if k.kind == expressionKindStringConstant {
		k = p.stringToConstantTable(fs, k)
	}
	isKstr := k.kind == expressionKindConstant &&
		!k.hasJumps() &&
		k.constantIndex() <= maxArgB &&
		fs.Constants[k.constantIndex()].isShortString()
	if t.kind == expressionKindUpvalue && !isKstr {
		// [OpGetTabUp] can only index short strings.
		// Copy the table from an upvalue to a register.
		var err error
		t, _, err = p.toAnyRegister(fs, t)
		if err != nil {
			return voidExpression(), err
		}
	}

	switch t.kind {
	case expressionKindUpvalue:
		return indexedUpvalueExpression(t.upvalueIndex(), uint16(k.constantIndex())), nil
	case expressionKindLocal, expressionKindNonRelocatable:
		if isKstr {
			return indexStringExpression(t.register(), uint16(k.constantIndex())), nil
		} else if i, isInt := k.intConstant(); isInt && !k.hasJumps() && 0 <= i && i <= maxArgC {
			return indexIntExpression(t.register(), uint16(i)), nil
		} else {
			_, reg, err := p.toAnyRegister(fs, k)
			if err != nil {
				return voidExpression(), err
			}
			return indexedExpression(t.register(), reg), nil
		}
	default:
		return voidExpression(), fmt.Errorf("internal error: codeIndexed: unhandled table kind %v", t.kind)
	}
}

// codePrefix appends the code to apply a prefix operator to an expression
// to fs.Code.
//
// Equivalent to `luaK_prefix` in upstream Lua.
func (p *parser) codePrefix(fs *funcState, operator unaryOperator, e expressionDescriptor, line int) (expressionDescriptor, error) {
	e = p.dischargeVars(fs, e)
	switch operator {
	case unaryOperatorMinus, unaryOperatorBNot:
		fakeRHS := intConstantExpression(0)
		aop, _ := operator.toArithmetic()
		if e, folded := p.foldConstants(aop, e, fakeRHS); folded {
			return e, nil
		}
		fallthrough
	case unaryOperatorLen:
		op, _ := operator.toOpCode()
		return p.codeUnaryExpValue(fs, op, e, line)
	case unaryOperatorNot:
		return p.codeNot(fs, e)
	default:
		return voidExpression(), fmt.Errorf("internal error: codePrefix: unhandled operator %v", operator)
	}
}

// codeUnaryExpValue appends the code for any unary expresion except "not"
// to fs.Code.
//
// Equivalent to `codeunexpval` in upstream Lua.
func (p *parser) codeUnaryExpValue(fs *funcState, op OpCode, e expressionDescriptor, line int) (expressionDescriptor, error) {
	e, r, err := p.toAnyRegister(fs, e)
	if err != nil {
		return e, err
	}
	p.freeExpression(fs, e)
	pc := p.code(fs, ABCInstruction(op, 0, uint8(r), 0, false))
	fs.fixLineInfo(line)
	return relocatableExpression(pc).withJumpLists(e), nil
}

// codeInfix processes the first operand of a binary expression
// before reading the second operand.
// The caller should call [*parser.codePostfix] after reading the second operand.
//
// Equivalent to `luaK_infix` in upstream Lua.
func (p *parser) codeInfix(fs *funcState, operator binaryOperator, v expressionDescriptor) (expressionDescriptor, error) {
	v = p.dischargeVars(fs, v)
	switch operator {
	case binaryOperatorAnd:
		return p.codeGoIfTrue(fs, v)
	case binaryOperatorOr:
		return p.codeGoIfFalse(fs, v)
	case binaryOperatorConcat:
		var err error
		v, _, err = p.toNextRegister(fs, v)
		return v, err
	case binaryOperatorAdd, binaryOperatorSub,
		binaryOperatorMul, binaryOperatorDiv, binaryOperatorIDiv, binaryOperatorMod,
		binaryOperatorPow,
		binaryOperatorBAnd, binaryOperatorBOr, binaryOperatorBXor,
		binaryOperatorShiftL, binaryOperatorShiftR:
		if v.isNumeral() {
			// Preserve numerals because they may be folded or used as an immediate operand.
			return v, nil
		}
		var err error
		v, _, err = p.toAnyRegister(fs, v)
		return v, err
	case binaryOperatorEq, binaryOperatorNE:
		if v.isNumeral() {
			// Preserve numerals because they may be used as an immediate operand.
			return v, nil
		}
		var err error
		v, _, _, err = p.toRK(fs, v)
		return v, err
	case binaryOperatorLT, binaryOperatorLE, binaryOperatorGT, binaryOperatorGE:
		if _, _, isSigned := v.toSignedArg(); isSigned {
			// Preserve numerals because they may be used as an immediate operand.
			return v, nil
		}
		var err error
		v, _, err = p.toAnyRegister(fs, v)
		return v, err
	default:
		return v, fmt.Errorf("internal error: codeInfix: unhandled operator %v", operator)
	}
}

// codePostfix finalizes the code for a binary operation
// after reading the second operand.
// This must have been preceded by a call to [*parser.codeInfix].
//
// Equivalent to `luaK_posfix` in upstream Lua.
func (p *parser) codePostfix(fs *funcState, operator binaryOperator, e1, e2 expressionDescriptor, line int) (expressionDescriptor, error) {
	e2 = p.dischargeVars(fs, e2)
	if operator, ok := operator.toArithmetic(); ok {
		if result, folded := p.foldConstants(operator, e1, e2); folded {
			return result, nil
		}
	}

	switch operator {
	case binaryOperatorAnd:
		if e1.t != noJump {
			return voidExpression(), errors.New("internal error: codePostfix: list should have been closed by codeInfix")
		}
		var err error
		e2.f, err = fs.concatJumpList(e2.f, e1.f)
		if err != nil {
			return voidExpression(), err
		}
		return e2, nil
	case binaryOperatorOr:
		if e1.t != noJump {
			return voidExpression(), errors.New("internal error: codePostfix: list should have been closed by codeInfix")
		}
		var err error
		e2.t, err = fs.concatJumpList(e2.t, e1.t)
		if err != nil {
			return voidExpression(), err
		}
		return e2, nil
	case binaryOperatorConcat:
		var err error
		e2, _, err = p.toNextRegister(fs, e2)
		if err != nil {
			return voidExpression(), err
		}
		p.codeConcat(fs, e1, e2, line)
		return e1, nil
	case binaryOperatorAdd, binaryOperatorMul:
		return p.codeCommutative(fs, operator, e1, e2, line)
	case binaryOperatorSub:
		result, err := p.finishBinaryExpNegated(fs, e1, e2, OpAddI, line, TagMethodSub)
		if err != nil {
			return voidExpression(), err
		}
		if result.kind != expressionKindVoid {
			return result, nil
		}
		fallthrough
	case binaryOperatorDiv, binaryOperatorIDiv, binaryOperatorMod, binaryOperatorPow:
		return p.codeArithmetic(fs, operator, e1, e2, false, line)
	case binaryOperatorBAnd, binaryOperatorBOr, binaryOperatorBXor:
		return p.codeBitwise(fs, operator, e1, e2, line)
	case binaryOperatorShiftL:
		if i1, ok := e1.intConstant(); ok && fitsSignedArg(i1) {
			// I << r2
			return p.codeBinaryExpImmediate(fs, OpSHLI, e2, e1, true, line, TagMethodSHL)
		}
		if result, err := p.finishBinaryExpNegated(fs, e1, e2, OpSHRI, line, TagMethodSHL); err != nil {
			return voidExpression(), err
		} else if result.kind != expressionKindVoid {
			return result, nil
		}
		return p.codeBinaryExp(fs, operator, e1, e2, line)
	case binaryOperatorShiftR:
		if i2, ok := e2.intConstant(); ok && fitsSignedArg(i2) {
			// r1 >> I
			return p.codeBinaryExpImmediate(fs, OpSHRI, e1, e2, false, line, TagMethodSHR)
		}
		return p.codeBinaryExp(fs, operator, e1, e2, line)
	case binaryOperatorEq, binaryOperatorNE:
		return p.codeEq(fs, operator, e1, e2)
	case binaryOperatorGT:
		// Convert "a > b" into "b < a".
		return p.codeOrder(fs, binaryOperatorLT, e2, e1)
	case binaryOperatorGE:
		// Convert "a >= b" into "b <= a".
		return p.codeOrder(fs, binaryOperatorLE, e2, e1)
	case binaryOperatorLT, binaryOperatorLE:
		return p.codeOrder(fs, operator, e1, e2)
	default:
		return voidExpression(), fmt.Errorf("internal error: codePostfix: unhandled operator %v", operator)
	}
}

// codeCommutative appends instructions for non-bitwise commutative operators (i.e. "+" and "*")
// to fs.Code.
//
// Equivalent to `codecommutative` in upstream Lua.
func (p *parser) codeCommutative(fs *funcState, operator binaryOperator, e1, e2 expressionDescriptor, line int) (expressionDescriptor, error) {
	// If first operand is a numeric constant,
	// change order of operands to try to use an immediate or K operator.
	flip := e1.isNumeral()
	if flip {
		e1, e2 = e2, e1
		flip = true
	}
	if i, isInt := e2.intConstant(); isInt && fitsSignedArg(i) && operator == binaryOperatorAdd {
		return p.codeBinaryExpImmediate(fs, OpAddI, e1, e2, flip, line, TagMethodAdd)
	}
	return p.codeArithmetic(fs, operator, e1, e2, flip, line)
}

// codeBitwise appends instructions for bitwise operators
//
// Equivalent to `codebitwise` in upstream Lua.
func (p *parser) codeBitwise(fs *funcState, operator binaryOperator, e1, e2 expressionDescriptor, line int) (expressionDescriptor, error) {
	// All operations are commutative,
	// so if first operand is a numeric constant,
	// change order of operands to try to use an immediate or K operator.
	flip := e1.kind == expressionKindIntConstant
	if flip {
		e1, e2 = e2, e1
	}
	if e2.kind == expressionKindIntConstant {
		if e2, _, ok := p.toConstantTable(fs, e2); ok {
			return p.codeBinaryExpConstant(fs, operator, e1, e2, flip, line)
		}
	}
	return p.codeBinaryExpNoConstants(fs, operator, e1, e2, flip, line)
}

// codeArithmetic appends instructions for an arithmetic binary operator
// to fs.Code.
//
// Equivalent to `codearith` in upstream Lua.
func (p *parser) codeArithmetic(fs *funcState, operator binaryOperator, e1, e2 expressionDescriptor, flip bool, line int) (expressionDescriptor, error) {
	if e2.isNumeral() {
		if e2, _, ok := p.toConstantTable(fs, e2); ok {
			return p.codeBinaryExpConstant(fs, operator, e1, e2, flip, line)
		}
	}
	return p.codeBinaryExpNoConstants(fs, operator, e1, e2, flip, line)
}

// codeBinaryExpNoConstants appends the instructions
// for a binary expression without constant operands
// to fs.Code.
//
// Equivalent to `codebinNoK` in upstream Lua.
func (p *parser) codeBinaryExpNoConstants(fs *funcState, operator binaryOperator, e1, e2 expressionDescriptor, flip bool, line int) (expressionDescriptor, error) {
	if flip {
		// Back to original order.
		e1, e2 = e2, e1
	}
	return p.codeBinaryExp(fs, operator, e1, e2, line)
}

// codeBinaryExp appends the instructions
// for a binary expression that "produces values" over two registers
// to fs.Code.
//
// Equivalent to `codebinexpval` in upstream Lua.
func (p *parser) codeBinaryExp(fs *funcState, operator binaryOperator, e1, e2 expressionDescriptor, line int) (expressionDescriptor, error) {
	op, ok := operator.toOpCode(OpAdd)
	if !ok {
		return voidExpression(), fmt.Errorf("internal error: codeBinaryExp: %v does not translate cleanly to OpCode", operator)
	}
	event, ok := operator.tagMethod()
	if !ok {
		return voidExpression(), fmt.Errorf("internal error: codeBinaryExp: %v does not have a TagMethod", operator)
	}
	if !e1.kind.isCompileTimeConstant() && e1.kind != expressionKindNonRelocatable && e1.kind != expressionKindRelocatable {
		return voidExpression(), fmt.Errorf("internal error: codeBinaryExp: left-side operand must be a constant or in a register")
	}

	e2, v2, err := p.toAnyRegister(fs, e2)
	if err != nil {
		return voidExpression(), err
	}
	return p.finishBinaryExpValue(fs, e1, e2, op, uint8(v2), false, line, OpMMBin, event)
}

// codeBinaryExpImmediate appends the instructions
// for a binary expression with immediate operands
// to fs.Code.
//
// Equivalent to `codebini` in upstream Lua.
func (p *parser) codeBinaryExpImmediate(fs *funcState, op OpCode, e1, e2 expressionDescriptor, flip bool, line int, event TagMethod) (expressionDescriptor, error) {
	i, ok := e2.intConstant()
	if !ok {
		return voidExpression(), fmt.Errorf("internal error: codeBinaryExpImmediate: right-side operand must be an immediate integer")
	}
	v2, ok := ToSignedArg(i)
	if !ok {
		return voidExpression(), fmt.Errorf("internal error: codeBinaryExpImmediate: right-side operand (%d) out of range", i)
	}
	return p.finishBinaryExpValue(fs, e1, e2, op, v2, flip, line, OpMMBinI, event)
}

// codeBinaryExpConstant appends the instructions
// for a binary expression with an operand
// that refers to a constant in the [Prototype] Constants table
// to fs.Code.
//
// Equivalent to `codebinK` in upstream Lua.
func (p *parser) codeBinaryExpConstant(fs *funcState, operator binaryOperator, e1, e2 expressionDescriptor, flip bool, line int) (expressionDescriptor, error) {
	event, ok := operator.tagMethod()
	if !ok {
		return voidExpression(), fmt.Errorf("internal error: codeBinaryExpConstant: operator %v does not have a metamethod", operator)
	}
	if e2.kind != expressionKindConstant {
		return voidExpression(), fmt.Errorf("internal error: codeBinaryExpConstant: right-side operand must be a reference to the Constants table")
	}
	v2 := e2.constantIndex()
	op, ok := operator.toOpCode(OpAddK)
	if !ok {
		return voidExpression(), fmt.Errorf("internal error: codeBinaryExpConstant: %v does not translate cleanly to OpCode", operator)
	}
	return p.finishBinaryExpValue(fs, e1, e2, op, uint8(v2), flip, line, OpMMBinK, event)
}

// finishBinaryExpValue appends the instructions
// for binary expressions that "produce values"
// (everything but logical operators 'and'/'or' and comparison operators).
//
// Equivalent to `finishbinexpval` in upstream Lua.
func (p *parser) finishBinaryExpValue(fs *funcState, e1, e2 expressionDescriptor, op OpCode, v2 uint8, flip bool, line int, mmop OpCode, event TagMethod) (expressionDescriptor, error) {
	e1, v1, err := p.toAnyRegister(fs, e1)
	if err != nil {
		return voidExpression(), err
	}
	pc := p.code(fs, ABCInstruction(op, 0, uint8(v1), v2, false))
	p.freeExpressions(fs, e1, e2)
	fs.fixLineInfo(line)
	p.code(fs, ABCInstruction(mmop, uint8(v1), v2, uint8(event), flip))
	fs.fixLineInfo(line)
	return relocatableExpression(pc).withJumpLists(e1), nil
}

// finishBinaryExpNegated attempts to append the instructions
// for a binary expression with the right-side operand negated
// to fs.Code.
// If the attempt fails, then finishBinaryExpNegated returns a [voidExpressionDescriptor] with a nil error.
//
// Equivalent to `finishbinexpneg` in upstream Lua.
func (p *parser) finishBinaryExpNegated(fs *funcState, e1, e2 expressionDescriptor, op OpCode, line int, event TagMethod) (expressionDescriptor, error) {
	i2, ok := e2.intConstant()
	if !ok || e2.hasJumps() {
		return voidExpression(), nil
	}
	v2, ok := ToSignedArg(i2)
	if !ok {
		return voidExpression(), nil
	}
	negV2, ok := ToSignedArg(-i2)
	if !ok {
		return voidExpression(), nil
	}
	const mmop = OpMMBinI
	result, err := p.finishBinaryExpValue(fs, e1, e2, op, negV2, false, line, mmop, event)
	if err != nil {
		return voidExpression(), err
	}
	// The metamethod must observe the original value.
	i := &fs.Code[len(fs.Code)-1]
	if i.OpCode() != mmop {
		panic("expected finishBinaryExpValue to end with metamethod instruction")
	}
	*i = ABCInstruction(mmop, i.ArgA(), v2, i.ArgC(), i.K())
	return result, nil
}

// codeConcat appends the instructions for "(e1 .. e2)" to fs.Code.
// e2 is not valid after codeConcat returns.
// If e1 does not reference a register, codeConcat will panic.
//
// Equivalent to `codeconcat` in upstream Lua.
func (p *parser) codeConcat(fs *funcState, e1, e2 expressionDescriptor, line int) {
	r1 := e1.register()

	// For "(e1 .. e2.1 .. e2.2)"
	// (which is "(e1 .. (e2.1 .. e2.2))" because concatenation is right associative),
	// merge both [OpConcat] instructions.
	ie2 := fs.previousInstruction()
	if ie2 != nil && ie2.OpCode() == OpConcat && r1+1 == registerIndex(ie2.ArgA()) {
		n := ie2.ArgB() // Number of elements concatenated in e2.
		p.freeExpression(fs, e2)
		*ie2 = ABCInstruction(OpConcat, uint8(r1), n+1, ie2.ArgC(), ie2.K())
		return
	}

	p.code(fs, ABCInstruction(OpConcat, uint8(r1), 2, 0, false))
	p.freeExpression(fs, e2)
	fs.fixLineInfo(line)
}

// codeOrder appends the instructions for an order comparison to fs.Code.
//
// Equivalent to `codeorder` in upstream Lua.
func (p *parser) codeOrder(fs *funcState, operator binaryOperator, e1, e2 expressionDescriptor) (expressionDescriptor, error) {
	var op OpCode
	var r1 registerIndex
	var b, c uint8
	if immediate, isFloat, ok := e2.toSignedArg(); ok {
		var err error
		e1, r1, err = p.toAnyRegister(fs, e1)
		if err != nil {
			return voidExpression(), err
		}
		b = immediate
		if isFloat {
			c = 1
		}
		op, _ = operator.toOpCode(OpLTI)
	} else if immediate, isFloat, ok = e1.toSignedArg(); ok {
		var err error
		e2, r1, err = p.toAnyRegister(fs, e2)
		if err != nil {
			return voidExpression(), err
		}
		b = immediate
		if isFloat {
			c = 1
		}
		switch operator {
		case binaryOperatorLT:
			op = OpGTI
		case binaryOperatorLE:
			op = OpGEI
		default:
			return voidExpression(), fmt.Errorf("internal error: codeOrder: unhandled operator %v", operator)
		}
	} else {
		var err error
		e1, r1, err = p.toAnyRegister(fs, e1)
		if err != nil {
			return voidExpression(), err
		}
		var r2 registerIndex
		e2, r2, err = p.toAnyRegister(fs, e2)
		if err != nil {
			return voidExpression(), err
		}
		b = uint8(r2)
		op, _ = operator.toOpCode(OpLT)
	}

	p.freeExpressions(fs, e1, e2)
	p.code(fs, ABCInstruction(op, uint8(r1), b, c, true))
	pc := p.codeJump(fs)
	return jumpExpression(pc), nil
}

// codeEq appends code for equality comparisons ("==" or "~=") to fs.Code.
// e1 must have already turned into an R/K by [*parser.codeInfix].
//
// Equivalent to `codeeq` in upstream Lua.
func (p *parser) codeEq(fs *funcState, operator binaryOperator, e1, e2 expressionDescriptor) (expressionDescriptor, error) {
	switch e1.kind {
	case expressionKindConstant, expressionKindIntConstant, expressionKindFloatConstant:
		// Swap constant/immediate to right side.
		e1, e2 = e2, e1
	case expressionKindNonRelocatable:
		// Fine as-is.
	default:
		return voidExpression(), fmt.Errorf("internal error: codeEq: left-side operand should have turned into a register or a constant (found %v)", e1.kind)
	}

	e1, r1, err := p.toAnyRegister(fs, e1)
	if err != nil {
		return voidExpression(), err
	}
	var op OpCode
	var b uint8
	var c uint8 // Not needed here, but kept for symmetry.
	if immediate, isFloat, isImmediate := e2.toSignedArg(); isImmediate {
		op = OpEQI
		b = immediate
		if isFloat {
			c = 1
		}
	} else {
		var k bool
		e2, b, k, err = p.toRK(fs, e2)
		if err != nil {
			return voidExpression(), err
		}
		if k {
			op = OpEQK
		} else {
			op = OpEQ
			// TODO(maybe): expToRK should have already converted to register.
			// Is this necessary?
			var r2 registerIndex
			e2, r2, err = p.toAnyRegister(fs, e2)
			if err != nil {
				return voidExpression(), err
			}
			b = uint8(r2)
		}
	}

	p.freeExpressions(fs, e1, e2)
	p.code(fs, ABCInstruction(op, uint8(r1), b, c, operator == binaryOperatorEq))
	pc := p.codeJump(fs)
	return jumpExpression(pc).withJumpLists(e1), nil
}

// fieldsPerFlush is the number of list items to accumulate
// before an [OpSetList] [Instruction].
const fieldsPerFlush = 50

// codeSetList appends the instructions for an [OpSetList] instruction to fs.Code.
// base is the register that keeps table;
// numElements is the length of the table (excluding those to be stored now);
// toStore is the number of values (in registers base + 1, ...)
// to add to the table (or [multiReturn] to add up to stack top).
//
// Equivalent to `luaK_setlist` in upstream Lua.
func (p *parser) codeSetList(fs *funcState, base registerIndex, numElements int, toStore int) error {
	switch {
	case toStore == MultiReturn:
		toStore = 0
	case toStore <= 0 || toStore > fieldsPerFlush:
		return fmt.Errorf("internal error: codeSetList: toStore out of range (%d)", toStore)
	}
	if numElements <= maxArgC {
		p.code(fs, ABCInstruction(OpSetList, uint8(base), uint8(toStore), uint8(numElements), false))
	} else {
		extra := numElements / (maxArgC + 1)
		numElements %= maxArgC + 1
		p.code(fs, ABCInstruction(OpSetList, uint8(base), uint8(toStore), uint8(numElements), true))
		p.code(fs, ExtraArgument(uint32(extra)))
	}
	// Free the registers used for list values.
	fs.firstFreeRegister = base + 1
	return nil
}

// foldConstants tries to statically evaluate an expression.
//
// Equivalent to `constfolding` in upstream Lua.
func (p *parser) foldConstants(op ArithmeticOperator, e1, e2 expressionDescriptor) (expressionDescriptor, bool) {
	v1, ok := e1.toNumeral()
	if !ok {
		return voidExpression(), false
	}
	v2, ok := e2.toNumeral()
	if !ok {
		return voidExpression(), false
	}

	result, err := Arithmetic(op, v1, v2)
	if err != nil {
		return voidExpression(), false
	}
	if result.IsInteger() {
		i, _ := result.Int64(OnlyIntegral)
		return intConstantExpression(i), true
	}
	n, ok := result.Float64()
	if !ok {
		// Shouldn't occur, but coding defensively.
		return voidExpression(), false
	}
	if math.IsNaN(n) || n == 0 {
		// Don't fold numbers that have tricky equality properties.
		return voidExpression(), false
	}
	return floatConstantExpression(n), true
}

// toValue ensures the final expression result
// is either in a register or it is a constant.
//
// Equivalent to `luaK_exp2val` in upstream Lua.
func (p *parser) toValue(fs *funcState, e expressionDescriptor) (expressionDescriptor, error) {
	if e.hasJumps() {
		e, _, err := p.toAnyRegister(fs, e)
		return e, err
	}
	return p.dischargeVars(fs, e), nil
}

// codeABRK converts the expression to a register or a constant
// (see [*parser.toRK])
// and uses it as the third (C) argument to an [ABCInstruction]
// that is appended to fs.Code.
//
// Equivalent to `codeABRK` in upstream Lua.
func (p *parser) codeABRK(fs *funcState, op OpCode, a, b uint8, e expressionDescriptor) (expressionDescriptor, error) {
	e, c, k, err := p.toRK(fs, e)
	if err != nil {
		return e, err
	}
	p.code(fs, ABCInstruction(op, a, b, c, k))
	return e, nil
}

// maxIndexRK is the maximum index that can be used
// as either a register index or a Constants table index.
const maxIndexRK = maxArgC

// toRK converts the expression to either [expressionKindConstant]
// with an index less than maxIndexRK
// or [expressionKindNonRelocatable].
// c is the register index or the constant index as an [Instruction] argument.
// k is true if the resulting expression is [expressionKindConstant].
//
// Equivalent to `exp2RK` in upstream Lua.
func (p *parser) toRK(fs *funcState, e expressionDescriptor) (_ expressionDescriptor, c uint8, k bool, err error) {
	if e, c, ok := p.toConstantTable(fs, e); ok {
		return e, c, true, nil
	}
	e, reg, err := p.toAnyRegister(fs, e)
	return e, uint8(reg), false, err
}

// toConstantTable attempts to make e an [expressionKindConstant]
// with an index in the range of R/K indices.
//
// Equivalent to `luaK_exp2K` in upstream Lua.
func (p *parser) toConstantTable(fs *funcState, e expressionDescriptor) (_ expressionDescriptor, idx uint8, ok bool) {
	if e.hasJumps() {
		return e, uint8(noRegister), false
	}
	v, ok := e.toValue()
	if !ok {
		return e, uint8(noRegister), false
	}
	// TODO(maybe): Can this waste a constant table entry?
	k := fs.addConstant(v)
	if k > maxIndexRK {
		return e, uint8(noRegister), false
	}
	return constantTableExpression(k), uint8(k), true
}

// toAnyRegisterOrUpvalue ensures the final expression result
// is either in a register or in an upvalue.
//
// Equivalent to `luaK_exp2anyregup` in upstream Lua.
func (p *parser) toAnyRegisterOrUpvalue(fs *funcState, e expressionDescriptor) (expressionDescriptor, error) {
	if e.kind == expressionKindUpvalue && !e.hasJumps() {
		return e, nil
	}
	e, _, err := p.toAnyRegister(fs, e)
	return e, err
}

// toAnyRegister ensures the final expression result is in some (any) register
// and returns that register.
//
// On success, the result of exp2nextreg will always be [expressionKindNonRelocatable].
//
// Equivalent to `luaK_exp2anyreg` in upstream Lua.
func (p *parser) toAnyRegister(fs *funcState, e expressionDescriptor) (expressionDescriptor, registerIndex, error) {
	e = p.dischargeVars(fs, e)
	if e.kind == expressionKindNonRelocatable {
		if !e.hasJumps() {
			// Result is already in a register.
			return e, e.register(), nil
		}
		if e.register() >= p.numVariablesInStack(fs) {
			// The register is not a local: put the final result in it.
			e, err := p.toRegister(fs, e, e.register())
			if err != nil {
				return e, noRegister, err
			}
			return e, e.register(), nil
		}
		// Otherwise expression has jumps and cannot change its register
		// to hold the jump values, because it is a local variable.
		// Go through to the default case.
	}
	// Default: use next available register.
	return p.toNextRegister(fs, e)
}

// toNextRegister ensures the final expression result is in the next available register.
//
// On success, the result of toNextRegister will always be [expressionKindNonRelocatable].
//
// Equivalent to `luaK_exp2nextreg` in upstream Lua.
func (p *parser) toNextRegister(fs *funcState, e expressionDescriptor) (expressionDescriptor, registerIndex, error) {
	e = p.dischargeVars(fs, e)
	p.freeExpression(fs, e)
	reg, err := fs.reserveRegister()
	if err != nil {
		return e, noRegister, err
	}
	e, err = p.toRegister(fs, e, reg)
	return e, reg, err
}

// toRegister ensures the final expression result
// (which includes results from its jump lists)
// is in the given register.
// If expression has jumps,
// need to patch these jumps either to its final position
// or to "load" instructions
// (for those tests that do not produce values).
//
// On success, the result of toRegister will always be [expressionKindNonRelocatable].
//
// Equivalent to `exp2reg` in upstream Lua.
func (p *parser) toRegister(fs *funcState, e expressionDescriptor, reg registerIndex) (expressionDescriptor, error) {
	e = p.dischargeToRegister(fs, e, reg)

	if e.kind == expressionKindJump {
		// Expression is a test, so put this jump in 't' list.
		var err error
		e.t, err = fs.concatJumpList(e.t, e.pc())
		if err != nil {
			return e, err
		}
	}

	if e.hasJumps() {
		needValue := func(list int) bool {
			for ; list != noJump; list, _ = fs.jumpDestination(list) {
				i := fs.findJumpControl(list)
				if i.OpCode() != OpTestSet {
					return true
				}
			}
			return false
		}

		positionLoadFalse := noJump
		positionLoadTrue := noJump
		if needValue(e.t) || needValue(e.f) {
			fj := noJump
			if e.kind != expressionKindJump {
				fj = p.codeJump(fs)
			}
			fs.label()
			positionLoadFalse = p.code(fs, ABCInstruction(OpLFalseSkip, uint8(reg), 0, 0, false))
			fs.label()
			positionLoadTrue = p.code(fs, ABCInstruction(OpLoadTrue, uint8(reg), 0, 0, false))
			// Jump around these booleans if e is not a test.
			if err := fs.patchToHere(fj); err != nil {
				return e, err
			}
		}

		final := fs.label()
		if err := fs.patchList(e.f, final, reg, positionLoadFalse); err != nil {
			return e, err
		}
		if err := fs.patchList(e.f, final, reg, positionLoadTrue); err != nil {
			return e, err
		}
	}

	// We've removed jumps, so no jump lists.
	return nonRelocatableExpression(reg), nil
}

// dischargeToAnyRegister ensures the expression value is in a register,
// making e a non-relocatable expression.
// (Expression still may have jump lists.)
//
// Equivalent to `discharge2anyreg` in upstream Lua.
func (p *parser) dischargeToAnyRegister(fs *funcState, e expressionDescriptor) (expressionDescriptor, error) {
	if e.kind == expressionKindNonRelocatable {
		return e, nil
	}
	reg, err := fs.reserveRegister()
	if err != nil {
		return e, err
	}
	return p.dischargeToRegister(fs, e, reg), nil
}

// dischargeToRegister ensures expression value is in the given register,
// making e a non-relocatable expression.
// (Expression still may have jump lists.)
//
// Equivalent to `discharge2reg` in upstream Lua.
func (p *parser) dischargeToRegister(fs *funcState, e expressionDescriptor, reg registerIndex) expressionDescriptor {
	e = p.dischargeVars(fs, e)
	switch e.kind {
	case expressionKindNil:
		p.codeNil(fs, reg, 1)
	case expressionKindFalse:
		p.code(fs, ABCInstruction(OpLoadFalse, uint8(reg), 0, 0, false))
	case expressionKindTrue:
		p.code(fs, ABCInstruction(OpLoadTrue, uint8(reg), 0, 0, false))
	case expressionKindStringConstant:
		e = p.stringToConstantTable(fs, e)
		fallthrough
	case expressionKindConstant:
		p.codeConstant(fs, reg, e.constantIndex())
	case expressionKindFloatConstant:
		f, _ := e.floatConstant()
		p.codeFloat(fs, reg, f)
	case expressionKindIntConstant:
		i, _ := e.intConstant()
		p.codeInt(fs, reg, i)
	case expressionKindRelocatable:
		newInstruction, ok := fs.Code[e.pc()].WithArgA(uint8(reg))
		if !ok {
			panic("reloc points to an instruction without A argument")
		}
		fs.Code[e.pc()] = newInstruction
	case expressionKindNonRelocatable:
		if ereg := e.register(); reg != ereg {
			p.code(fs, ABCInstruction(OpMove, uint8(reg), uint8(ereg), 0, false))
		}
	case expressionKindJump:
		return e
	default:
		panic("unhandled expression kind")
	}
	return nonRelocatableExpression(reg).withJumpLists(e)
}

// dischargeVars ensures that the expression is not a variable (nor a <const>).
// (Expression still may have jump lists.)
//
// Equivalent to `luaK_dischargevars` in upstream Lua.
func (p *parser) dischargeVars(fs *funcState, e expressionDescriptor) expressionDescriptor {
	switch e.kind {
	case expressionKindConstLocal:
		k := p.activeVariables[e.constLocalIndex()].k
		return constantToExpression(k).withJumpLists(e)
	case expressionKindLocal:
		// Already in register? Becomes a non-relocatable value.
		return nonRelocatableExpression(e.register()).withJumpLists(e)
	case expressionKindUpvalue:
		// Move value to some (pending) register.
		addr := p.code(fs, ABCInstruction(OpGetUpval, 0, uint8(e.upvalueIndex()), 0, false))
		return relocatableExpression(addr).withJumpLists(e)
	case expressionKindIndexUpvalue:
		addr := p.code(fs, ABCInstruction(OpGetTabUp, 0, uint8(e.tableUpvalue()), uint8(e.constantIndex()), false))
		return relocatableExpression(addr).withJumpLists(e)
	case expressionKindIndexInt:
		p.freeRegister(fs, e.tableRegister())
		addr := p.code(fs, ABCInstruction(OpGetI, 0, uint8(e.tableRegister()), uint8(e.indexInt()), false))
		return relocatableExpression(addr).withJumpLists(e)
	case expressionKindIndexString:
		p.freeRegister(fs, e.tableRegister())
		addr := p.code(fs, ABCInstruction(OpGetField, 0, uint8(e.tableRegister()), uint8(e.constantIndex()), false))
		return relocatableExpression(addr).withJumpLists(e)
	case expressionKindIndexed:
		p.freeRegisters(fs, e.tableRegister(), e.indexRegister())
		addr := p.code(fs, ABCInstruction(OpGetTable, 0, uint8(e.tableRegister()), uint8(e.indexRegister()), false))
		return relocatableExpression(addr).withJumpLists(e)
	}
	if e.kind == expressionKindVararg || e.kind == expressionKindCall {
		return p.setOneReturn(fs, e)
	}
	// There is one value available (somewhere).
	return e
}

// MultiReturn is the sentinel
// that indicates that an arbitrary number of result values are accepted.
const MultiReturn = -1

// setReturns fixes an expression to return the given number of results.
// If e is not a multi-ret expression (i.e. a function call or vararg),
// setReturns returns an error.
//
// Equivalent to `luaK_setreturns` in upstream Lua.
func (p *parser) setReturns(fs *funcState, e expressionDescriptor, nResults int) error {
	c := nResults + 1
	if !(0 <= c && c <= maxArgC) {
		return fmt.Errorf("internal error: number of results (%d) out of range for setReturns", nResults)
	}
	switch e.kind {
	case expressionKindCall:
		i := fs.Code[e.pc()]
		fs.Code[e.pc()] = ABCInstruction(
			i.OpCode(),
			i.ArgA(),
			i.ArgB(),
			uint8(c),
			i.K(),
		)
	case expressionKindVararg:
		i := fs.Code[e.pc()]
		fs.Code[e.pc()] = ABCInstruction(
			i.OpCode(),
			uint8(fs.firstFreeRegister),
			i.ArgB(),
			uint8(c),
			i.K(),
		)
		if err := fs.reserveRegisters(1); err != nil {
			return err
		}
	default:
		return fmt.Errorf("setReturns on %v", e.kind)
	}
	return nil
}

// setOneReturn fixes an expression to return one result.
// If expression is not a multi-ret expression
// (i.e. a function call or vararg),
// it already returns one result, so nothing needs to be done.
// Function calls become [expressionKindNonRelocatable] expressions
// (as its result comes fixed in the base register of the call),
// while vararg expressions become [expressionKindRelocatable]
// (as [OpVararg] puts its results where it wants).
// (Calls are created returning one result,
// so that does not need to be fixed.)
//
// Equivalent to `luaK_setoneret` in upstream Lua.
func (p *parser) setOneReturn(fs *funcState, e expressionDescriptor) expressionDescriptor {
	switch e.kind {
	case expressionKindCall:
		i := fs.Code[e.pc()]
		return nonRelocatableExpression(registerIndex(i.ArgA())).withJumpLists(e)
	case expressionKindVararg:
		pc := e.pc()
		i := fs.Code[pc]
		fs.Code[pc] = ABCInstruction(i.OpCode(), i.ArgA(), i.ArgB(), 2, i.K())
		return relocatableExpression(pc).withJumpLists(e)
	default:
		return e
	}
}

// freeExpression frees the register used (if any) by the given expression.
//
// Equivalent to `freeexp` in upstream Lua.
func (p *parser) freeExpression(fs *funcState, e expressionDescriptor) {
	if e.kind == expressionKindNonRelocatable {
		p.freeRegister(fs, e.register())
	}
}

// freeExpressions frees the registers used (if any) by the given expressions.
//
// Equivalent to `freeExps` in upstream Lua.
func (p *parser) freeExpressions(fs *funcState, e1, e2 expressionDescriptor) {
	switch {
	case e1.kind == expressionKindNonRelocatable && e2.kind == expressionKindNonRelocatable:
		p.freeRegisters(fs, e1.register(), e2.register())
	case e1.kind == expressionKindNonRelocatable:
		p.freeRegister(fs, e1.register())
	case e2.kind == expressionKindNonRelocatable:
		p.freeRegister(fs, e2.register())
	}
}

// freeRegister frees the given register
// if it is neither a constant index nor a local variable.
//
// Equivalent to `freereg` in upstream Lua.
func (p *parser) freeRegister(fs *funcState, reg registerIndex) {
	if reg >= p.numVariablesInStack(fs) {
		fs.firstFreeRegister--
		if reg != fs.firstFreeRegister {
			panic("freereg should be called on fs.firstFreeRegister+1")
		}
	}
}

// freeRegisters frees two registers.
//
// Equivalent to `freeregs` in upstream Lua.
func (p *parser) freeRegisters(fs *funcState, reg1, reg2 registerIndex) {
	p.freeRegister(fs, max(reg1, reg2))
	p.freeRegister(fs, min(reg1, reg2))
}

// toConstant returns a constant expression's value.
//
// Equivalent to `luaK_exp2const` in upstream Lua.
func (p *parser) toConstant(e expressionDescriptor) (_ Value, isConstant bool) {
	if e.hasJumps() {
		return Value{}, false
	}
	if e.kind == expressionKindConstLocal {
		return p.activeVariables[e.constLocalIndex()].k, true
	}
	return e.toValue()
}

// stringToConstantTable converts a string constant to a constant table expression.
//
// Equivalent to `str2K` in upstream Lua.
func (p *parser) stringToConstantTable(fs *funcState, e expressionDescriptor) expressionDescriptor {
	s, ok := e.stringConstant()
	if !ok {
		panic("stringToConstant must be called on expressionKindStringConstant")
	}
	k := fs.addConstant(StringValue(s))
	return constantTableExpression(k).withJumpLists(e)
}

// newTableInstructions returns a sequence of instructions for [OpNewTable].
//
// Mostly equivalent to `luaK_settablesize` in upstream Lua,
// but returns the instructions in an array.
func newTableInstructions(ra registerIndex, arraySize, hashSize int) [2]Instruction {
	var rb uint8
	if hashSize != 0 {
		rb = ceilLog2(uint(hashSize)) + 1
	}
	extra := uint32(arraySize / (maxArgC + 1))
	rc := uint8(arraySize % (maxArgC + 1))
	return [2]Instruction{
		ABCInstruction(OpNewTable, uint8(ra), rb, rc, extra > 0),
		ExtraArgument(extra),
	}
}

// ceilLog2 computes ceil(log2(x)).
func ceilLog2(x uint) uint8 {
	var l uint8
	x--
	for x >= 256 {
		l += 8
		x >>= 8
	}
	return l + log2Table[x]
}

// log2Table is a lookup table where log2Table[i] = ceil(log2(i - 1)).
var log2Table = [...]uint8{
	0, 1, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
	6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6,
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
}
