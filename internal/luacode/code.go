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
func (p *parser) code(fs *funcState, i Instruction) int {
	fs.Code = append(fs.Code, i)
	fs.saveLineInfo(p.lastLine)
	return len(fs.Code) - 1
}

// codeNil appends an [OpLoadNil] instruction to fs.Code.
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
func (p *parser) codeJump(fs *funcState) int {
	return p.code(fs, JInstruction(OpJmp, noJump))
}

// codeReturn appends a return instruction to fs.Code.
// codeReturn panics if nret is out of range.
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
func (p *parser) codeConstant(fs *funcState, reg registerIndex, k int) int {
	if k > maxArgBx {
		pc := p.code(fs, ABxInstruction(OpLoadKX, uint8(reg), 0))
		p.code(fs, ExtraArgument(uint32(k)))
		return pc
	}
	return p.code(fs, ABxInstruction(OpLoadK, uint8(reg), int32(k)))
}

// codeStoreVar appends instructions to store the result of expr into variable v.
// expr is no longer valid after a call to codeStoreVar.
func (p *parser) codeStoreVar(fs *funcState, v, expr expDesc) error {
	switch v.kind {
	case expKindLocal:
		p.freeExp(fs, expr)
		_, err := p.exp2reg(fs, expr, v.register())
		return err
	case expKindUpval:
		var e registerIndex
		var err error
		expr, e, err = p.exp2anyreg(fs, expr)
		if err != nil {
			return err
		}
		p.code(fs, ABCInstruction(OpSetUpval, uint8(e), uint8(v.upvalueIndex()), 0, false))
	case expKindIndexUp:
		var err error
		expr, err = p.codeABRK(fs, OpSetTabUp, uint8(v.tableUpvalue()), uint8(v.constIndex()), expr)
		if err != nil {
			return err
		}
	case expKindIndexI:
		var err error
		expr, err = p.codeABRK(fs, OpSetI, uint8(v.tableRegister()), uint8(v.indexInt()), expr)
		if err != nil {
			return err
		}
	case expKindIndexStr:
		var err error
		expr, err = p.codeABRK(fs, OpSetField, uint8(v.tableRegister()), uint8(v.constIndex()), expr)
		if err != nil {
			return err
		}
	case expKindIndexed:
		var err error
		expr, err = p.codeABRK(fs, OpSetTable, uint8(v.tableRegister()), uint8(v.indexRegister()), expr)
		if err != nil {
			return err
		}
	default:
		p.freeExp(fs, expr)
		return fmt.Errorf("invalid variable kind to store (%v)", v.kind)
	}

	p.freeExp(fs, expr)
	return nil
}

// codeSelf appends an [OpSelf] instruction to fs.Code.
// This has the effect of converting expression e into "e:key(e,".
// Both e and key are invalid after a call to codeSelf.
func (p *parser) codeSelf(fs *funcState, e, key expDesc) error {
	e, ereg, err := p.exp2anyreg(fs, e)
	if err != nil {
		return err
	}
	p.freeExp(fs, e)

	// Reserve registers for function and self produced by OpSelf.
	baseRegister := fs.firstFreeRegister
	if err := fs.reserveRegisters(2); err != nil {
		return err
	}

	key, err = p.codeABRK(fs, OpSelf, uint8(baseRegister), uint8(ereg), key)
	if err != nil {
		return err
	}
	p.freeExp(fs, key)

	return nil
}

// codeGoIfTrue appends instructions to go through if e is true, jump otherwise.
func (p *parser) codeGoIfTrue(fs *funcState, e expDesc) (expDesc, error) {
	e = p.dischargeVars(fs, e)
	var pc int
	switch e.kind {
	case expKindJmp:
		if err := fs.negateCondition(e.pc()); err != nil {
			return e, err
		}
	case expKindK, expKindKFlt, expKindKInt, expKindKStr, expKindTrue:
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
	here := fs.label()
	if err := fs.patchList(e.t, here, noRegister, here); err != nil {
		return e, err
	}
	e.t = noJump
	return e, nil
}

// codeGoIfFalse appends instructions to go through if e is false, jump otherwise.
func (p *parser) codeGoIfFalse(fs *funcState, e expDesc) (expDesc, error) {
	e = p.dischargeVars(fs, e)
	var pc int
	switch e.kind {
	case expKindJmp:
		pc = e.pc()
	case expKindNil, expKindFalse:
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
	here := fs.label()
	if err := fs.patchList(e.t, here, noRegister, here); err != nil {
		return e, err
	}
	e.f = noJump
	return e, nil
}

// jumpOnCond appends an instruction to jump if e is cond
// (that is, if cond is true, code will jump if e is true)
// and returns the jump position.
func (p *parser) jumpOnCond(fs *funcState, e expDesc, cond bool) (int, error) {
	if e.kind == expKindReloc {
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
	p.freeExp(fs, e)
	p.code(fs, ABCInstruction(OpTestSet, uint8(noRegister), uint8(e.register()), 0, cond))
	return p.codeJump(fs), nil
}

// codeNot codes "not e", doing constant folding.
func (p *parser) codeNot(fs *funcState, e expDesc) (expDesc, error) {
	switch e.kind {
	case expKindNil, expKindFalse:
		e.kind = expKindTrue
	case expKindK, expKindKFlt, expKindKInt, expKindKStr, expKindTrue:
		e.kind = expKindFalse
	case expKindJmp:
		if err := fs.negateCondition(e.pc()); err != nil {
			return e, err
		}
	case expKindReloc, expKindNonReloc:
		var err error
		e, err = p.dischargeToAnyRegister(fs, e)
		if err != nil {
			return e, err
		}
		pc := p.code(fs, ABCInstruction(OpNot, 0, uint8(e.register()), 0, false))
		e = newRelocExpDesc(pc).withJumpLists(e)
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
func (p *parser) codeIndexed(fs *funcState, t, k expDesc) (expDesc, error) {
	if t.hasJumps() {
		return voidExpDesc(), errors.New("internal error: codeIndexed: table expression has jumps")
	}

	if k.kind == expKindKStr {
		k = p.stringToConstantTable(fs, k)
	}
	isKstr := k.kind == expKindK &&
		!k.hasJumps() &&
		k.constIndex() <= maxArgB &&
		fs.Constants[k.constIndex()].isShortString()
	if t.kind == expKindUpval && !isKstr {
		// [OpGetTabUp] can only index short strings.
		// Copy the table from an upvalue to a register.
		var err error
		t, _, err = p.exp2anyreg(fs, t)
		if err != nil {
			return voidExpDesc(), err
		}
	}

	switch t.kind {
	case expKindUpval:
		return newIndexUpExpDesc(t.upvalueIndex(), uint16(k.constIndex())), nil
	case expKindLocal, expKindNonReloc:
		if isKstr {
			return newIndexStrExpDesc(t.register(), uint16(k.constIndex())), nil
		} else if i, isInt := k.intConstant(); isInt && !k.hasJumps() && 0 <= i && i <= maxArgC {
			return newIndexIExpDesc(t.register(), uint16(i)), nil
		} else {
			_, reg, err := p.exp2anyreg(fs, k)
			if err != nil {
				return voidExpDesc(), err
			}
			return newIndexedExpDesc(t.register(), reg), nil
		}
	default:
		return voidExpDesc(), fmt.Errorf("internal error: codeIndexed: unhandled table kind %v", t.kind)
	}
}

// codePrefix appends the code to apply a prefix operator to an expression
// to fs.Code.
func (p *parser) codePrefix(fs *funcState, operator unaryOperator, e expDesc, line int) (expDesc, error) {
	e = p.dischargeVars(fs, e)
	switch operator {
	case unaryOperatorMinus, unaryOperatorBNot:
		fakeRHS := newIntConstExpDesc(0)
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
		return voidExpDesc(), fmt.Errorf("internal error: codePrefix: unhandled operator %v", operator)
	}
}

// codeUnaryExpValue appends the code for any unary expresion except "not"
// to fs.Code.
func (p *parser) codeUnaryExpValue(fs *funcState, op OpCode, e expDesc, line int) (expDesc, error) {
	e, r, err := p.exp2anyreg(fs, e)
	if err != nil {
		return e, err
	}
	p.freeExp(fs, e)
	pc := p.code(fs, ABCInstruction(op, 0, uint8(r), 0, false))
	fs.fixLineInfo(line)
	return newRelocExpDesc(pc).withJumpLists(e), nil
}

// codeInfix processes the first operand of a binary expression
// before reading the second operand.
// The caller should call [*parser.codePostfix] after reading the second operand.
func (p *parser) codeInfix(fs *funcState, operator binaryOperator, v expDesc) (expDesc, error) {
	v = p.dischargeVars(fs, v)
	switch operator {
	case binaryOperatorAnd:
		return p.codeGoIfTrue(fs, v)
	case binaryOperatorOr:
		return p.codeGoIfFalse(fs, v)
	case binaryOperatorConcat:
		var err error
		v, _, err = p.exp2nextReg(fs, v)
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
		v, _, err = p.exp2anyreg(fs, v)
		return v, err
	case binaryOperatorEq, binaryOperatorNE:
		if v.isNumeral() {
			// Preserve numerals because they may be used as an immediate operand.
			return v, nil
		}
		var err error
		v, _, _, err = p.expToRK(fs, v)
		return v, err
	case binaryOperatorLT, binaryOperatorLE, binaryOperatorGT, binaryOperatorGE:
		if _, _, isSigned := v.toSignedArg(); isSigned {
			// Preserve numerals because they may be used as an immediate operand.
			return v, nil
		}
		var err error
		v, _, err = p.exp2anyreg(fs, v)
		return v, err
	default:
		return v, fmt.Errorf("internal error: codeInfix: unhandled operator %v", operator)
	}
}

// codePostfix finalizes the code for a binary operation
// after reading the second operand.
// This must have been preceded by a call to [*parser.codeInfix].
func (p *parser) codePostfix(fs *funcState, operator binaryOperator, e1, e2 expDesc, line int) (expDesc, error) {
	e2 = p.dischargeVars(fs, e2)
	if operator, ok := operator.toArithmetic(); ok {
		if result, folded := p.foldConstants(operator, e1, e2); folded {
			return result, nil
		}
	}

	switch operator {
	case binaryOperatorAnd:
		if e1.t != noJump {
			return voidExpDesc(), errors.New("internal error: codePostfix: list should have been closed by codeInfix")
		}
		var err error
		e2.f, err = fs.concatJumpList(e2.f, e1.f)
		if err != nil {
			return voidExpDesc(), err
		}
		return e2, nil
	case binaryOperatorOr:
		if e1.t != noJump {
			return voidExpDesc(), errors.New("internal error: codePostfix: list should have been closed by codeInfix")
		}
		var err error
		e2.t, err = fs.concatJumpList(e2.t, e1.t)
		if err != nil {
			return voidExpDesc(), err
		}
		return e2, nil
	case binaryOperatorConcat:
		var err error
		e2, _, err = p.exp2nextReg(fs, e2)
		if err != nil {
			return voidExpDesc(), err
		}
		p.codeConcat(fs, e1, e2, line)
		return e1, nil
	case binaryOperatorAdd, binaryOperatorMul:
		return p.codeCommutative(fs, operator, e1, e2, line)
	case binaryOperatorSub:
		result, err := p.finishBinaryExpNegated(fs, e1, e2, OpAddI, line, TagMethodSub)
		if err != nil {
			return voidExpDesc(), err
		}
		if result.kind != expKindVoid {
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
			return p.codeBinaryExpImmediate(fs, OpShlI, e2, e1, true, line, TagMethodSHL)
		}
		if result, err := p.finishBinaryExpNegated(fs, e1, e2, OpShrI, line, TagMethodSHL); err != nil {
			return voidExpDesc(), err
		} else if result.kind != expKindVoid {
			return result, nil
		}
		return p.codeBinaryExp(fs, operator, e1, e2, line)
	case binaryOperatorShiftR:
		if i2, ok := e2.intConstant(); ok && fitsSignedArg(i2) {
			// r1 >> I
			return p.codeBinaryExpImmediate(fs, OpShrI, e1, e2, false, line, TagMethodSHR)
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
		return voidExpDesc(), fmt.Errorf("internal error: codePostfix: unhandled operator %v", operator)
	}
}

// codeSetTableSize
func (p *parser) codeSetTableSize(fs *funcState, pc int, ra registerIndex, aSize, hSize int) {

}

// TODO(now): codeSetList

func (p *parser) codeCommutative(fs *funcState, operator binaryOperator, e1, e2 expDesc, line int) (expDesc, error) {
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

func (p *parser) codeBitwise(fs *funcState, operator binaryOperator, e1, e2 expDesc, line int) (expDesc, error) {
	flip := e1.kind == expKindKInt
	if flip {
		e1, e2 = e2, e1
	}
	if e2.kind == expKindKInt {
		if e2, _, ok := p.expToK(fs, e2); ok {
			return p.codeBinaryExpConstant(fs, operator, e1, e2, flip, line)
		}
	}
	return p.codeBinaryExpNoConstants(fs, operator, e1, e2, flip, line)
}

func (p *parser) codeArithmetic(fs *funcState, operator binaryOperator, e1, e2 expDesc, flip bool, line int) (expDesc, error) {
	if e2.isNumeral() {
		if e2, _, ok := p.expToK(fs, e2); ok {
			return p.codeBinaryExpConstant(fs, operator, e1, e2, flip, line)
		}
	}
	return p.codeBinaryExpNoConstants(fs, operator, e1, e2, flip, line)
}

// codeBinaryExpNoConstants appends the instructions
// for a binary expression without constant operands
// to fs.Code.
func (p *parser) codeBinaryExpNoConstants(fs *funcState, operator binaryOperator, e1, e2 expDesc, flip bool, line int) (expDesc, error) {
	if flip {
		// Back to original order.
		e1, e2 = e2, e1
	}
	return p.codeBinaryExp(fs, operator, e1, e2, line)
}

func (p *parser) codeBinaryExp(fs *funcState, operator binaryOperator, e1, e2 expDesc, line int) (expDesc, error) {
	op, ok := operator.toOpCode(OpAdd)
	if !ok {
		return voidExpDesc(), fmt.Errorf("internal error: codeBinaryExp: %v does not translate cleanly to OpCode", operator)
	}
	event, ok := operator.tagMethod()
	if !ok {
		return voidExpDesc(), fmt.Errorf("internal error: codeBinaryExp: %v does not have a TagMethod", operator)
	}
	if !e1.kind.isCompileTimeConstant() && e1.kind != expKindNonReloc && e1.kind != expKindReloc {
		return voidExpDesc(), fmt.Errorf("internal error: codeBinaryExp: left-side operand must be a constant or in a register")
	}

	e2, v2, err := p.exp2anyreg(fs, e2)
	if err != nil {
		return voidExpDesc(), err
	}
	return p.finishBinaryExpValue(fs, e1, e2, op, uint8(v2), false, line, OpMMBin, event)
}

func (p *parser) codeBinaryExpImmediate(fs *funcState, op OpCode, e1, e2 expDesc, flip bool, line int, event TagMethod) (expDesc, error) {
	i, ok := e2.intConstant()
	if !ok {
		return voidExpDesc(), fmt.Errorf("internal error: codeBinaryExpImmediate: right-side operand must be an immediate integer")
	}
	v2, ok := ToSignedArg(i)
	if !ok {
		return voidExpDesc(), fmt.Errorf("internal error: codeBinaryExpImmediate: right-side operand (%d) out of range", i)
	}
	return p.finishBinaryExpValue(fs, e1, e2, op, v2, flip, line, OpMMBinI, event)
}

func (p *parser) codeBinaryExpConstant(fs *funcState, operator binaryOperator, e1, e2 expDesc, flip bool, line int) (expDesc, error) {
	event, ok := operator.tagMethod()
	if !ok {
		return voidExpDesc(), fmt.Errorf("internal error: codeBinaryExpConstant: operator %v does not have a metamethod", operator)
	}
	if e2.kind != expKindK {
		return voidExpDesc(), fmt.Errorf("internal error: codeBinaryExpConstant: right-side operand must be a reference to the Constants table")
	}
	v2 := e2.constIndex()
	op, ok := operator.toOpCode(OpAddK)
	if !ok {
		return voidExpDesc(), fmt.Errorf("internal error: codeBinaryExpConstant: %v does not translate cleanly to OpCode", operator)
	}
	return p.finishBinaryExpValue(fs, e1, e2, op, uint8(v2), flip, line, OpMMBinK, event)
}

func (p *parser) finishBinaryExpValue(fs *funcState, e1, e2 expDesc, op OpCode, v2 uint8, flip bool, line int, mmop OpCode, event TagMethod) (expDesc, error) {
	e1, v1, err := p.exp2anyreg(fs, e1)
	if err != nil {
		return voidExpDesc(), err
	}
	pc := p.code(fs, ABCInstruction(op, 0, uint8(v1), v2, false))
	p.freeExps(fs, e1, e2)
	fs.fixLineInfo(line)
	p.code(fs, ABCInstruction(mmop, uint8(v1), v2, uint8(event), flip))
	fs.fixLineInfo(line)
	return newRelocExpDesc(pc).withJumpLists(e1), nil
}

// finishBinaryExpNegated attempts to append the instructions
// for a binary expression with the right-side operand negated
// to fs.Code.
// If the attempt fails, then finishBinaryExpNegated returns a [voidExpDesc] with a nil error.
func (p *parser) finishBinaryExpNegated(fs *funcState, e1, e2 expDesc, op OpCode, line int, event TagMethod) (expDesc, error) {
	i2, ok := e2.intConstant()
	if !ok || e2.hasJumps() {
		return voidExpDesc(), nil
	}
	v2, ok := ToSignedArg(i2)
	if !ok {
		return voidExpDesc(), nil
	}
	negV2, ok := ToSignedArg(-i2)
	if !ok {
		return voidExpDesc(), nil
	}
	const mmop = OpMMBinI
	result, err := p.finishBinaryExpValue(fs, e1, e2, op, negV2, false, line, mmop, event)
	if err != nil {
		return voidExpDesc(), err
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
func (p *parser) codeConcat(fs *funcState, e1, e2 expDesc, line int) {
	r1 := e1.register()

	// For "(e1 .. e2.1 .. e2.2)"
	// (which is "(e1 .. (e2.1 .. e2.2))" because concatenation is right associative),
	// merge both [OpConcat] instructions.
	ie2 := fs.previousInstruction()
	if ie2 != nil && ie2.OpCode() == OpConcat && r1+1 == registerIndex(ie2.ArgA()) {
		n := ie2.ArgB() // Number of elements concatenated in e2.
		p.freeExp(fs, e2)
		*ie2 = ABCInstruction(OpConcat, uint8(r1), n+1, ie2.ArgC(), ie2.K())
		return
	}

	p.code(fs, ABCInstruction(OpConcat, uint8(r1), 2, 0, false))
	p.freeExp(fs, e2)
	fs.fixLineInfo(line)
}

func (p *parser) codeOrder(fs *funcState, operator binaryOperator, e1, e2 expDesc) (expDesc, error) {
	var op OpCode
	var r1 registerIndex
	var b, c uint8
	if immediate, isFloat, ok := e2.toSignedArg(); ok {
		var err error
		e1, r1, err = p.exp2anyreg(fs, e1)
		if err != nil {
			return voidExpDesc(), err
		}
		b = immediate
		if isFloat {
			c = 1
		}
		op, _ = operator.toOpCode(OpLTI)
	} else if immediate, isFloat, ok = e1.toSignedArg(); ok {
		var err error
		e2, r1, err = p.exp2anyreg(fs, e2)
		if err != nil {
			return voidExpDesc(), err
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
			return voidExpDesc(), fmt.Errorf("internal error: codeOrder: unhandled operator %v", operator)
		}
	} else {
		var err error
		e1, r1, err = p.exp2anyreg(fs, e1)
		if err != nil {
			return voidExpDesc(), err
		}
		var r2 registerIndex
		e2, r2, err = p.exp2anyreg(fs, e2)
		if err != nil {
			return voidExpDesc(), err
		}
		b = uint8(r2)
		op, _ = operator.toOpCode(OpLT)
	}

	p.freeExps(fs, e1, e2)
	p.code(fs, ABCInstruction(op, uint8(r1), b, c, true))
	pc := p.codeJump(fs)
	return newJumpExpDesc(pc), nil
}

// codeEq appends code for equality comparisons ("==" or "~=") to fs.Code.
// e1 must have already turned into an R/K by [*parser.codeInfix].
func (p *parser) codeEq(fs *funcState, operator binaryOperator, e1, e2 expDesc) (expDesc, error) {
	switch e1.kind {
	case expKindK, expKindKInt, expKindKFlt:
		// Swap constant/immediate to right side.
		e1, e2 = e2, e1
	case expKindNonReloc:
		// Fine as-is.
	default:
		return voidExpDesc(), fmt.Errorf("internal error: codeEq: left-side operand should have turned into a register or a constant (found %v)", e1.kind)
	}

	e1, r1, err := p.exp2anyreg(fs, e1)
	if err != nil {
		return voidExpDesc(), err
	}
	var op OpCode
	var b uint8
	var c uint8 // Not needed here, but kept for symmetry.
	if immediate, isFloat, isImmediate := e2.toSignedArg(); isImmediate {
		op = OpEqI
		b = immediate
		if isFloat {
			c = 1
		}
	} else {
		var k bool
		e2, b, k, err = p.expToRK(fs, e2)
		if err != nil {
			return voidExpDesc(), err
		}
		if k {
			op = OpEqK
		} else {
			op = OpEq
			// TODO(maybe): expToRK should have already converted to register.
			// Is this necessary?
			var r2 registerIndex
			e2, r2, err = p.exp2anyreg(fs, e2)
			if err != nil {
				return voidExpDesc(), err
			}
			b = uint8(r2)
		}
	}

	p.freeExps(fs, e1, e2)
	p.code(fs, ABCInstruction(op, uint8(r1), b, c, operator == binaryOperatorEq))
	pc := p.codeJump(fs)
	return newJumpExpDesc(pc).withJumpLists(e1), nil
}

// foldConstants tries to statically evaluate an expression.
func (p *parser) foldConstants(op ArithmeticOperator, e1, e2 expDesc) (expDesc, bool) {
	v1, ok := e1.toNumeral()
	if !ok {
		return voidExpDesc(), false
	}
	v2, ok := e2.toNumeral()
	if !ok {
		return voidExpDesc(), false
	}

	result, err := Arithmetic(op, v1, v2)
	if err != nil {
		return voidExpDesc(), false
	}
	if result.IsInteger() {
		i, _ := result.Int64(OnlyIntegral)
		return newIntConstExpDesc(i), true
	}
	n, ok := result.Float64()
	if !ok {
		// Shouldn't occur, but coding defensively.
		return voidExpDesc(), false
	}
	if math.IsNaN(n) || n == 0 {
		// Don't fold numbers that have tricky equality properties.
		return voidExpDesc(), false
	}
	return newFloatConstExpDesc(n), true
}

// expToValue ensures the final expression result
// is either in a register or it is a constant.
func (p *parser) expToValue(fs *funcState, e expDesc) (expDesc, error) {
	if e.hasJumps() {
		e, _, err := p.exp2anyreg(fs, e)
		return e, err
	}
	return p.dischargeVars(fs, e), nil
}

func (p *parser) codeABRK(fs *funcState, op OpCode, a, b uint8, e expDesc) (expDesc, error) {
	e, c, k, err := p.expToRK(fs, e)
	if err != nil {
		return e, err
	}
	p.code(fs, ABCInstruction(op, a, b, c, k))
	return e, nil
}

// maxIndexRK is the maximum index that can be used
// as either a register index or a Constants table index.
const maxIndexRK = maxArgC

// expToRK converts the expression to either [expKindK]
// with an index less than maxIndexRK
// or [expKindNonReloc].
// c is the register index or the constant index as an [Instruction] argument.
// k is true if the resulting expression is [expKindK].
func (p *parser) expToRK(fs *funcState, e expDesc) (_ expDesc, c uint8, k bool, err error) {
	if e, c, ok := p.expToK(fs, e); ok {
		return e, c, true, nil
	}
	e, reg, err := p.exp2anyreg(fs, e)
	return e, uint8(reg), false, err
}

// expToK attempts to make e an [expKindK]
// with an index in the range of R/K indices.
func (p *parser) expToK(fs *funcState, e expDesc) (_ expDesc, idx uint8, ok bool) {
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
	return newConstExpDesc(k), uint8(k), true
}

// exp2anyregup ensures the final expression result
// is either in a register or in an upvalue.
func (p *parser) exp2anyregup(fs *funcState, e expDesc) (expDesc, error) {
	if e.kind == expKindUpval && !e.hasJumps() {
		return e, nil
	}
	e, _, err := p.exp2anyreg(fs, e)
	return e, err
}

// exp2anyreg ensures the final expression result is in some (any) register
// and returns that register.
//
// On success, the result of exp2nextreg will always be [expKindNonReloc].
func (p *parser) exp2anyreg(fs *funcState, e expDesc) (expDesc, registerIndex, error) {
	e = p.dischargeVars(fs, e)
	if e.kind == expKindNonReloc {
		if !e.hasJumps() {
			// Result is already in a register.
			return e, e.register(), nil
		}
		if e.register() >= p.numVariablesInStack(fs) {
			// The register is not a local: put the final result in it.
			e, err := p.exp2reg(fs, e, e.register())
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
	return p.exp2nextReg(fs, e)
}

// exp2nextReg ensures the final expression result is in the next available register.
//
// On success, the result of exp2nextreg will always be [expKindNonReloc].
func (p *parser) exp2nextReg(fs *funcState, e expDesc) (expDesc, registerIndex, error) {
	e = p.dischargeVars(fs, e)
	p.freeExp(fs, e)
	reg, err := fs.reserveRegister()
	if err != nil {
		return e, noRegister, err
	}
	e, err = p.exp2reg(fs, e, reg)
	return e, reg, err
}

// exp2reg ensures the final expression result
// (which includes results from its jump lists)
// is in the given register.
// If expression has jumps,
// need to patch these jumps either to its final position
// or to "load" instructions
// (for those tests that do not produce values).
//
// On success, the result of exp2reg will always be [expKindNonReloc].
func (p *parser) exp2reg(fs *funcState, e expDesc, reg registerIndex) (expDesc, error) {
	e = p.dischargeToRegister(fs, e, reg)

	if e.kind == expKindJmp {
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
			if e.kind != expKindJmp {
				fj = p.codeJump(fs)
			}
			fs.label()
			positionLoadFalse = p.code(fs, ABCInstruction(OpLFalseSkip, uint8(reg), 0, 0, false))
			fs.label()
			positionLoadTrue = p.code(fs, ABCInstruction(OpLoadTrue, uint8(reg), 0, 0, false))
			// Jump around these booleans if e is not a test.
			here := fs.label()
			if err := fs.patchList(fj, here, noRegister, here); err != nil {
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
	return newNonRelocExpDesc(reg), nil
}

// dischargeToAnyRegister ensures the expression value is in a register,
// making e a non-relocatable expression.
// (Expression still may have jump lists.)
func (p *parser) dischargeToAnyRegister(fs *funcState, e expDesc) (expDesc, error) {
	if e.kind == expKindNonReloc {
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
func (p *parser) dischargeToRegister(fs *funcState, e expDesc, reg registerIndex) expDesc {
	e = p.dischargeVars(fs, e)
	switch e.kind {
	case expKindNil:
		p.codeNil(fs, reg, 1)
	case expKindFalse:
		p.code(fs, ABCInstruction(OpLoadFalse, uint8(reg), 0, 0, false))
	case expKindTrue:
		p.code(fs, ABCInstruction(OpLoadTrue, uint8(reg), 0, 0, false))
	case expKindKStr:
		e = p.stringToConstantTable(fs, e)
		fallthrough
	case expKindK:
		p.codeConstant(fs, reg, e.constIndex())
	case expKindKFlt:
		f, _ := e.floatConstant()
		p.codeFloat(fs, reg, f)
	case expKindKInt:
		i, _ := e.intConstant()
		p.codeInt(fs, reg, i)
	case expKindReloc:
		newInstruction, ok := fs.Code[e.pc()].WithArgA(uint8(reg))
		if !ok {
			panic("reloc points to an instruction without A argument")
		}
		fs.Code[e.pc()] = newInstruction
	case expKindNonReloc:
		if ereg := e.register(); reg != ereg {
			p.code(fs, ABCInstruction(OpMove, uint8(reg), uint8(ereg), 0, false))
		}
	case expKindJmp:
		return e
	default:
		panic("unhandled expression kind")
	}
	return newNonRelocExpDesc(reg).withJumpLists(e)
}

// dischargeVars ensures that the expression is not a variable (nor a <const>).
// (Expression still may have jump lists.)
func (p *parser) dischargeVars(fs *funcState, e expDesc) expDesc {
	switch e.kind {
	case expKindConst:
		return constToExp(fs.Constants[e.constIndex()]).withJumpLists(e)
	case expKindLocal:
		// Already in register? Becomes a non-relocatable value.
		return newNonRelocExpDesc(e.register()).withJumpLists(e)
	case expKindUpval:
		// Move value to some (pending) register.
		addr := p.code(fs, ABCInstruction(OpGetUpval, 0, uint8(e.upvalueIndex()), 0, false))
		return newRelocExpDesc(addr).withJumpLists(e)
	case expKindIndexUp:
		addr := p.code(fs, ABCInstruction(OpGetTabUp, 0, uint8(e.tableUpvalue()), uint8(e.constIndex()), false))
		return newRelocExpDesc(addr).withJumpLists(e)
	case expKindIndexI:
		p.freeReg(fs, e.tableRegister())
		addr := p.code(fs, ABCInstruction(OpGetI, 0, uint8(e.tableRegister()), uint8(e.indexInt()), false))
		return newRelocExpDesc(addr).withJumpLists(e)
	case expKindIndexStr:
		p.freeReg(fs, e.tableRegister())
		addr := p.code(fs, ABCInstruction(OpGetField, 0, uint8(e.tableRegister()), uint8(e.constIndex()), false))
		return newRelocExpDesc(addr).withJumpLists(e)
	case expKindIndexed:
		p.freeRegs(fs, e.tableRegister(), e.indexRegister())
		addr := p.code(fs, ABCInstruction(OpGetTable, 0, uint8(e.tableRegister()), uint8(e.indexRegister()), false))
		return newRelocExpDesc(addr).withJumpLists(e)
	}
	if e.kind == expKindVararg || e.kind == expKindCall {
		return p.setOneReturn(fs, e)
	}
	// There is one value available (somewhere).
	return e
}

const multiReturn = -1

// setReturns fixes an expression to return the given number of results.
// If e is not a multi-ret expression (i.e. a function call or vararg),
// setReturns returns an error.
func (p *parser) setReturns(fs *funcState, e expDesc, nResults int) error {
	c := nResults + 1
	if !(0 <= c && c <= maxArgC) {
		return fmt.Errorf("internal error: number of results (%d) out of range for setReturns", nResults)
	}
	switch e.kind {
	case expKindCall:
		i := fs.Code[e.pc()]
		fs.Code[e.pc()] = ABCInstruction(
			i.OpCode(),
			i.ArgA(),
			i.ArgB(),
			uint8(c),
			i.K(),
		)
	case expKindVararg:
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
// Function calls become [expKindNonReloc] expressions
// (as its result comes fixed in the base register of the call),
// while vararg expressions become [expKindReloc]
// (as [OpVararg] puts its results where it wants).
// (Calls are created returning one result,
// so that does not need to be fixed.)
func (p *parser) setOneReturn(fs *funcState, e expDesc) expDesc {
	switch e.kind {
	case expKindCall:
		i := fs.Code[e.pc()]
		return newNonRelocExpDesc(registerIndex(i.ArgA())).withJumpLists(e)
	case expKindVararg:
		pc := e.pc()
		i := fs.Code[pc]
		fs.Code[pc] = ABCInstruction(i.OpCode(), i.ArgA(), i.ArgB(), 2, i.K())
		return newRelocExpDesc(pc).withJumpLists(e)
	default:
		return e
	}
}

// freeExp frees the register used (if any) by the given expression.
func (p *parser) freeExp(fs *funcState, e expDesc) {
	if e.kind == expKindNonReloc {
		p.freeReg(fs, e.register())
	}
}

// freeExps frees the registers used (if any) by the given expressions.
func (p *parser) freeExps(fs *funcState, e1, e2 expDesc) {
	switch {
	case e1.kind == expKindNonReloc && e2.kind == expKindNonReloc:
		p.freeRegs(fs, e1.register(), e2.register())
	case e1.kind == expKindNonReloc:
		p.freeReg(fs, e1.register())
	case e2.kind == expKindNonReloc:
		p.freeReg(fs, e2.register())
	}
}

func (p *parser) freeReg(fs *funcState, reg registerIndex) {
	if reg >= p.numVariablesInStack(fs) {
		fs.firstFreeRegister--
		if reg != fs.firstFreeRegister {
			panic("freereg should be called on fs.firstFreeRegister+1")
		}
	}
}

func (p *parser) freeRegs(fs *funcState, reg1, reg2 registerIndex) {
	p.freeReg(fs, max(reg1, reg2))
	p.freeReg(fs, min(reg1, reg2))
}

func (p *parser) expToConst(e expDesc) (_ Value, ok bool) {
	if e.hasJumps() {
		return Value{}, false
	}
	if e.kind == expKindConst {
		return p.constToValue(e)
	}
	return e.toValue()
}

func (p *parser) stringToConstantTable(fs *funcState, e expDesc) expDesc {
	s, ok := e.stringConstant()
	if !ok {
		panic("stringToConstant must be called on expKindKStr")
	}
	k := fs.addConstant(StringValue(s))
	return newConstExpDesc(k).withJumpLists(e)
}

func (p *parser) constToValue(e expDesc) (_ Value, ok bool) {
	if e.kind != expKindK {
		return Value{}, false
	}
	return p.activeVariables[e.constIndex()].k, true
}

// setTableSize returns a sequence of instructions for [OpSetTable].
func setTableSize(ra registerIndex, arraySize, hashSize int) [2]Instruction {
	var rb uint8
	if hashSize != 0 {
		// TODO(now): ceillog2
	}
	extra := uint32(arraySize / (maxArgC + 1))
	rc := uint8(arraySize % (maxArgC + 1))
	return [2]Instruction{
		ABCInstruction(OpNewTable, uint8(ra), rb, rc, extra > 0),
		ExtraArgument(extra),
	}
}
