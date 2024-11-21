// Copyright (C) 1994-2024 Lua.org, PUC-Rio.
// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import "math"

// expDesc describes the location of the result of an expression.
type expDesc struct {
	kind expKind
	// bits is interpreted based on kind.
	bits uint64
	// strval stores the argument of [codeString].
	strval string

	// t is a patch list of "exit when true".
	t int
	// f is a patch list of "exit when false".
	f int
}

func newExpDesc(kind expKind) expDesc {
	return expDesc{
		kind: kind,
		t:    noJump,
		f:    noJump,
	}
}

func voidExpDesc() expDesc {
	return newExpDesc(expKindVoid)
}

func codeString(s string) expDesc {
	e := newExpDesc(expKindKStr)
	e.strval = s
	return e
}

// newConstExpDesc returns an [expDesc] for the k'th constant
// in the [Prototype] Constants table.
func newConstExpDesc(k int) expDesc {
	e := newExpDesc(expKindK)
	e.bits = uint64(k)
	return e
}

// newFloatConstExpDesc returns an [expDesc] for a numerical floating point constant.
func newFloatConstExpDesc(f float64) expDesc {
	e := newExpDesc(expKindKFlt)
	e.bits = math.Float64bits(f)
	return e
}

// newIntConstExpDesc returns an [expDesc] for a numerical integer constant.
func newIntConstExpDesc(i int64) expDesc {
	e := newExpDesc(expKindKInt)
	e.bits = uint64(i)
	return e
}

// newNonRelocExpDesc returns an [expDesc] for a value in a fixed register.
func newNonRelocExpDesc(ridx registerIndex) expDesc {
	e := newExpDesc(expKindNonReloc)
	e.bits = uint64(ridx)
	return e
}

// newLocalExpDesc returns an [expDesc] for a local variable
// given the register index
// and the index in [parser].activeVars relative to [parser].firstLocal.
func newLocalExpDesc(ridx registerIndex, vidx uint16) expDesc {
	e := newExpDesc(expKindLocal)
	e.bits = uint64(ridx) | uint64(vidx)<<8
	return e
}

func newUpvalExpDesc(idx upvalueIndex) expDesc {
	e := newExpDesc(expKindUpval)
	e.bits = uint64(idx)
	return e
}

// newConstLocalExpDesc returns an [expDesc] for a compile-time <const> variable
// given an absolute index in [parser].activeVars.
func newConstLocalExpDesc(i int) expDesc {
	e := newExpDesc(expKindConst)
	e.bits = uint64(i)
	return e
}

func newIndexedExpDesc(table, idx registerIndex) expDesc {
	e := newExpDesc(expKindIndexed)
	e.bits = uint64(idx) | uint64(table)<<16
	return e
}

func newIndexUpExpDesc(table upvalueIndex, constIndex uint16) expDesc {
	e := newExpDesc(expKindIndexUp)
	e.bits = uint64(constIndex) | uint64(table)<<16
	return e
}

func newIndexIExpDesc(table registerIndex, i uint16) expDesc {
	e := newExpDesc(expKindIndexI)
	e.bits = uint64(i) | uint64(table)<<16
	return e
}

func newIndexStrExpDesc(table registerIndex, constIndex uint16) expDesc {
	e := newExpDesc(expKindIndexStr)
	e.bits = uint64(constIndex) | uint64(table)<<16
	return e
}

func newJumpExpDesc(pc int) expDesc {
	e := newExpDesc(expKindJmp)
	e.bits = uint64(pc)
	return e
}

func newRelocExpDesc(pc int) expDesc {
	e := newExpDesc(expKindReloc)
	e.bits = uint64(pc)
	return e
}

func newCallExpDesc(pc int) expDesc {
	e := newExpDesc(expKindCall)
	e.bits = uint64(pc)
	return e
}

func newVarargExpDesc(pc int) expDesc {
	e := newExpDesc(expKindVararg)
	e.bits = uint64(pc)
	return e
}

func constToExp(v Value) expDesc {
	if v.IsNil() {
		return newExpDesc(expKindNil)
	}
	if v.IsString() {
		s, _ := v.Unquoted()
		return codeString(s)
	}
	if v.IsInteger() {
		i, _ := v.Int64(OnlyIntegral)
		return newIntConstExpDesc(i)
	}
	if f, ok := v.Float64(); ok {
		return newFloatConstExpDesc(f)
	}
	if b, ok := v.Bool(); ok {
		if b {
			return newExpDesc(expKindTrue)
		} else {
			return newExpDesc(expKindFalse)
		}
	}
	panic("unhandled Value type")
}

func (e expDesc) hasJumps() bool {
	return e.t != e.f
}

func (e expDesc) withJumpLists(from expDesc) expDesc {
	e.t = from.t
	e.f = from.f
	return e
}

// toValue returns the argument passed to
// [newFloatConstExpDesc], [newIntConstExpDesc], or [codeString]
// as a [Value].
// It also supports values from [newExpDesc]
// with kinds [expKindNil], [expKindFalse], or [expKindTrue].
func (e expDesc) toValue() (_ Value, ok bool) {
	if e.hasJumps() {
		return Value{}, false
	}
	switch e.kind {
	case expKindNil:
		return Value{}, true
	case expKindFalse:
		return BoolValue(false), true
	case expKindTrue:
		return BoolValue(true), true
	case expKindKInt:
		i, _ := e.intConstant()
		return IntegerValue(i), true
	case expKindKFlt:
		f, _ := e.floatConstant()
		return FloatValue(f), true
	case expKindKStr:
		return StringValue(e.strval), true
	default:
		return Value{}, false
	}
}

// isNumeral reports whether e
// was created from [newFloatConstExpDesc] or [newIntConstExpDesc]
// and does not have jumps.
func (e expDesc) isNumeral() bool {
	return !e.hasJumps() && e.kind == expKindKInt || e.kind == expKindKFlt
}

// toNumeral returns the argument passed to
// [newFloatConstExpDesc] or [newIntConstExpDesc]
// as a [Value],
// as long as the expression does not have jumps.
func (e expDesc) toNumeral() (_ Value, ok bool) {
	if !e.isNumeral() {
		return Value{}, false
	}
	return e.toValue()
}

// toSignedArg converts a numeral (see [expDesc.isNumeral])
// into a signed argument (see [ToSignedArg]), if possible.
func (e expDesc) toSignedArg() (arg uint8, isFloat bool, ok bool) {
	var i int64
	switch e.kind {
	case expKindKInt:
		i, _ = e.intConstant()
	case expKindKFlt:
		f, _ := e.floatConstant()
		i, ok = FloatToInteger(f, OnlyIntegral)
		if !ok {
			return 0, true, false
		}
		isFloat = true
	default:
		return 0, false, false
	}

	if e.hasJumps() {
		return 0, isFloat, false
	}
	arg, ok = ToSignedArg(i)
	return arg, isFloat, ok
}

// floatConstant returns the argument passed to [newFloatConstExpDesc].
func (e expDesc) floatConstant() (_ float64, ok bool) {
	if e.kind != expKindKFlt {
		return 0, false
	}
	return math.Float64frombits(e.bits), true
}

// intConstant returns the argument passed to [newIntConstExpDesc].
func (e expDesc) intConstant() (_ int64, ok bool) {
	if e.kind != expKindKInt {
		return 0, false
	}
	return int64(e.bits), true
}

// stringConstant returns the argument passed to [codeString].
func (e expDesc) stringConstant() (_ string, ok bool) {
	if e.kind != expKindKStr {
		return "", false
	}
	return e.strval, true
}

// constIndex returns the index in the [Prototype] Constants table.
// For [expKindIndexUp] or [expKindIndexStr],
// constIndex returns the table index constant.
func (e expDesc) constIndex() int {
	switch e.kind {
	case expKindK:
		return int(e.bits)
	case expKindIndexUp, expKindIndexStr:
		return int(e.bits & 0xffff)
	default:
		panic("constIndex not supported on expression")
	}
}

func (e expDesc) register() registerIndex {
	switch e.kind {
	case expKindNonReloc, expKindLocal:
		return registerIndex(e.bits & 0xff)
	default:
		panic("register not supported on expression")
	}
}

// localIndex returns the index in the [parser] activeVars slice
// for a [newLocalExpDesc].
func (e expDesc) localIndex(firstLocal int) int {
	if e.kind != expKindLocal {
		panic("localIndex on non-local expression")
	}
	return firstLocal + int(e.bits>>8&0xffff)
}

// upvalueIndex returns the upvalue index passed to [newUpvalExpDesc].
func (e expDesc) upvalueIndex() upvalueIndex {
	if e.kind != expKindUpval {
		panic("upvalueIndex on non-upvalue expression")
	}
	return upvalueIndex(e.bits)
}

// constLocalIndex returns the absolute index in the [parser] activeVars slice
// for a [newConstLocalExpDesc].
func (e expDesc) constLocalIndex() int {
	if e.kind != expKindConst {
		panic("constLocalIndex on non-<const> expression")
	}
	return int(e.bits)
}

// tableRegister returns the register holding the table in an index expression.
func (e expDesc) tableRegister() registerIndex {
	switch e.kind {
	case expKindIndexed, expKindIndexI, expKindIndexStr:
		return registerIndex(e.bits >> 16)
	default:
		panic("tableRegister on non-index expression")
	}
}

// tableUpvalue returns the table's upvalue index of the [expKindIndexUp] expression.
func (e expDesc) tableUpvalue() upvalueIndex {
	if e.kind != expKindIndexUp {
		panic("tableUpvalue on non-upvalue-index expression")
	}
	return upvalueIndex(e.bits >> 16)
}

// indexRegister returns the table index register of the [expKindIndexed] expression.
func (e expDesc) indexRegister() registerIndex {
	if e.kind != expKindIndexed {
		panic("indexRegister on non-index expression")
	}
	return registerIndex(e.bits)
}

// indexInt returns the constant integer of the [expKindIndexI] expression.
func (e expDesc) indexInt() int64 {
	if e.kind != expKindIndexI {
		panic("indexInt on non-index expression")
	}
	return int64(e.bits)
}

// pc returns the index of the expression's instruction
// in the [Prototype] Code slice.
func (e expDesc) pc() int {
	switch e.kind {
	case expKindJmp, expKindReloc, expKindCall, expKindVararg:
		return int(e.bits)
	default:
		panic("pc not supported on expression")
	}
}

type expKind int

const (
	// when 'expdesc' describes the last expression of a list,
	// this kind means an empty list (so, no expression)
	expKindVoid expKind = iota
	// constant nil
	expKindNil
	// constant true
	expKindTrue
	// constant false
	expKindFalse
	// constant in 'k'; info = index of constant in 'k'
	expKindK
	// floating constant; nval = numerical float value
	expKindKFlt
	// integer constant; ival = numerical integer value
	expKindKInt
	// string constant; strval = TString address;
	// (string is fixed by the lexer)
	expKindKStr
	// expression has its value in a fixed register;
	// info = result register
	expKindNonReloc
	// local variable; var.ridx = register index;
	// var.vidx = relative index in 'actvar.arr'
	expKindLocal
	// upvalue variable; info = index of upvalue in 'upvalues'
	expKindUpval
	// compile-time <const> variable;
	// info = absolute index in 'actvar.arr'
	// TODO(now): Rename.
	expKindConst
	// indexed variable;
	// ind.t = table register;
	// ind.idx = key's R index
	expKindIndexed
	// indexed upvalue;
	// ind.t = table upvalue;
	// ind.idx = key's K index
	expKindIndexUp
	// indexed variable with constant integer;
	// ind.t = table register;
	// ind.idx = key's value
	expKindIndexI
	// indexed variable with literal string;
	// ind.t = table register;
	// ind.idx = key's K index
	expKindIndexStr
	// expression is a test/comparison;
	// info = pc of corresponding jump instruction
	expKindJmp
	// expression can put result in any register;
	// info = instruction pc
	expKindReloc
	// expression is a function call; info = instruction pc
	expKindCall
	// vararg expression; info = instruction pc
	expKindVararg
)

func (k expKind) isCompileTimeConstant() bool {
	return expKindNil <= k && k <= expKindKStr
}

func (k expKind) isVar() bool {
	return expKindLocal <= k && k <= expKindIndexStr
}

func (k expKind) isIndexed() bool {
	return expKindIndexed <= k && k <= expKindIndexStr
}

func (k expKind) hasMultipleReturns() bool {
	return k == expKindCall || k == expKindVararg
}
