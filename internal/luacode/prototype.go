// Copyright (C) 1994-2024 Lua.org, PUC-Rio.
// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"strings"
)

const (
	signature           = "\x1bLua"
	luacVersion byte    = 5*16 + 4
	luacFormat  byte    = 0
	luacData            = "\x19\x93\r\n\x1a\n"
	luacInt     int64   = 0x5678
	luacNum     float64 = 370.5
)

// Prototype represents a parsed function.
type Prototype struct {
	// NumParams is the number of fixed (named) parameters.
	NumParams uint8
	IsVararg  bool
	// MaxStackSize is the number of registers needed by this function.
	MaxStackSize uint8

	Constants []Value
	Code      []Instruction
	Functions []*Prototype
	Upvalues  []UpvalueDescriptor

	// Debug information:

	Source          Source
	LocalVariables  []LocalVariable
	LineInfo        LineInfo
	LineDefined     int
	LastLineDefined int
}

// StripDebug returns a copy of a [Prototype]
// with the debug information removed.
func (f *Prototype) StripDebug() *Prototype {
	f2 := new(Prototype)
	*f2 = *f
	f2.Source = ""
	f2.LineInfo = LineInfo{}
	f2.LocalVariables = nil

	if len(f.Upvalues) > 0 {
		f2.Upvalues = slices.Clone(f.Upvalues)
		for i := range f2.Upvalues {
			f2.Upvalues[i].Name = ""
		}
	}

	if len(f.Functions) > 0 {
		f2.Functions = make([]*Prototype, len(f.Functions))
		for i, p := range f.Functions {
			f2.Functions[i] = p.StripDebug()
		}
	}

	return f2
}

func (f *Prototype) hasUpvalueNames() bool {
	for _, upval := range f.Upvalues {
		if upval.Name != "" {
			return true
		}
	}
	return false
}

func (f *Prototype) addConstant(k Value) int {
	if i := slices.Index(f.Constants, k); i >= 0 {
		return i
	}
	f.Constants = append(f.Constants, k)
	return len(f.Constants) - 1
}

// MarshalBinary marshals the function as a precompiled chunk
// in the same format as [luac].
//
// [luac]: https://www.lua.org/manual/5.4/luac.html
func (f *Prototype) MarshalBinary() ([]byte, error) {
	var buf []byte

	buf = append(buf, signature...)
	buf = append(buf, luacVersion, luacFormat)
	buf = append(buf, luacData...)
	// Size of [Instruction], [int64], and [float64] in bytes.
	buf = append(buf, 4, 8, 8)
	buf = binary.NativeEndian.AppendUint64(buf, uint64(luacInt))
	buf = binary.NativeEndian.AppendUint64(buf, math.Float64bits(luacNum))

	if len(f.Upvalues) > 0xff {
		return nil, fmt.Errorf("dump lua chunk: too many upvalues (%d)", len(f.Upvalues))
	}
	buf = append(buf, byte(len(f.Upvalues)))

	return dumpFunction(buf, f, "")
}

func dumpFunction(buf []byte, f *Prototype, parentSource Source) ([]byte, error) {
	if f.Source == "" || f.Source == parentSource {
		buf = dumpInt(buf, 0)
	} else {
		buf = dumpString(buf, string(f.Source))
	}
	buf = dumpInt(buf, f.LineDefined)
	buf = dumpInt(buf, f.LastLineDefined)
	buf = append(buf, f.NumParams)
	buf = dumpBool(buf, f.IsVararg)
	buf = append(buf, f.MaxStackSize)

	// Code
	buf = dumpInt(buf, len(f.Code))
	for _, code := range f.Code {
		buf = binary.NativeEndian.AppendUint32(buf, uint32(code))
	}

	// Constants
	buf = dumpInt(buf, len(f.Constants))
	for i, value := range f.Constants {
		switch {
		case value.IsNil():
			buf = append(buf, 0x00)
		case value.IsInteger():
			i, _ := value.Int64(OnlyIntegral)
			buf = append(buf, 0x03)
			buf = binary.NativeEndian.AppendUint64(buf, uint64(i))
		case value.IsNumber():
			f, _ := value.Float64()
			buf = append(buf, 0x13)
			buf = binary.NativeEndian.AppendUint64(buf, math.Float64bits(f))
		case value.IsString():
			if value.isShortString() {
				buf = append(buf, 0x04)
			} else {
				buf = append(buf, 0x14)
			}
			s, _ := value.Unquoted()
			buf = dumpString(buf, s)
		default:
			b, isBool := value.Bool()
			if !isBool {
				return nil, fmt.Errorf("dump lua chunk: Constants[%d] cannot be represented", i)
			}
			if b {
				buf = append(buf, 0x01)
			} else {
				buf = append(buf, 0x11)
			}
		}
	}

	// Upvalues
	buf = dumpInt(buf, len(f.Upvalues))
	for _, upval := range f.Upvalues {
		buf = dumpBool(buf, upval.InStack)
		buf = append(buf, upval.Index)
		buf = append(buf, byte(upval.Kind))
	}

	// Protos
	buf = dumpInt(buf, len(f.Functions))
	for _, p := range f.Functions {
		var err error
		buf, err = dumpFunction(buf, p, f.Source)
		if err != nil {
			return nil, err
		}
	}

	// Debug information
	buf = dumpLineInfo(buf, f.LineInfo)
	buf = dumpInt(buf, len(f.LocalVariables))
	for _, v := range f.LocalVariables {
		buf = dumpString(buf, v.Name)
		buf = dumpInt(buf, v.StartPC)
		buf = dumpInt(buf, v.EndPC)
	}
	if !f.hasUpvalueNames() {
		buf = dumpInt(buf, 0)
	} else {
		buf = dumpInt(buf, len(f.Upvalues))
		for _, upval := range f.Upvalues {
			buf = dumpString(buf, upval.Name)
		}
	}

	return buf, nil
}

func dumpString(buf []byte, s string) []byte {
	buf = dumpInt(buf, len(s)+1)
	buf = append(buf, s...)
	buf = append(buf, 0)
	return buf
}

// dumpInt appends an integer to the byte slice
// in big-endian with a variable-length encoding,
// with the most significant bit indicating the end of the integer.
func dumpInt(buf []byte, size int) []byte {
	start := len(buf)
	for {
		buf = append(buf, uint8(size&0x7f))
		size >>= 7
		if size != 0 {
			break
		}
	}
	slices.Reverse(buf[start:])
	buf[len(buf)-1] |= 0x80
	return buf
}

func dumpBool(buf []byte, b bool) []byte {
	if b {
		return append(buf, 1)
	} else {
		return append(buf, 0)
	}
}

type UpvalueDescriptor struct {
	Name    string
	InStack bool
	Index   uint8
	Kind    VariableKind
}

type VariableKind uint8

const (
	RegularVariable     VariableKind = 0
	Constant            VariableKind = 1
	ToClose             VariableKind = 2
	CompileTimeConstant VariableKind = 3
)

// LocalVariable is a description of a local variable in [Prototype]
// used for debug information.
type LocalVariable struct {
	Name string
	// StartPC is the first instruction in the [Prototype.Code] slice
	// where the variable is active.
	StartPC int
	// EndPC is the first instruction in the [Prototype.Code] slice
	// where the variable is dead.
	EndPC int
}

// Source is a description of a chunk that created a [Prototype].
// If a source starts with a '@',
// it means that the function was defined in a file
// where the file name follows the '@'.
// (The file name can be accessed with [Source.Filename].)
// If a source starts with a '=',
// the remainder of its contents describes the source
// in a user-dependent manner.
// (The string can be accessed with [Source.Literal].)
// Otherwise, the function was defined in a string where source is that string.
type Source string

// Filename returns the file name of the chunk
// if the source is a file name.
func (source Source) Filename() (_ string, isFilename bool) {
	if !strings.HasPrefix(string(source), "@") {
		return "", false
	}
	return string(source[1:]), true
}

// TODO(now): Pick better name.
func (source Source) Literal() (string, bool) {
	if !strings.HasPrefix(string(source), "=") {
		return "", false
	}
	return string(source[1:]), true
}

// IsString reports whether the source is the literal chunk string.
func (source Source) IsString() bool {
	return len(source) == 0 || (source[0] != '@' && source[0] != '=')
}

// String formats the source in a concise manner
// suitable for debugging.
func (source Source) String() string {
	const size = 60
	const truncSignifier = "..."

	if s, ok := source.Literal(); ok {
		if len(s) > size {
			return s[:size]
		}
		return s
	}
	if fname, ok := source.Filename(); ok {
		if len(source) > size {
			const n = size - len(truncSignifier)
			return truncSignifier + fname[len(fname)-n:]
		}
		return fname
	}
	const prefix = `[string "`
	const suffix = `"]`
	const stringSize = size - (len(prefix) - len(suffix))
	line, _, multipleLines := strings.Cut(string(source), "\n")
	if !multipleLines && len(line) <= stringSize {
		return prefix + line + suffix
	}
	if len(line)+len(truncSignifier) > stringSize {
		line = line[:stringSize-len(truncSignifier)]
	}
	return prefix + line + truncSignifier + suffix
}

const maxInstructionsWithoutAbsLineInfo = 128

const (
	// lineInfoRelativeLimit is the limit for values in the rel slice
	// of [LineInfo].
	lineInfoRelativeLimit = 1 << 7

	// absMarker is the mark for entries in the rel slice of [LineInfo]
	// that have absolute information in the abs slice.
	absMarker int8 = -lineInfoRelativeLimit
)

type LineInfo struct {
	rel []int8
	abs []absLineInfo
}

type absLineInfo struct {
	pc   int
	line int
}

func dumpLineInfo(buf []byte, info LineInfo) []byte {
	buf = dumpInt(buf, len(info.rel))
	for _, i := range info.rel {
		buf = append(buf, byte(i))
	}
	buf = dumpInt(buf, len(info.abs))
	for _, a := range info.abs {
		buf = dumpInt(buf, a.pc)
		buf = dumpInt(buf, a.line)
	}
	return buf
}

// maxRegisters is the maximum number of registers in a Lua function.
const maxRegisters = 255

type registerIndex uint8

// noRegister is a sentinel for an invalid register.
const noRegister registerIndex = maxRegisters

func (ridx registerIndex) isValid() bool {
	return ridx < maxRegisters
}

// maxUpvalues is the maximum number of upvalues in a closure.
// Value must fit in a VM register.
const maxUpvalues = 255

type upvalueIndex uint8

func (vidx upvalueIndex) isValid() bool {
	return vidx < maxUpvalues
}
