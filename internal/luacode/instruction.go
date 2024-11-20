// Copyright (C) 1994-2024 Lua.org, PUC-Rio.
// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

//go:generate stringer -type=OpCode,OpMode -linecomment -output=instruction_string.go

package luacode

import "fmt"

// Instruction is a single virtual machine instruction.
type Instruction uint32

func ABCInstruction(op OpCode, a, b, c uint8, k bool) Instruction {
	if op.OpMode() != OpModeABC {
		panic("ABCInstruction with invalid OpCode")
	}
	var kflag Instruction
	if k {
		kflag = 1 << posK
	}
	return Instruction(op) |
		Instruction(a)<<posA |
		kflag |
		Instruction(b)<<posB |
		Instruction(c)<<posC
}

func ABxInstruction(op OpCode, a uint8, bx int32) Instruction {
	switch op.OpMode() {
	case OpModeABx:
		if bx < 0 || bx > maxArgBx {
			panic("Bx argument out of range")
		}
		return Instruction(op) |
			Instruction(a)<<posA |
			Instruction(bx)<<posBx
	case OpModeAsBx:
		if !fitsSignedBx(int64(bx)) {
			panic("Bx argument out of range")
		}
		return Instruction(op) |
			Instruction(a)<<posA |
			Instruction(bx+offsetBx)<<posBx
	default:
		panic("ABxInstruction with invalid OpCode")
	}
}

// ExtraArgument returns an [OpExtraArg] [Instruction].
func ExtraArgument(ax uint32) Instruction {
	if ax > maxArgAx {
		panic("ExtraArgument argument out of range")
	}
	return Instruction(OpExtraArg) | Instruction(ax)<<posAx
}

func JInstruction(op OpCode, j int32) Instruction {
	if op.OpMode() != OpModeJ {
		panic("JInstruction with invalid OpCode")
	}
	return Instruction(op) | Instruction(j+offsetJ)<<posJ
}

const sizeOpCode = 7

// OpCode returns the instruction's type.
func (i Instruction) OpCode() OpCode {
	return OpCode(i & (1<<sizeOpCode - 1))
}

const (
	sizeA   = 8
	maxArgA = 1<<sizeA - 1
	posA    = sizeOpCode
)

func (i Instruction) ArgA() uint8 {
	switch i.OpCode().OpMode() {
	case OpModeABC, OpModeABx, OpModeAsBx:
		return uint8(i >> posA)
	default:
		return 0
	}
}

// WithArgA returns a copy of i
// with its A argument changed to the given value.
func (i Instruction) WithArgA(a uint8) (_ Instruction, ok bool) {
	switch i.OpCode().OpMode() {
	case OpModeABC, OpModeABx, OpModeAsBx:
		const mask = maxArgA << posA
		return i&^mask | (Instruction(a) << posA), true
	default:
		return i, false
	}
}

const (
	sizeB   = 8
	maxArgB = 1<<sizeB - 1
	posB    = posK + sizeK
)

func (i Instruction) ArgB() uint8 {
	switch i.OpCode().OpMode() {
	case OpModeABC:
		return uint8(i >> posB)
	default:
		return 0
	}
}

const (
	sizeAx   = 25
	maxArgAx = 1<<sizeAx - 1
	posAx    = sizeOpCode
)

func (i Instruction) ArgAx() uint32 {
	switch i.OpCode().OpMode() {
	case OpModeAx:
		return uint32(i >> posAx)
	default:
		return 0
	}
}

const (
	sizeBx   = 17
	maxArgBx = 1<<sizeBx - 1
	posBx    = posA + sizeA
	offsetBx = maxArgBx >> 1
)

func (i Instruction) ArgBx() int32 {
	switch i.OpCode().OpMode() {
	case OpModeABx:
		return int32(i >> posBx)
	case OpModeAsBx:
		return int32(i>>posBx) - offsetBx
	default:
		return 0
	}
}

// fitsSignedBx reports whether i can be stored in a signed Bx argument.
func fitsSignedBx(i int64) bool {
	return -offsetBx <= i && i <= maxArgBx-offsetBx
}

const (
	sizeC   = 8
	maxArgC = 1<<sizeC - 1
	offsetC = maxArgC >> 1
	posC    = posB + sizeB
)

func (i Instruction) ArgC() uint8 {
	if code := i.OpCode(); code.OpMode() != OpModeABC {
		return 0
	}
	return uint8(i >> posC)
}

// WithArgC returns a copy of i
// with its C argument changed to the given value.
func (i Instruction) WithArgC(c uint8) (_ Instruction, ok bool) {
	if i.OpCode().OpMode() != OpModeABC {
		return i, false
	}
	const mask = maxArgC << posC
	return i&^mask | (Instruction(c) << posC), true
}

// SignedArg converts an [ABCInstruction] argument
// into a signed integer.
func SignedArg(arg uint8) int16 {
	return int16(arg) - offsetC
}

// ToSignedArg converts an integer into a signed [ABCInstruction] argument.
// ok is true if and only if the integer is within the range.
func ToSignedArg(i int64) (_ uint8, ok bool) {
	if !fitsSignedArg(i) {
		return 0, false
	}
	return uint8(i + offsetC), true
}

func fitsSignedArg(i int64) bool {
	return -offsetC <= i && i <= maxArgC-offsetC
}

const (
	sizeK = 1
	posK  = posA + sizeA
)

func (i Instruction) K() bool {
	return i.OpCode().OpMode() == OpModeABC && i&posK != 0
}

// WithK returns a copy of i
// with its K argument changed to the given value.
func (i Instruction) WithK(k bool) (_ Instruction, ok bool) {
	if i.OpCode().OpMode() != OpModeABC {
		return i, false
	}
	const mask = 1 << posK
	if k {
		return i | mask, true
	} else {
		return i &^ mask, true
	}
}

const (
	maxJArg = 1<<25 - 1
	posJ    = sizeOpCode
	offsetJ = maxJArg >> 1

	noJump = -1
)

func (i Instruction) J() int32 {
	switch i.OpCode().OpMode() {
	case OpModeJ:
		return int32(i>>7) - offsetJ
	default:
		return noJump
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

// String decodes the instruction
// and formats it in a manner similar to [luac] -l.
//
// [luac]: https://www.lua.org/manual/5.4/luac.html
func (i Instruction) String() string {
	switch op := i.OpCode(); op.OpMode() {
	case OpModeABC:
		k := 0
		if i.K() {
			k = 1
		}
		return fmt.Sprintf("%-9s %#02x %#02x %#02x %d", op, i.ArgA(), i.ArgB(), i.ArgC(), k)
	case OpModeABx:
		return fmt.Sprintf("%-9s %#02x %#04x", op, i.ArgA(), i.ArgBx())
	case OpModeAsBx:
		return fmt.Sprintf("%-9s %#02x %d", op, i.ArgA(), i.ArgBx())
	case OpModeAx:
		return fmt.Sprintf("%-9s %#07x", op, i.ArgAx())
	case OpModeJ:
		return fmt.Sprintf("%-9s %+d", op, i.J())
	default:
		return fmt.Sprintf("Instruction(%#08x)", uint32(i))
	}
}

// OpCode is an enumeration of [Instruction] types.
type OpCode uint8

// IsValid reports whether the opcode is one of the known instructions.
func (op OpCode) IsValid() bool {
	return op <= maxOpCode
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
	// A B R[A] := R[B]
	OpMove OpCode = 0 // MOVE
	// A sBx R[A] := sBx
	OpLoadI OpCode = 1 // LOADI
	// A sBx R[A] := (lua_Number)sBx
	OpLoadF OpCode = 2 // LOADF
	// A Bx R[A] := K[Bx]
	OpLoadK OpCode = 3 // LOADK
	// A R[A] := K[extra arg]
	OpLoadKX OpCode = 4 // LOADKX
	// A R[A] := false
	OpLoadFalse OpCode = 5 // LOADFALSE
	// A R[A] := false; pc++ (
	OpLFalseSkip OpCode = 6 // LFALSESKIP
	// A R[A] := true
	OpLoadTrue OpCode = 7 // LOADTRUE
	// A B R[A], R[A+1], ..., R[A+B] := nil
	OpLoadNil OpCode = 8 // LOADNIL
	// A B R[A] := UpValue[B]
	OpGetUpval OpCode = 9 // GETUPVAL
	// A B UpValue[B] := R[A]
	OpSetUpval OpCode = 10 // SETUPVAL

	// A B C R[A] := UpValue[B][K[C]:string]
	OpGetTabUp OpCode = 11 // GETTABUP
	// A B C R[A] := R[B][R[C]]
	OpGetTable OpCode = 12 // GETTABLE
	// A B C R[A] := R[B][C]
	OpGetI OpCode = 13 // GETI
	// A B C R[A] := R[B][K[C]:string]
	OpGetField OpCode = 14 // GETFIELD

	// A B C UpValue[A][K[B]:string] := RK(C)
	OpSetTabUp OpCode = 15 // SETTABUP
	// A B C R[A][R[B]] := RK(C)
	OpSetTable OpCode = 16 // SETTABLE
	// A B C R[A][B] := RK(C)
	OpSetI OpCode = 17 // SETI
	// A B C R[A][K[B]:string] := RK(C)
	OpSetField OpCode = 18 // SETFIELD

	// A B C k R[A] := {}
	OpNewTable OpCode = 19 // NEWTABLE

	// A B C R[A+1] := R[B]; R[A] := R[B][RK(C):string]
	OpSelf OpCode = 20 // SELF

	// A B sC R[A] := R[B] + sC
	OpAddI OpCode = 21 // ADDI

	// A B C R[A] := R[B] + K[C]:number
	OpAddK OpCode = 22 // ADDK
	// A B C R[A] := R[B] - K[C]:number
	OpSubK OpCode = 23 // SUBK
	// A B C R[A] := R[B]
	OpMulK OpCode = 24 // MULK
	// A B C R[A] := R[B] % K[C]:number
	OpModK OpCode = 25 // MODK
	// A B C R[A] := R[B] ^ K[C]:number
	OpPowK OpCode = 26 // POWK
	// A B C R[A] := R[B] / K[C]:number
	OpDivK OpCode = 27 // DIVK
	// A B C R[A] := R[B] // K[C]:number
	OpIDivK OpCode = 28 // IDIVK

	// A B C R[A] := R[B] & K[C]:integer
	OpBAndK OpCode = 29 // BANDK
	// A B C R[A] := R[B] | K[C]:integer
	OpBOrK OpCode = 30 // BORK
	// A B C R[A] := R[B] ~ K[C]:integer
	OpBXorK OpCode = 31 // BXORK

	// A B sC R[A] := R[B] >> sC
	OpShrI OpCode = 32 // SHRI
	// A B sC R[A] := sC << R[B]
	OpShlI OpCode = 33 // SHLI

	// A B C R[A] := R[B] + R[C]
	OpAdd OpCode = 34 // ADD
	// A B C R[A] := R[B] - R[C]
	OpSub OpCode = 35 // SUB
	// A B C R[A] := R[B]
	OpMul OpCode = 36 // MUL
	// A B C R[A] := R[B] % R[C]
	OpMod OpCode = 37 // MOD
	// A B C R[A] := R[B] ^ R[C]
	OpPow OpCode = 38 // POW
	// A B C R[A] := R[B] / R[C]
	OpDiv OpCode = 39 // DIV
	// A B C R[A] := R[B] // R[C]
	OpIDiv OpCode = 40 // IDIV

	// A B C R[A] := R[B] & R[C]
	OpBAnd OpCode = 41 // BAND
	// A B C R[A] := R[B] | R[C]
	OpBOr OpCode = 42 // BOR
	// A B C R[A] := R[B] ~ R[C]
	OpBXor OpCode = 43 // BXOR
	// A B C R[A] := R[B] << R[C]
	OpShl OpCode = 44 // SHL
	// A B C R[A] := R[B] >> R[C]
	OpShr OpCode = 45 // SHR

	// A B C call C metamethod over R[A] and R[B] (
	OpMMBin OpCode = 46 // MMBIN
	// A sB C k call C metamethod over R[A] and sB
	OpMMBinI OpCode = 47 // MMBINI
	// A B C k  call C metamethod over R[A] and K[B]
	OpMMBinK OpCode = 48 // MMBINK

	// A B R[A] := -R[B]
	OpUnM OpCode = 49 // UNM
	// A B R[A] := ~R[B]
	OpBNot OpCode = 50 // BNOT
	// A B R[A] := not R[B]
	OpNot OpCode = 51 // NOT
	// A B R[A] := #R[B] (length operator)
	OpLen OpCode = 52 // LEN

	// A B R[A] := R[A].. ... ..R[A + B - 1]
	OpConcat OpCode = 53 // CONCAT

	// A close all upvalues >= R[A]
	OpClose OpCode = 54 // CLOSE
	// A mark variable A "to be closed"
	OpTBC OpCode = 55 // TBC
	// sJ pc += sJ
	OpJmp OpCode = 56 // JMP
	// A B k if ((R[A] == R[B]) ~= k) then pc++
	OpEq OpCode = 57 // EQ
	// A B k if ((R[A] <  R[B]) ~= k) then pc++
	OpLT OpCode = 58 // LT
	// A B k if ((R[A] <= R[B]) ~= k) then pc++
	OpLE OpCode = 59 // LE

	// A B k if ((R[A] == K[B]) ~= k) then pc++
	OpEqK OpCode = 60 // EQK
	// A sB k if ((R[A] == sB) ~= k) then pc++
	OpEqI OpCode = 61 // EQI
	// A sB k if ((R[A] < sB) ~= k) then pc++
	OpLTI OpCode = 62 // LTI
	// A sB k if ((R[A] <= sB) ~= k) then pc++
	OpLEI OpCode = 63 // LEI
	// A sB k if ((R[A] > sB) ~= k) then pc++
	OpGTI OpCode = 64 // GTI
	// A sB k if ((R[A] >= sB) ~= k) then pc++
	OpGEI OpCode = 65 // GEI

	// A k if (not R[A] == k) then pc++
	OpTest OpCode = 66 // TEST
	// A B k if (not R[B] == k) then pc++ else R[A] := R[B] (
	OpTestSet OpCode = 67 // TESTSET

	// A B C R[A], ... ,R[A+C-2] := R[A](R[A+1], ... ,R[A+B-1])
	OpCall OpCode = 68 // CALL
	// A B C k return R[A](R[A+1], ... ,R[A+B-1])
	OpTailCall OpCode = 69 // TAILCALL

	// A B C k return R[A], ... ,R[A+B-2] (see note)
	OpReturn OpCode = 70 // RETURN
	//  return
	OpReturn0 OpCode = 71 // RETURN0
	// A return R[A]
	OpReturn1 OpCode = 72 // RETURN1

	// A Bx update counters; if loop continues then pc-=Bx;
	OpForLoop OpCode = 73 // FORLOOP
	// A Bx <check values and prepare counters>; if not to run then pc+=Bx+1;
	OpForPrep OpCode = 74 // FORPREP

	// A Bx create upvalue for R[A + 3]; pc+=Bx
	OpTForPrep OpCode = 75 // TFORPREP
	// A C R[A+4], ... ,R[A+3+C] := R[A](R[A+1], R[A+2]);
	OpTForCall OpCode = 76 // TFORCALL
	// A Bx if R[A+2] ~= nil then { R[A]=R[A+2]; pc -= Bx }
	OpTForLoop OpCode = 77 // TFORLOOP

	// A B C k R[A][C+i] := R[A+i], 1 <= i <= B
	OpSetList OpCode = 78 // SETLIST

	// A Bx R[A] := closure(KPROTO[Bx])
	OpClosure OpCode = 79 // CLOSURE

	// A C R[A], R[A+1], ..., R[A+C-2] = vararg
	OpVararg OpCode = 80 // VARARG

	//A (adjust vararg parameters)
	OpVarargPrep OpCode = 81 // VARARGPREP

	// Ax extra (larger) argument for previous opcode
	OpExtraArg OpCode = 82 // EXTRAARG

	maxOpCode = OpExtraArg
)

var opProps = [...]byte{
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
