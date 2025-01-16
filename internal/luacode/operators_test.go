// Copyright (C) 1994-2024 Lua.org, PUC-Rio.
// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import (
	"errors"
	"math"
	"testing"
)

func TestArithmetic(t *testing.T) {
	tests := []struct {
		op   ArithmeticOperator
		p1   Value
		p2   Value
		want Value
		err  error
	}{
		{op: Add, p1: IntegerValue(2), p2: IntegerValue(2), want: IntegerValue(4)},

		{op: Divide, p1: FloatValue(36), p2: FloatValue(4), want: FloatValue(9)},
		{op: Divide, p1: FloatValue(0), p2: FloatValue(0), want: FloatValue(math.NaN())},
		{op: Divide, p1: FloatValue(1), p2: FloatValue(0), want: FloatValue(math.Inf(1))},
		{op: Divide, p1: FloatValue(-1), p2: FloatValue(0), want: FloatValue(math.Inf(-1))},
		{op: Divide, p1: FloatValue(1), p2: FloatValue(math.Inf(-1)), want: FloatValue(math.Copysign(0, -1))},

		{op: IntegerDivide, p1: IntegerValue(-0x8000000000000000), p2: IntegerValue(-1), want: IntegerValue(-0x8000000000000000)},
		{op: IntegerDivide, p1: IntegerValue(-16), p2: IntegerValue(3), want: IntegerValue(-6)},
		{op: IntegerDivide, p1: FloatValue(-16), p2: FloatValue(3), want: FloatValue(-6)},

		{op: Modulo, p1: IntegerValue(-4), p2: IntegerValue(3), want: IntegerValue(2)},
		{op: Modulo, p1: IntegerValue(4), p2: IntegerValue(-3), want: IntegerValue(-2)},
		{op: Modulo, p1: FloatValue(-4), p2: IntegerValue(3), want: FloatValue(2)},
		{op: Modulo, p1: IntegerValue(4), p2: FloatValue(-3), want: FloatValue(-2)},
		{op: Modulo, p1: IntegerValue(4), p2: IntegerValue(-5), want: IntegerValue(-1)},
		{op: Modulo, p1: IntegerValue(4), p2: FloatValue(-5), want: FloatValue(-1)},
		{op: Modulo, p1: IntegerValue(4), p2: IntegerValue(5), want: IntegerValue(4)},
		{op: Modulo, p1: IntegerValue(4), p2: FloatValue(5), want: FloatValue(4)},
		{op: Modulo, p1: IntegerValue(-4), p2: IntegerValue(-5), want: IntegerValue(-4)},
		{op: Modulo, p1: IntegerValue(-4), p2: FloatValue(-5), want: FloatValue(-4)},
		{op: Modulo, p1: IntegerValue(-4), p2: IntegerValue(5), want: IntegerValue(1)},
		{op: Modulo, p1: IntegerValue(-4), p2: FloatValue(5), want: FloatValue(1)},
		{op: Modulo, p1: FloatValue(4.25), p2: IntegerValue(4), want: FloatValue(0.25)},
		{op: Modulo, p1: FloatValue(10.0), p2: IntegerValue(2), want: FloatValue(0)},
		{op: Modulo, p1: FloatValue(-10.0), p2: IntegerValue(2), want: FloatValue(math.Copysign(0, -1))},
		{op: Modulo, p1: FloatValue(-10.0), p2: IntegerValue(-2), want: FloatValue(math.Copysign(0, -1))},
	}

	for _, test := range tests {
		if got, err := Arithmetic(test.op, test.p1, test.p2); !got.IdenticalTo(test.want) || !errors.Is(err, test.err) {
			t.Errorf("Arithmetic(%v, %v, %v) = %v, %v; want %v, %v",
				test.op, test.p1, test.p2, got, err, test.want, test.err)
		}
	}
}

func TestUnaryOperatorToOpCode(t *testing.T) {
	tests := []struct {
		op   unaryOperator
		want OpCode
		ok   bool
	}{
		{unaryOperatorNone, maxOpCode + 1, false},
		{unaryOperatorMinus, OpUNM, true},
		{unaryOperatorBNot, OpBNot, true},
		{unaryOperatorNot, OpNot, true},
		{unaryOperatorLen, OpLen, true},
	}

	for _, test := range tests {
		got, ok := test.op.toOpCode()
		if got != test.want || ok != test.ok {
			t.Errorf("%v.toOpCode() = %v, %t; want %v, %t", test.op, got, ok, test.want, test.ok)
		}
	}

	// Check for exhaustiveness.
	for op := unaryOperator(0); op <= numUnaryOperators; op++ {
		found := false
		for _, test := range tests {
			if test.op == op {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("TestUnaryOperatorToOpCode is missing test for %v", op)
		}
	}
}

func TestBinaryOperatorToOpCode(t *testing.T) {
	type opCodeCase struct {
		op   binaryOperator
		want OpCode
		ok   bool
	}

	tests := []struct {
		base  OpCode
		cases []opCodeCase
	}{
		{
			base: OpAdd,
			cases: []opCodeCase{
				{binaryOperatorNone, maxOpCode + 1, false},

				{binaryOperatorAdd, OpAdd, true},
				{binaryOperatorSub, OpSub, true},
				{binaryOperatorMul, OpMul, true},
				{binaryOperatorMod, OpMod, true},
				{binaryOperatorPow, OpPow, true},
				{binaryOperatorDiv, OpDiv, true},
				{binaryOperatorIDiv, OpIDiv, true},
				{binaryOperatorBAnd, OpBAnd, true},
				{binaryOperatorBOr, OpBOr, true},
				{binaryOperatorBXor, OpBXOR, true},
				{binaryOperatorShiftL, OpSHL, true},
				{binaryOperatorShiftR, OpSHR, true},
				{binaryOperatorConcat, OpConcat, true},

				{binaryOperatorEq, maxOpCode + 1, false},
				{binaryOperatorLT, maxOpCode + 1, false},
				{binaryOperatorLE, maxOpCode + 1, false},
				{binaryOperatorNE, maxOpCode + 1, false},
				{binaryOperatorGT, maxOpCode + 1, false},
				{binaryOperatorGE, maxOpCode + 1, false},
				{binaryOperatorAnd, maxOpCode + 1, false},
				{binaryOperatorOr, maxOpCode + 1, false},
			},
		},
		{
			base: OpAddK,
			cases: []opCodeCase{
				{binaryOperatorNone, maxOpCode + 1, false},

				{binaryOperatorAdd, OpAddK, true},
				{binaryOperatorSub, OpSubK, true},
				{binaryOperatorMul, OpMulK, true},
				{binaryOperatorMod, OpModK, true},
				{binaryOperatorPow, OpPowK, true},
				{binaryOperatorDiv, OpDivK, true},
				{binaryOperatorIDiv, OpIDivK, true},
				{binaryOperatorBAnd, OpBAndK, true},
				{binaryOperatorBOr, OpBOrK, true},
				{binaryOperatorBXor, OpBXORK, true},

				{binaryOperatorShiftL, maxOpCode + 1, false},
				{binaryOperatorShiftR, maxOpCode + 1, false},
				{binaryOperatorConcat, maxOpCode + 1, false},
				{binaryOperatorEq, maxOpCode + 1, false},
				{binaryOperatorLT, maxOpCode + 1, false},
				{binaryOperatorLE, maxOpCode + 1, false},
				{binaryOperatorNE, maxOpCode + 1, false},
				{binaryOperatorGT, maxOpCode + 1, false},
				{binaryOperatorGE, maxOpCode + 1, false},
				{binaryOperatorAnd, maxOpCode + 1, false},
				{binaryOperatorOr, maxOpCode + 1, false},
			},
		},
		{
			base: OpLT,
			cases: []opCodeCase{
				{binaryOperatorNone, maxOpCode + 1, false},
				{binaryOperatorAdd, maxOpCode + 1, false},
				{binaryOperatorSub, maxOpCode + 1, false},
				{binaryOperatorMul, maxOpCode + 1, false},
				{binaryOperatorMod, maxOpCode + 1, false},
				{binaryOperatorPow, maxOpCode + 1, false},
				{binaryOperatorDiv, maxOpCode + 1, false},
				{binaryOperatorIDiv, maxOpCode + 1, false},
				{binaryOperatorBAnd, maxOpCode + 1, false},
				{binaryOperatorBOr, maxOpCode + 1, false},
				{binaryOperatorBXor, maxOpCode + 1, false},
				{binaryOperatorShiftL, maxOpCode + 1, false},
				{binaryOperatorShiftR, maxOpCode + 1, false},
				{binaryOperatorConcat, maxOpCode + 1, false},
				{binaryOperatorEq, maxOpCode + 1, false},

				{binaryOperatorLT, OpLT, true},
				{binaryOperatorLE, OpLE, true},
				{binaryOperatorNE, maxOpCode + 1, false},
				{binaryOperatorGT, maxOpCode + 1, false},
				{binaryOperatorGE, maxOpCode + 1, false},

				{binaryOperatorAnd, maxOpCode + 1, false},
				{binaryOperatorOr, maxOpCode + 1, false},
			},
		},
		{
			base: OpLTI,
			cases: []opCodeCase{
				{binaryOperatorNone, maxOpCode + 1, false},
				{binaryOperatorAdd, maxOpCode + 1, false},
				{binaryOperatorSub, maxOpCode + 1, false},
				{binaryOperatorMul, maxOpCode + 1, false},
				{binaryOperatorMod, maxOpCode + 1, false},
				{binaryOperatorPow, maxOpCode + 1, false},
				{binaryOperatorDiv, maxOpCode + 1, false},
				{binaryOperatorIDiv, maxOpCode + 1, false},
				{binaryOperatorBAnd, maxOpCode + 1, false},
				{binaryOperatorBOr, maxOpCode + 1, false},
				{binaryOperatorBXor, maxOpCode + 1, false},
				{binaryOperatorShiftL, maxOpCode + 1, false},
				{binaryOperatorShiftR, maxOpCode + 1, false},
				{binaryOperatorConcat, maxOpCode + 1, false},
				{binaryOperatorEq, maxOpCode + 1, false},

				{binaryOperatorLT, OpLTI, true},
				{binaryOperatorLE, OpLEI, true},
				{binaryOperatorNE, maxOpCode + 1, false},
				{binaryOperatorGT, OpGTI, true},
				{binaryOperatorGE, OpGEI, true},

				{binaryOperatorAnd, maxOpCode + 1, false},
				{binaryOperatorOr, maxOpCode + 1, false},
			},
		},
	}

	for _, suite := range tests {
		for _, test := range suite.cases {
			got, ok := test.op.toOpCode(suite.base)
			if got != test.want || ok != test.ok {
				t.Errorf("%v.toOpCode(%v) = %v, %t; want %v, %t", test.op, suite.base, got, ok, test.want, test.ok)
			}
		}
	}

	// Check for exhaustiveness.
	for _, suite := range tests {
		for op := binaryOperator(0); op <= numBinaryOperators; op++ {
			found := false
			for _, test := range suite.cases {
				if test.op == op {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("TestBinaryOperatorToOpCode is missing test for %v in %v", op, suite.base)
			}
		}
	}
}

func TestBinaryOperatorToArithmetic(t *testing.T) {
	tests := []struct {
		op   binaryOperator
		want ArithmeticOperator
		ok   bool
	}{
		{binaryOperatorNone, 0, false},

		{binaryOperatorAdd, Add, true},
		{binaryOperatorSub, Subtract, true},
		{binaryOperatorMul, Multiply, true},
		{binaryOperatorMod, Modulo, true},
		{binaryOperatorPow, Power, true},
		{binaryOperatorDiv, Divide, true},
		{binaryOperatorIDiv, IntegerDivide, true},
		{binaryOperatorBAnd, BitwiseAnd, true},
		{binaryOperatorBOr, BitwiseOr, true},
		{binaryOperatorBXor, BitwiseXOR, true},
		{binaryOperatorShiftL, ShiftLeft, true},
		{binaryOperatorShiftR, ShiftRight, true},

		{binaryOperatorConcat, 0, false},
		{binaryOperatorEq, 0, false},
		{binaryOperatorLT, 0, false},
		{binaryOperatorLE, 0, false},
		{binaryOperatorNE, 0, false},
		{binaryOperatorGT, 0, false},
		{binaryOperatorGE, 0, false},
		{binaryOperatorAnd, 0, false},
		{binaryOperatorOr, 0, false},
	}

	for _, test := range tests {
		got, ok := test.op.toArithmetic()
		if got != test.want || ok != test.ok {
			t.Errorf("%v.toArithmetic() = %v, %t; want %v, %t", test.op, got, ok, test.want, test.ok)
		}
	}

	// Check for exhaustiveness.
	for op := binaryOperator(0); op <= numBinaryOperators; op++ {
		found := false
		for _, test := range tests {
			if test.op == op {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("TestBinaryOperatorToArithmetic is missing test for %v", op)
		}
	}
}

func TestBinaryOperatorTagMethod(t *testing.T) {
	tests := []struct {
		op   binaryOperator
		want TagMethod
		ok   bool
	}{
		{binaryOperatorNone, 0, false},

		{binaryOperatorAdd, TagMethodAdd, true},
		{binaryOperatorSub, TagMethodSub, true},
		{binaryOperatorMul, TagMethodMul, true},
		{binaryOperatorMod, TagMethodMod, true},
		{binaryOperatorPow, TagMethodPow, true},
		{binaryOperatorDiv, TagMethodDiv, true},
		{binaryOperatorIDiv, TagMethodIDiv, true},
		{binaryOperatorBAnd, TagMethodBAnd, true},
		{binaryOperatorBOr, TagMethodBOr, true},
		{binaryOperatorBXor, TagMethodBXOR, true},
		{binaryOperatorShiftL, TagMethodSHL, true},
		{binaryOperatorShiftR, TagMethodSHR, true},
		{binaryOperatorConcat, TagMethodConcat, true},

		{binaryOperatorEq, 0, false},
		{binaryOperatorLT, 0, false},
		{binaryOperatorLE, 0, false},
		{binaryOperatorNE, 0, false},
		{binaryOperatorGT, 0, false},
		{binaryOperatorGE, 0, false},
		{binaryOperatorAnd, 0, false},
		{binaryOperatorOr, 0, false},
	}

	for _, test := range tests {
		got, ok := test.op.tagMethod()
		if got != test.want || ok != test.ok {
			t.Errorf("%v.tagMethod() = %v, %t; want %v, %t", test.op, got, ok, test.want, test.ok)
		}
	}

	// Check for exhaustiveness.
	for op := binaryOperator(0); op <= numBinaryOperators; op++ {
		found := false
		for _, test := range tests {
			if test.op == op {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("TestBinaryOperatorTagMethod is missing test for %v", op)
		}
	}
}
