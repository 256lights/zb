// Copyright (C) 1994-2024 Lua.org, PUC-Rio.
// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

//go:generate stringer -type=ArithmeticOperator,unaryOperator,binaryOperator -output=operators_string.go

package luacode

import (
	"errors"
	"fmt"
	"math"
)

// ArithmeticOperator is the subset of Lua operators that operate on numbers.
type ArithmeticOperator int

const (
	Add ArithmeticOperator = 1 + iota
	Subtract
	Multiply
	Modulo
	Power
	Divide
	IntegerDivide
	BitwiseAnd
	BitwiseOr
	BitwiseXOR
	ShiftLeft
	ShiftRight
	UnaryMinus
	BitwiseNot

	numArithmeticOperators = iota
)

func (op ArithmeticOperator) isValid() bool {
	return 0 < op && op <= numArithmeticOperators
}

// IsUnary reports whether the operator only uses one value.
func (op ArithmeticOperator) IsUnary() bool {
	return op == UnaryMinus || op == BitwiseNot
}

// intArithmeticMode is the [FloatToIntegerMode] used for [Arithmetic].
// It is equivalent to upstream's LUA_FLOORN2I.
const intArithmeticMode = OnlyIntegral

// [Arithmetic] errors.
var (
	// ErrDivideByZero is returned by [Arithmetic]
	// when an integer division by zero occurs.
	ErrDivideByZero = errors.New("attempt to divide by zero")
	// ErrNotNumbers is returned by [Arithmetic]
	// when an operand is not a number.
	ErrNotNumber = errors.New("arithmetic on non-number")
	// ErrNotInteger is returned by [Arithmetic]
	// when performing integer-only arithmetic
	// and an operand is a number but not an integer.
	ErrNotInteger = errors.New("number has no integer representation")
)

// Arithmetic performs an arithmetic or bitwise operation.
// If the operator is unary, p1 is used and p2 is ignored.
// Arithmetic may return an error that wraps one of
// [ErrDivideByZero], [ErrNotNumber], or [ErrNotInteger].
func Arithmetic(op ArithmeticOperator, p1, p2 Value) (Value, error) {
	if op.IsUnary() {
		p2 = IntegerValue(0)
	}

	switch op {
	case BitwiseAnd, BitwiseOr, BitwiseXOR, ShiftLeft, ShiftRight, BitwiseNot:
		// Operate only on integers.
		i1, ok := p1.Int64(intArithmeticMode)
		if !ok {
			if p1.IsNumber() {
				return Value{}, ErrNotInteger
			}
			return Value{}, ErrNotNumber
		}
		i2, ok := p2.Int64(intArithmeticMode)
		if !ok {
			if p2.IsNumber() {
				return Value{}, ErrNotInteger
			}
			return Value{}, ErrNotNumber
		}
		result, err := intArithmetic(op, i1, i2)
		if err != nil {
			return Value{}, err
		}
		return IntegerValue(result), nil
	case Divide, Power:
		// Operate only on floats.
		n1, ok := p1.Float64()
		if !ok {
			return Value{}, ErrNotNumber
		}
		n2, ok := p2.Float64()
		if !ok {
			return Value{}, ErrNotNumber
		}
		return FloatValue(floatArithmetic(op, n1, n2)), nil
	default:
		if !op.isValid() {
			return Value{}, fmt.Errorf("invalid operator %v", op)
		}

		if p1.IsInteger() && p2.IsInteger() {
			i1, _ := p1.Int64(intArithmeticMode)
			i2, _ := p2.Int64(intArithmeticMode)
			result, err := intArithmetic(op, i1, i2)
			if err != nil {
				return Value{}, err
			}
			return IntegerValue(result), nil
		}

		n1, ok := p1.Float64()
		if !ok {
			return Value{}, ErrNotNumber
		}
		n2, ok := p2.Float64()
		if !ok {
			return Value{}, ErrNotNumber
		}
		return FloatValue(floatArithmetic(op, n1, n2)), nil
	}
}

func intArithmetic(op ArithmeticOperator, v1, v2 int64) (int64, error) {
	switch op {
	case Add:
		return v1 + v2, nil
	case Subtract:
		return v1 - v2, nil
	case Multiply:
		return v1 * v2, nil
	case Modulo:
		return v1 % v2, nil
	case IntegerDivide:
		if v2 == 0 {
			return 0, ErrDivideByZero
		}
		return v1 / v2, nil
	case BitwiseAnd:
		return v1 & v2, nil
	case BitwiseOr:
		return v1 | v2, nil
	case BitwiseXOR:
		return v1 ^ v2, nil
	case ShiftRight:
		v2 = -v2
		fallthrough
	case ShiftLeft:
		// From Lua manual at https://www.lua.org/manual/5.4/manual.html#3.4.3:
		// "Both right and left shifts fill the vacant bits with zeros.
		// Negative displacements shift to the other direction;
		// displacements with absolute values equal to or higher than the number of bits in an integer
		// result in zero (as all bits are shifted out)."
		//
		// In Go, shifting a signed integer performs an arithmetic shift.
		// Lua is describing a logical shift, so we convert to unsigned.
		// Go will panic if given a negative shift amount,
		// so we flip the operator ourselves.
		if v2 < 0 {
			return int64(uint64(v1) >> -v2), nil
		}
		return int64(uint64(v1) << v2), nil
	case UnaryMinus:
		return -v1, nil
	case BitwiseNot:
		return int64(^uint64(v1)), nil
	default:
		return 0, fmt.Errorf("%v not implemented for integers", op)
	}
}

func floatArithmetic(op ArithmeticOperator, v1, v2 float64) float64 {
	switch op {
	case Add:
		return v1 + v2
	case Subtract:
		return v1 - v2
	case Multiply:
		return v1 * v2
	case Divide:
		return floatDivide(v1, v2)
	case Power:
		if v2 == 2 {
			return v1 * v1
		}
		return math.Pow(v1, v2)
	case IntegerDivide:
		return math.Floor(floatDivide(v1, v2))
	case UnaryMinus:
		return -v1
	case Modulo:
		// TODO(now): Make sure this aligns with Lua's definition.
		return math.Mod(v1, v2)
	default:
		panic("unhandled arithmetic operator")
	}
}

// floatDivide returns the result of v1 divided by v2.
// If v2 is zero, then the result is ±Inf.
func floatDivide(v1, v2 float64) float64 {
	if v2 == 0 {
		// We handle this case ourselves
		// because as per https://go.dev/ref/spec#Floating_point_operators,
		// "whether a run-time panic occurs [on division by zero] is implementation-specific."
		if math.Signbit(v1) != math.Signbit(v2) {
			return math.Inf(-1)
		}
		return math.Inf(1)
	}
	return v1 / v2
}

type unaryOperator int

const (
	unaryOperatorNone unaryOperator = iota
	unaryOperatorMinus
	unaryOperatorBNot
	unaryOperatorNot
	unaryOperatorLen

	numUnaryOperators = iota - 1
)

func (op unaryOperator) toOpCode() (_ OpCode, ok bool) {
	if op <= unaryOperatorNone || op > numUnaryOperators {
		return numOpCodes, false
	}
	return OpUnM + OpCode(op-unaryOperatorMinus), true
}

func (op unaryOperator) toArithmetic() (_ ArithmeticOperator, ok bool) {
	switch op {
	case unaryOperatorMinus:
		return UnaryMinus, true
	case unaryOperatorBNot:
		return BitwiseNot, true
	default:
		return 0, false
	}
}

type binaryOperator int

const (
	binaryOperatorNone binaryOperator = iota

	binaryOperatorAdd
	binaryOperatorSub
	binaryOperatorMul
	binaryOperatorMod
	binaryOperatorPow
	binaryOperatorDiv
	binaryOperatorIDiv

	binaryOperatorBAnd
	binaryOperatorBOr
	binaryOperatorBXor
	binaryOperatorShiftL
	binaryOperatorShiftR

	binaryOperatorConcat

	binaryOperatorEq
	binaryOperatorLT
	binaryOperatorLE
	binaryOperatorNE
	binaryOperatorGT
	binaryOperatorGE

	binaryOperatorAnd
	binaryOperatorOr

	numBinaryOperators = iota - 1
)

func (op binaryOperator) isFoldable() bool {
	return binaryOperatorNone < op && op <= binaryOperatorShiftR
}

// toOpCode translates the operator to an [OpCode].
// base can be one of [OpAdd], [OpAddK], [OpLT], or [OpLTI].
func (op binaryOperator) toOpCode(base OpCode) (_ OpCode, ok bool) {
	switch {
	case base == OpAdd && binaryOperatorAdd <= op && op <= binaryOperatorShiftR:
		return OpAdd + OpCode(op-binaryOperatorAdd), true
	case base == OpAdd && op == binaryOperatorConcat:
		return OpConcat, true
	case base == OpAddK && binaryOperatorAdd <= op && op <= binaryOperatorBXor:
		return OpAddK + OpCode(op-binaryOperatorAdd), true
	case base == OpLT && binaryOperatorLT <= op && op <= binaryOperatorLE:
		return OpLT + OpCode(op-binaryOperatorLT), true
	case base == OpLTI && binaryOperatorLT <= op && op <= binaryOperatorLE:
		return OpLTI + OpCode(op-binaryOperatorLT), true
	case base == OpLTI && binaryOperatorGT <= op && op <= binaryOperatorGE:
		return OpGTI + OpCode(op-binaryOperatorGT), true
	default:
		return numOpCodes, false
	}
}

func (op binaryOperator) toArithmetic() (_ ArithmeticOperator, ok bool) {
	ok = binaryOperatorAdd <= op && op <= binaryOperatorShiftR
	if !ok {
		return 0, false
	}
	return Add + ArithmeticOperator(op-binaryOperatorAdd), true
}

func (op binaryOperator) tagMethod() (_ TagMethod, ok bool) {
	switch {
	case binaryOperatorAdd <= op && op <= binaryOperatorShiftR:
		return TagMethodAdd + TagMethod(op-binaryOperatorAdd), true
	case op == binaryOperatorConcat:
		return TagMethodConcat, true
	default:
		return 0, false
	}
}
