// Copyright (C) 1994-2024 Lua.org, PUC-Rio.
// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import "testing"

func TestUnaryOperatorToOpCode(t *testing.T) {
	tests := []struct {
		op   unaryOperator
		want OpCode
		ok   bool
	}{
		{unaryOperatorNone, numOpCodes, false},
		{unaryOperatorMinus, OpUnM, true},
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
				{binaryOperatorNone, numOpCodes, false},

				{binaryOperatorAdd, OpAdd, true},
				{binaryOperatorSub, OpSub, true},
				{binaryOperatorMul, OpMul, true},
				{binaryOperatorMod, OpMod, true},
				{binaryOperatorPow, OpPow, true},
				{binaryOperatorDiv, OpDiv, true},
				{binaryOperatorIDiv, OpIDiv, true},
				{binaryOperatorBAnd, OpBAnd, true},
				{binaryOperatorBOr, OpBOr, true},
				{binaryOperatorBXor, OpBXor, true},
				{binaryOperatorShiftL, OpShl, true},
				{binaryOperatorShiftR, OpShr, true},
				{binaryOperatorConcat, OpConcat, true},

				{binaryOperatorEq, numOpCodes, false},
				{binaryOperatorLT, numOpCodes, false},
				{binaryOperatorLE, numOpCodes, false},
				{binaryOperatorNE, numOpCodes, false},
				{binaryOperatorGT, numOpCodes, false},
				{binaryOperatorGE, numOpCodes, false},
				{binaryOperatorAnd, numOpCodes, false},
				{binaryOperatorOr, numOpCodes, false},
			},
		},
		{
			base: OpAddK,
			cases: []opCodeCase{
				{binaryOperatorNone, numOpCodes, false},

				{binaryOperatorAdd, OpAddK, true},
				{binaryOperatorSub, OpSubK, true},
				{binaryOperatorMul, OpMulK, true},
				{binaryOperatorMod, OpModK, true},
				{binaryOperatorPow, OpPowK, true},
				{binaryOperatorDiv, OpDivK, true},
				{binaryOperatorIDiv, OpIDivK, true},
				{binaryOperatorBAnd, OpBAndK, true},
				{binaryOperatorBOr, OpBOrK, true},
				{binaryOperatorBXor, OpBXorK, true},

				{binaryOperatorShiftL, numOpCodes, false},
				{binaryOperatorShiftR, numOpCodes, false},
				{binaryOperatorConcat, numOpCodes, false},
				{binaryOperatorEq, numOpCodes, false},
				{binaryOperatorLT, numOpCodes, false},
				{binaryOperatorLE, numOpCodes, false},
				{binaryOperatorNE, numOpCodes, false},
				{binaryOperatorGT, numOpCodes, false},
				{binaryOperatorGE, numOpCodes, false},
				{binaryOperatorAnd, numOpCodes, false},
				{binaryOperatorOr, numOpCodes, false},
			},
		},
		{
			base: OpLT,
			cases: []opCodeCase{
				{binaryOperatorNone, numOpCodes, false},
				{binaryOperatorAdd, numOpCodes, false},
				{binaryOperatorSub, numOpCodes, false},
				{binaryOperatorMul, numOpCodes, false},
				{binaryOperatorMod, numOpCodes, false},
				{binaryOperatorPow, numOpCodes, false},
				{binaryOperatorDiv, numOpCodes, false},
				{binaryOperatorIDiv, numOpCodes, false},
				{binaryOperatorBAnd, numOpCodes, false},
				{binaryOperatorBOr, numOpCodes, false},
				{binaryOperatorBXor, numOpCodes, false},
				{binaryOperatorShiftL, numOpCodes, false},
				{binaryOperatorShiftR, numOpCodes, false},
				{binaryOperatorConcat, numOpCodes, false},
				{binaryOperatorEq, numOpCodes, false},

				{binaryOperatorLT, OpLT, true},
				{binaryOperatorLE, OpLE, true},
				{binaryOperatorNE, numOpCodes, false},
				{binaryOperatorGT, numOpCodes, false},
				{binaryOperatorGE, numOpCodes, false},

				{binaryOperatorAnd, numOpCodes, false},
				{binaryOperatorOr, numOpCodes, false},
			},
		},
		{
			base: OpLTI,
			cases: []opCodeCase{
				{binaryOperatorNone, numOpCodes, false},
				{binaryOperatorAdd, numOpCodes, false},
				{binaryOperatorSub, numOpCodes, false},
				{binaryOperatorMul, numOpCodes, false},
				{binaryOperatorMod, numOpCodes, false},
				{binaryOperatorPow, numOpCodes, false},
				{binaryOperatorDiv, numOpCodes, false},
				{binaryOperatorIDiv, numOpCodes, false},
				{binaryOperatorBAnd, numOpCodes, false},
				{binaryOperatorBOr, numOpCodes, false},
				{binaryOperatorBXor, numOpCodes, false},
				{binaryOperatorShiftL, numOpCodes, false},
				{binaryOperatorShiftR, numOpCodes, false},
				{binaryOperatorConcat, numOpCodes, false},
				{binaryOperatorEq, numOpCodes, false},

				{binaryOperatorLT, OpLTI, true},
				{binaryOperatorLE, OpLEI, true},
				{binaryOperatorNE, numOpCodes, false},
				{binaryOperatorGT, OpGTI, true},
				{binaryOperatorGE, OpGEI, true},

				{binaryOperatorAnd, numOpCodes, false},
				{binaryOperatorOr, numOpCodes, false},
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
		{binaryOperatorBXor, TagMethodBXor, true},
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
