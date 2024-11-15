// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luasyntax

import (
	"slices"
	"strings"
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

	LineInfo LineInfo
	Source   Source
}

func (f *Prototype) addConstant(k Value) int {
	if i := slices.Index(f.Constants, k); i >= 0 {
		return i
	}
	f.Constants = append(f.Constants, k)
	return len(f.Constants) - 1
}

type UpvalueDescriptor struct {
	Name    string
	InStack bool
	Index   uint8
	Kind    VariableKind
}

type VariableKind uint8

const (
	RegularVariable VariableKind = iota
	Constant
	ToClose
	CompileTimeConstant
)

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
