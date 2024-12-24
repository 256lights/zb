// Copyright (C) 1994-2024 Lua.org, PUC-Rio.
// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// [Value] type constants in dump format.
const (
	valueDumpTypeNil         byte = 0x00
	valueDumpTypeFalse       byte = 0x01
	valueDumpTypeTrue        byte = 0x11
	valueDumpTypeInt         byte = 0x03
	valueDumpTypeFloat       byte = 0x13
	valueDumpTypeShortString byte = 0x04
	valueDumpTypeLongString  byte = 0x14
)

func loadFunction(f *Prototype, r *chunkReader, parentSource Source) error {
	source, hasSource, err := r.readString()
	if err != nil {
		return fmt.Errorf("load function: source: %v", err)
	}
	if !hasSource {
		source = string(parentSource)
	}
	f.Source = Source(source)

	f.LineDefined, err = r.readVarint()
	if err != nil {
		return fmt.Errorf("load function: line defined: %v", err)
	}
	f.LastLineDefined, err = r.readVarint()
	if err != nil {
		return fmt.Errorf("load function: last line defined: %v", err)
	}
	var ok bool
	f.NumParams, ok = r.readByte()
	if !ok {
		return fmt.Errorf("load function: number of parameters: %v", io.ErrUnexpectedEOF)
	}
	f.IsVararg, ok = r.readBool()
	if !ok {
		return fmt.Errorf("load function: is vararg: %v", io.ErrUnexpectedEOF)
	}
	f.MaxStackSize, ok = r.readByte()
	if !ok {
		return fmt.Errorf("load function: max stack size: %v", io.ErrUnexpectedEOF)
	}

	// Code
	n, err := r.readVarint()
	if err != nil {
		return fmt.Errorf("load function: instruction length: %v", err)
	}
	f.Code = make([]Instruction, n)
	for i := range f.Code {
		f.Code[i], ok = r.readInstruction()
		if !ok {
			return fmt.Errorf("load function: instructions: %v", io.ErrUnexpectedEOF)
		}
	}

	// Constants
	n, err = r.readVarint()
	if err != nil {
		return fmt.Errorf("load function: constant table size: %v", err)
	}
	f.Constants = make([]Value, n)
	for i := range f.Constants {
		t, ok := r.readByte()
		if !ok {
			return fmt.Errorf("load function: constant table: %v", io.ErrUnexpectedEOF)
		}
		switch t {
		case valueDumpTypeNil:
			// Already zeroed; nothing to do.
		case valueDumpTypeFalse:
			f.Constants[i] = BoolValue(false)
		case valueDumpTypeTrue:
			f.Constants[i] = BoolValue(true)
		case valueDumpTypeFloat:
			n, ok := r.readNumber()
			if !ok {
				return fmt.Errorf("load function: constant table: %v", io.ErrUnexpectedEOF)
			}
			f.Constants[i] = FloatValue(n)
		case valueDumpTypeInt:
			n, ok := r.readInteger()
			if !ok {
				return fmt.Errorf("load function: constant table: %v", io.ErrUnexpectedEOF)
			}
			f.Constants[i] = IntegerValue(n)
		case valueDumpTypeShortString, valueDumpTypeLongString:
			s, _, err := r.readString()
			if err != nil {
				return fmt.Errorf("load function: constant table [%d]: %v", i, err)
			}
			f.Constants[i] = StringValue(s)
		default:
			return fmt.Errorf("load function: constant table [%d]: unknown type %#02x", i, t)
		}
	}

	// Upvalues
	n, err = r.readVarint()
	if err != nil {
		return fmt.Errorf("load function: upvalues: %v", err)
	}
	f.Upvalues = make([]UpvalueDescriptor, n)
	for i := range f.Upvalues {
		f.Upvalues[i].InStack, ok = r.readBool()
		if !ok {
			return fmt.Errorf("load function: upvalues: %v", io.ErrUnexpectedEOF)
		}
		f.Upvalues[i].Index, ok = r.readByte()
		if !ok {
			return fmt.Errorf("load function: upvalues: %v", io.ErrUnexpectedEOF)
		}

		rawKind, ok := r.readByte()
		if !ok {
			return fmt.Errorf("load function: upvalues: %v", io.ErrUnexpectedEOF)
		}
		kind := VariableKind(rawKind)
		if !kind.isValid() {
			return fmt.Errorf("load function: upvalues [%d]: unknown kind %#02x", i, rawKind)
		}
		f.Upvalues[i].Kind = kind
	}

	// Protos
	n, err = r.readVarint()
	if err != nil {
		return fmt.Errorf("load function: prototypes: %v", err)
	}
	f.Functions = make([]*Prototype, n)
	for i := range f.Functions {
		fi := new(Prototype)
		if err := loadFunction(fi, r, f.Source); err != nil {
			return err
		}
		f.Functions[i] = fi
	}

	// Debug
	f.LineInfo, err = loadLineInfo(r, f.LineDefined)
	if err != nil {
		return fmt.Errorf("load function: %v", err)
	}
	n, err = r.readVarint()
	if err != nil {
		return fmt.Errorf("load function: local variables: %v", err)
	}
	f.LocalVariables = make([]LocalVariable, n)
	for i := range f.LocalVariables {
		f.LocalVariables[i].Name, _, err = r.readString()
		if err != nil {
			return fmt.Errorf("load function: local variables [%d]: name: %v", i, err)
		}
		f.LocalVariables[i].StartPC, err = r.readVarint()
		if err != nil {
			return fmt.Errorf("load function: local variables [%d]: start pc: %v", i, err)
		}
		f.LocalVariables[i].EndPC, err = r.readVarint()
		if err != nil {
			return fmt.Errorf("load function: local variables [%d]: end pc: %v", i, err)
		}
	}
	n, err = r.readVarint()
	if err != nil {
		return fmt.Errorf("load function: upvalue names: %v", err)
	}
	if n != 0 && n != len(f.Upvalues) {
		return fmt.Errorf("load function: upvalue names: length (%d) does not match table (%d)", n, len(f.Upvalues))
	}
	for i := range n {
		f.Upvalues[i].Name, _, err = r.readString()
		if err != nil {
			return fmt.Errorf("load function: upvalue names [%d]: %v", i, err)
		}
	}

	return nil
}

type chunkReader struct {
	s []byte

	byteOrder   binary.ByteOrder
	integerSize int
	numberSize  int
}

func newChunkReader(s []byte) (*chunkReader, error) {
	r := &chunkReader{s: s}
	if !r.literal(Signature) {
		return nil, errors.New("missing signature")
	}
	if version, ok := r.readByte(); !ok {
		return nil, io.ErrUnexpectedEOF
	} else if version != luacVersion {
		return nil, errors.New("version mismatch")
	}
	if format, ok := r.readByte(); !ok {
		return nil, io.ErrUnexpectedEOF
	} else if format != luacFormat {
		return nil, errors.New("format mismatch")
	}
	if !r.literal(luacData) {
		return nil, errors.New("corrupted chunk")
	}

	if instructionSize, ok := r.readByte(); !ok {
		return nil, io.ErrUnexpectedEOF
	} else if instructionSize != 4 {
		return nil, errors.New("instruction size must be 4")
	}

	integerSize, ok := r.readByte()
	if !ok {
		return nil, io.ErrUnexpectedEOF
	}
	if integerSize != 4 && integerSize != 8 {
		return nil, fmt.Errorf("unsupported integer size (%d)", integerSize)
	}
	r.integerSize = int(integerSize)

	numberSize, ok := r.readByte()
	if !ok {
		return nil, io.ErrUnexpectedEOF
	}
	if numberSize != 4 && numberSize != 8 {
		return nil, fmt.Errorf("unsupported float size (%d)", numberSize)
	}
	r.numberSize = int(numberSize)

	// Determine endianness.
	if len(r.s) < r.integerSize {
		return nil, io.ErrUnexpectedEOF
	}
	switch r.integerSize {
	case 4:
		switch {
		case binary.LittleEndian.Uint32(r.s) == luacInt:
			r.byteOrder = binary.LittleEndian
		case binary.BigEndian.Uint32(r.s) == luacInt:
			r.byteOrder = binary.BigEndian
		default:
			return nil, errors.New("integer format mismatch")
		}
	case 8:
		switch {
		case binary.LittleEndian.Uint64(r.s) == luacInt:
			r.byteOrder = binary.LittleEndian
		case binary.BigEndian.Uint64(r.s) == luacInt:
			r.byteOrder = binary.BigEndian
		default:
			return nil, errors.New("integer format mismatch")
		}
	default:
		panic("unreachable")
	}
	r.s = r.s[r.integerSize:]

	// Verify float.
	if n, ok := r.readNumber(); !ok {
		return nil, io.ErrUnexpectedEOF
	} else if n != luacNum {
		return nil, errors.New("float format mismatch")
	}

	return r, nil
}

func (r *chunkReader) readByte() (byte, bool) {
	if len(r.s) == 0 {
		return 0, false
	}
	b := r.s[0]
	r.s = r.s[1:]
	return b, true
}

func (r *chunkReader) readBool() (bool, bool) {
	if len(r.s) == 0 {
		return false, false
	}
	b := r.s[0] != 0
	r.s = r.s[1:]
	return b, true
}

func (r *chunkReader) readInteger() (int64, bool) {
	if len(r.s) < r.integerSize {
		return 0, false
	}
	var i int64
	switch r.integerSize {
	case 4:
		i = int64(int32(r.byteOrder.Uint32(r.s)))
	case 8:
		i = int64(r.byteOrder.Uint64(r.s))
	default:
		return 0, false
	}
	r.s = r.s[r.integerSize:]
	return i, true
}

func (r *chunkReader) readNumber() (float64, bool) {
	if len(r.s) < r.numberSize {
		return 0, false
	}
	var f float64
	switch r.numberSize {
	case 4:
		f = float64(math.Float32frombits(r.byteOrder.Uint32(r.s)))
	case 8:
		f = math.Float64frombits(r.byteOrder.Uint64(r.s))
	default:
		return 0, false
	}
	r.s = r.s[r.numberSize:]
	return f, true
}

func (r *chunkReader) readVarint() (int, error) {
	var x uint64
	for {
		b, ok := r.readByte()
		if !ok {
			return 0, io.ErrUnexpectedEOF
		}
		if x >= math.MaxInt>>7 {
			return 0, errors.New("integer overflow")
		}
		x = (x << 7) | uint64(b&0x7f)
		if b&0x80 != 0 {
			return int(x), nil
		}
	}
}

func (r *chunkReader) readString() (s string, valid bool, err error) {
	n, err := r.readVarint()
	if err != nil {
		return "", false, err
	}
	if n == 0 {
		return "", false, nil
	}
	n--
	if len(r.s) < n {
		return "", false, io.ErrUnexpectedEOF
	}
	s = string(r.s[:n])
	r.s = r.s[n:]
	return s, true, nil
}

func (r *chunkReader) readInstruction() (Instruction, bool) {
	const size = 4
	if len(r.s) < size {
		return 0, false
	}
	i := Instruction(r.byteOrder.Uint32(r.s))
	r.s = r.s[size:]
	return i, true
}

func (r *chunkReader) literal(prefix string) bool {
	if len(r.s) < len(prefix) || string(r.s[:len(prefix)]) != prefix {
		return false
	}
	r.s = r.s[len(prefix):]
	return true
}
