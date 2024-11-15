// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luasyntax

// Instruction is a single virtual machine instruction.
type Instruction uint32

func ABCInstruction(op OpCode, a, b, c uint8, k bool) Instruction {
	if op.OpMode() != OpModeABC {
		panic("ABCInstruction with invalid OpCode")
	}
	var kflag Instruction
	if k {
		kflag = 1 << 15
	}
	return Instruction(op) |
		Instruction(a)<<7 |
		kflag |
		Instruction(b)<<16 |
		Instruction(c)<<24
}

func ABxInstruction(op OpCode, a uint8, bx int32) Instruction {
	switch op.OpMode() {
	case OpModeABx:
		return Instruction(op) |
			Instruction(a)<<7 |
			Instruction(bx)<<15
	case OpModeAsBx:
		return Instruction(op) |
			Instruction(a)<<7 |
			Instruction(bx+offsetBx)<<15
	default:
		panic("ABxInstruction with invalid OpCode")
	}
}

func AxInstruction(op OpCode, ax uint32) Instruction {
	if op.OpMode() != OpModeAx {
		panic("AxInstruction with invalid OpCode")
	}
	return Instruction(op) | Instruction(ax)<<7
}

func JInstruction(op OpCode, j int32) Instruction {
	if op.OpMode() != OpModeJ {
		panic("JInstruction with invalid OpCode")
	}
	return Instruction(op) | Instruction(j+offsetJ)<<7
}

// OpCode returns the instruction's type.
func (i Instruction) OpCode() OpCode {
	return OpCode(i & 0x7f)
}

func (i Instruction) ArgA() uint8 {
	switch i.OpCode().OpMode() {
	case OpModeABC, OpModeABx, OpModeAsBx:
		return uint8(i >> 7)
	default:
		return 0
	}
}

func (i Instruction) ArgB() uint8 {
	switch i.OpCode().OpMode() {
	case OpModeABC:
		return uint8(i >> 16)
	default:
		return 0
	}
}

func (i Instruction) ArgAx() uint32 {
	switch i.OpCode().OpMode() {
	case OpModeAx:
		return uint32(i >> 7)
	default:
		return 0
	}
}

const offsetBx = (1<<17 - 1) >> 1

func (i Instruction) ArgBx() int32 {
	switch i.OpCode().OpMode() {
	case OpModeABx:
		return int32(i >> 15)
	case OpModeAsBx:
		return int32(i>>15) - offsetBx
	default:
		return 0
	}
}

const offsetC = (1<<8 - 1) >> 1

func (i Instruction) ArgC() uint8 {
	switch i.OpCode().OpMode() {
	case OpModeABC:
		return uint8(i >> 24)
	default:
		return 0
	}
}

func (i Instruction) K() bool {
	return i.OpCode().OpMode() == OpModeABC && i&(1<<15) != 0
}

const offsetJ = (1<<25 - 1) >> 1

func (i Instruction) J() int32 {
	switch i.OpCode().OpMode() {
	case OpModeJ:
		return int32(i>>7) - offsetJ
	default:
		return 0
	}
}

// IsInTop reports whether the instruction uses the stack top
// from the next instruction.
func (i Instruction) IsInTop() bool {
	op := i.OpCode()
	return op.UsesTopFromPrevious() && i.ArgB() == 0
}

// IsOutTop reports whether the instruction sets the stack top
// for the next instruction.
func (i Instruction) IsOutTop() bool {
	op := i.OpCode()
	return op.SetsTopForNext() && i.ArgC() == 0 || op == OpTailCall
}

// OpCode is an enumeration of [Instruction] types.
type OpCode uint8

// IsValid reports whether the opcode is one of the known instructions.
func (op OpCode) IsValid() bool {
	return op < numOpCodes
}

func (op OpCode) props() byte {
	if !op.IsValid() {
		return 0
	}
	return opProps[op]
}

// OpMode returns the format of an [Instruction] that uses the opcode.
func (op OpCode) OpMode() OpMode {
	return OpMode(op.props() & 7)
}

func (op OpCode) SetsA() bool {
	return op.props()&(1<<3) != 0
}

// IsTest reports whether the instruction is a test.
// In a valid program, the next instruction will be a jump.
func (op OpCode) IsTest() bool {
	return op.props()&(1<<4) != 0
}

// UsesTopFromPrevious reports whether the instruction uses the stack top
// set by the previous instruction (when B == 0).
func (op OpCode) UsesTopFromPrevious() bool {
	return op.props()&(1<<5) != 0
}

// SetsTopForNext reports whether the instruction sets the stack top
// for the next instruction (when C == 0).
func (op OpCode) SetsTopForNext() bool {
	return op.props()&(1<<6) != 0
}

// IsMetamethod reports whether the instruction calls a metamethod.
func (op OpCode) IsMetamethod() bool {
	return op.props()&(1<<7) != 0
}

// Defined [OpCode] values.
const (
	OpMove       OpCode = iota // A B R[A] := R[B]
	OpLoadI                    // A sBx R[A] := sBx
	OpLoadF                    // A sBx R[A] := (lua_Number)sBx
	OpLoadK                    // A Bx R[A] := K[Bx]
	OpLoadKX                   // A R[A] := K[extra arg]
	OpLoadFalse                // A R[A] := false
	OpLFalseSkip               // A R[A] := false; pc++ (
	OpLoadTrue                 // A R[A] := true
	OpLoadNil                  // A B R[A], R[A+1], ..., R[A+B] := nil
	OpGetUpval                 // A B R[A] := UpValue[B]
	OpSetUpval                 // A B UpValue[B] := R[A]

	OpGetTabUp // A B C R[A] := UpValue[B][K[C]:string]
	OpGetTable // A B C R[A] := R[B][R[C]]
	OpGetI     // A B C R[A] := R[B][C]
	OpGetField // A B C R[A] := R[B][K[C]:string]

	OpSetTabUp // A B C UpValue[A][K[B]:string] := RK(C)
	OpSetTable // A B C R[A][R[B]] := RK(C)
	OpSetI     // A B C R[A][B] := RK(C)
	OpSetField // A B C R[A][K[B]:string] := RK(C)

	OpNewTable // A B C k R[A] := {}

	OpSelf // A B C R[A+1] := R[B]; R[A] := R[B][RK(C):string]

	OpAddI // A B sC R[A] := R[B] + sC

	OpAddK  // A B C R[A] := R[B] + K[C]:number
	OpSubK  // A B C R[A] := R[B] - K[C]:number
	OpMulK  // A B C R[A] := R[B]
	OpModK  // A B C R[A] := R[B] % K[C]:number
	OpPowK  // A B C R[A] := R[B] ^ K[C]:number
	OpDivK  // A B C R[A] := R[B] / K[C]:number
	OpIDivK // A B C R[A] := R[B] // K[C]:number

	OpBAndK // A B C R[A] := R[B] & K[C]:integer
	OpBOrK  // A B C R[A] := R[B] | K[C]:integer
	OpBXorK // A B C R[A] := R[B] ~ K[C]:integer

	OpShrI // A B sC R[A] := R[B] >> sC
	OpShlI // A B sC R[A] := sC << R[B]

	OpAdd  // A B C R[A] := R[B] + R[C]
	OpSub  // A B C R[A] := R[B] - R[C]
	OpMul  // A B C R[A] := R[B]
	OpMod  // A B C R[A] := R[B] % R[C]
	OpPow  // A B C R[A] := R[B] ^ R[C]
	OpDiv  // A B C R[A] := R[B] / R[C]
	OpIDiv // A B C R[A] := R[B] // R[C]

	OpBAnd // A B C R[A] := R[B] & R[C]
	OpBOr  // A B C R[A] := R[B] | R[C]
	OpBXor // A B C R[A] := R[B] ~ R[C]
	OpShl  // A B C R[A] := R[B] << R[C]
	OpShr  // A B C R[A] := R[B] >> R[C]

	OpMMBin  // A B C call C metamethod over R[A] and R[B] (
	OpMMBinI // A sB C k call C metamethod over R[A] and sB
	OpMMBinK // A B C k  call C metamethod over R[A] and K[B]

	OpUnM  // A B R[A] := -R[B]
	OpBNot // A B R[A] := ~R[B]
	OpNot  // A B R[A] := not R[B]
	OpLen  // A B R[A] := #R[B] (length operator)

	OpConcat // A B R[A] := R[A].. ... ..R[A + B - 1]

	OpClose // A close all upvalues >= R[A]
	OpTBC   // A mark variable A "to be closed"
	OpJmp   // sJ pc += sJ
	OpEq    // A B k if ((R[A] == R[B]) ~= k) then pc++
	OpLT    // A B k if ((R[A] <  R[B]) ~= k) then pc++
	OpLE    // A B k if ((R[A] <= R[B]) ~= k) then pc++

	OpEqK // A B k if ((R[A] == K[B]) ~= k) then pc++
	OpEqI // A sB k if ((R[A] == sB) ~= k) then pc++
	OpLTI // A sB k if ((R[A] < sB) ~= k) then pc++
	OpLEI // A sB k if ((R[A] <= sB) ~= k) then pc++
	OpGTI // A sB k if ((R[A] > sB) ~= k) then pc++
	OpGEI // A sB k if ((R[A] >= sB) ~= k) then pc++

	OpTest    // A k if (not R[A] == k) then pc++
	OpTestSet // A B k if (not R[B] == k) then pc++ else R[A] := R[B] (

	OpCall     // A B C R[A], ... ,R[A+C-2] := R[A](R[A+1], ... ,R[A+B-1])
	OpTailCall // A B C k return R[A](R[A+1], ... ,R[A+B-1])

	OpReturn  // A B C k return R[A], ... ,R[A+B-2] (see note)
	OpReturn0 //  return
	OpReturn1 // A return R[A]

	OpForLoop // A Bx update counters; if loop continues then pc-=Bx;
	OpForPrep // A Bx <check values and prepare counters>; if not to run then pc+=Bx+1;

	OpTForPrep // A Bx create upvalue for R[A + 3]; pc+=Bx
	OpTForCall // A C R[A+4], ... ,R[A+3+C] := R[A](R[A+1], R[A+2]);
	OpTForLoop // A Bx if R[A+2] ~= nil then { R[A]=R[A+2]; pc -= Bx }

	OpSetList // A B C k R[A][C+i] := R[A+i], 1 <= i <= B

	OpClosure // A Bx R[A] := closure(KPROTO[Bx])

	OpVararg // A C R[A], R[A+1], ..., R[A+C-2] = vararg

	OpVarargPrep //A (adjust vararg parameters)

	OpExtraArg // Ax extra (larger) argument for previous opcode

	numOpCodes = iota
)

var opProps = [numOpCodes]byte{
	OpMove:       0b00001000 | byte(OpModeABC),
	OpLoadI:      0b00001000 | byte(OpModeAsBx),
	OpLoadF:      0b00001000 | byte(OpModeAsBx),
	OpLoadK:      0b00001000 | byte(OpModeABx),
	OpLoadKX:     0b00001000 | byte(OpModeABx),
	OpLoadFalse:  0b00001000 | byte(OpModeABC),
	OpLFalseSkip: 0b00001000 | byte(OpModeABC),
	OpLoadTrue:   0b00001000 | byte(OpModeABC),
	OpLoadNil:    0b00001000 | byte(OpModeABC),
	OpGetUpval:   0b00001000 | byte(OpModeABC),
	OpSetUpval:   0b00000000 | byte(OpModeABC),
	OpGetTabUp:   0b00001000 | byte(OpModeABC),
	OpGetTable:   0b00001000 | byte(OpModeABC),
	OpGetI:       0b00001000 | byte(OpModeABC),
	OpGetField:   0b00001000 | byte(OpModeABC),
	OpSetTabUp:   0b00000000 | byte(OpModeABC),
	OpSetTable:   0b00000000 | byte(OpModeABC),
	OpSetI:       0b00000000 | byte(OpModeABC),
	OpSetField:   0b00000000 | byte(OpModeABC),
	OpNewTable:   0b00001000 | byte(OpModeABC),
	OpSelf:       0b00001000 | byte(OpModeABC),
	OpAddI:       0b00001000 | byte(OpModeABC),
	OpAddK:       0b00001000 | byte(OpModeABC),
	OpSubK:       0b00001000 | byte(OpModeABC),
	OpMulK:       0b00001000 | byte(OpModeABC),
	OpModK:       0b00001000 | byte(OpModeABC),
	OpPowK:       0b00001000 | byte(OpModeABC),
	OpDivK:       0b00001000 | byte(OpModeABC),
	OpIDivK:      0b00001000 | byte(OpModeABC),
	OpBAndK:      0b00001000 | byte(OpModeABC),
	OpBOrK:       0b00001000 | byte(OpModeABC),
	OpBXorK:      0b00001000 | byte(OpModeABC),
	OpShrI:       0b00001000 | byte(OpModeABC),
	OpShlI:       0b00001000 | byte(OpModeABC),
	OpAdd:        0b00001000 | byte(OpModeABC),
	OpSub:        0b00001000 | byte(OpModeABC),
	OpMul:        0b00001000 | byte(OpModeABC),
	OpMod:        0b00001000 | byte(OpModeABC),
	OpPow:        0b00001000 | byte(OpModeABC),
	OpDiv:        0b00001000 | byte(OpModeABC),
	OpIDiv:       0b00001000 | byte(OpModeABC),
	OpBAnd:       0b00001000 | byte(OpModeABC),
	OpBOr:        0b00001000 | byte(OpModeABC),
	OpBXor:       0b00001000 | byte(OpModeABC),
	OpShl:        0b00001000 | byte(OpModeABC),
	OpShr:        0b00001000 | byte(OpModeABC),
	OpMMBin:      0b10000000 | byte(OpModeABC),
	OpMMBinI:     0b10000000 | byte(OpModeABC),
	OpMMBinK:     0b10000000 | byte(OpModeABC),
	OpUnM:        0b00001000 | byte(OpModeABC),
	OpBNot:       0b00001000 | byte(OpModeABC),
	OpNot:        0b00001000 | byte(OpModeABC),
	OpLen:        0b00001000 | byte(OpModeABC),
	OpConcat:     0b00001000 | byte(OpModeABC),
	OpClose:      0b00000000 | byte(OpModeABC),
	OpTBC:        0b00000000 | byte(OpModeABC),
	OpJmp:        0b00000000 | byte(OpModeJ),
	OpEq:         0b00010000 | byte(OpModeABC),
	OpLT:         0b00010000 | byte(OpModeABC),
	OpLE:         0b00010000 | byte(OpModeABC),
	OpEqK:        0b00010000 | byte(OpModeABC),
	OpEqI:        0b00010000 | byte(OpModeABC),
	OpLTI:        0b00010000 | byte(OpModeABC),
	OpLEI:        0b00010000 | byte(OpModeABC),
	OpGTI:        0b00010000 | byte(OpModeABC),
	OpGEI:        0b00010000 | byte(OpModeABC),
	OpTest:       0b00010000 | byte(OpModeABC),
	OpTestSet:    0b00011000 | byte(OpModeABC),
	OpCall:       0b01101000 | byte(OpModeABC),
	OpTailCall:   0b01101000 | byte(OpModeABC),
	OpReturn:     0b00100000 | byte(OpModeABC),
	OpReturn0:    0b00000000 | byte(OpModeABC),
	OpReturn1:    0b00000000 | byte(OpModeABC),
	OpForLoop:    0b00001000 | byte(OpModeABx),
	OpForPrep:    0b00001000 | byte(OpModeABx),
	OpTForPrep:   0b00000000 | byte(OpModeABx),
	OpTForCall:   0b00000000 | byte(OpModeABC),
	OpTForLoop:   0b00001000 | byte(OpModeABx),
	OpSetList:    0b00100000 | byte(OpModeABC),
	OpClosure:    0b00001000 | byte(OpModeABx),
	OpVararg:     0b01001000 | byte(OpModeABC),
	OpVarargPrep: 0b00101000 | byte(OpModeABC),
	OpExtraArg:   0b00000000 | byte(OpModeAx),
}

// OpMode is an enumeration of [Instruction] formats.
type OpMode uint8

// Instruction formats.
const (
	OpModeABC OpMode = 1 + iota
	OpModeABx
	OpModeAsBx
	OpModeAx
	OpModeJ
)
