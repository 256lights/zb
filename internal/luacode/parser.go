// Copyright (C) 1994-2024 Lua.org, PUC-Rio.
// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"zb.256lights.llc/pkg/internal/lualex"
)

const envName = "_ENV"

// Parse converts a Lua source file into virtual machine bytecode.
func Parse(name Source, r io.ByteScanner) (*Prototype, error) {
	fs := &funcState{
		f: &Prototype{
			Source: name,
			Upvalues: []UpvalueDescriptor{
				{
					Name:    envName,
					InStack: true,
					Index:   0,
					Kind:    RegularVariable,
				},
			},
		},
	}
	p := &parser{
		ls: lualex.NewScanner(r),
	}
	p.next()
	// TODO(soon): p.block()
	if p.curr.Kind != lualex.ErrorToken {
		return nil, syntaxError(name, p.curr, "<eof> expected")
	}
	if p.err != nil {
		return nil, p.err
	}
	return fs.f, nil
}

type parser struct {
	ls   *lualex.Scanner
	curr lualex.Token
	err  error
	// lastLine is the line number of the previous token.
	lastLine int

	activeVars   []varDesc
	pendingGotos []labelDesc
	labels       []labelDesc
}

func (p *parser) next() {
	if p.err == nil {
		p.lastLine = p.curr.Position.Line
		p.curr, p.err = p.ls.Scan()
	}
}

func (p *parser) name(fs *funcState) (string, error) {
	if p.curr.Kind != lualex.IdentifierToken {
		return "", syntaxError(fs.f.Source, p.curr, "name expected")
	}
	v := p.curr.Value
	p.next()
	return v, nil
}

func (p *parser) checkMatch(fs *funcState, start lualex.Position, open, close lualex.TokenKind) error {
	if p.curr.Kind == close {
		p.next()
		return nil
	}
	var msg string
	if p.curr.Position.Line == start.Line {
		msg = fmt.Sprintf("'%v' expected", close)
	} else {
		msg = fmt.Sprintf("'%v' expected (to close '%v' at %v)", close, open, start)
	}
	return syntaxError(fs.f.Source, p.curr, msg)
}

// newVar returns a new expression representing the vidx'th variable.
func (p *parser) newVar(fs *funcState, vidx uint16) expDesc {
	ridx := p.localVarDesc(fs, int(vidx)).ridx
	return newLocalExpDesc(ridx, vidx)
}

func (p *parser) localVarDesc(fs *funcState, i int) *varDesc {
	return &p.activeVars[fs.firstLocal+i]
}

// regLevel converts a compiler index level to its corresponding register.
// It searches for the highest variable below that level
// that is in a register
// and uses its register index ('ridx') plus one.
func (p *parser) regLevel(fs *funcState, nvar int) int {
	for nvar > 0 {
		nvar--
		prevVar := p.localVarDesc(fs, nvar)
		if prevVar.kind != CompileTimeConstant {
			return int(prevVar.ridx) + 1
		}
	}
	return 0
}

// numVariablesInStack returns the number of variables in the register stack
// for the given function.
func (p *parser) numVariablesInStack(fs *funcState) int {
	return p.regLevel(fs, int(fs.numActiveVariables))
}

// varDesc is a description of an active local variable.
type varDesc struct {
	name string
	kind VariableKind
	// ridx is the register holding the variable.
	ridx registerIndex
	// pidx is the index of the variable in the Prototype's LocalVars slice.
	pidx uint16
	// k is the constant value (if any).
	k Value
}

// labelDesc is a description of pending goto statements and label statements.
type labelDesc struct {
	name string
	// pc is the position in code.
	pc int
	// position is the source position where the label appeared.
	position lualex.Position
	// numActiveVariables is the number of active variables in that position.
	numActiveVariables uint8
	// close is the goto that escapes upvalues.
	close uint8
}

func syntaxError(source Source, token lualex.Token, msg string) error {
	sb := new(strings.Builder)
	if source == "" {
		sb.WriteString("?")
	} else {
		sb.WriteString(source.String())
	}
	if token.Position.IsValid() {
		sb.WriteString(":")
		sb.WriteString(token.Position.String())
	}
	sb.WriteString(": ")
	sb.WriteString(msg)
	if token.Kind != lualex.ErrorToken {
		sb.WriteString(" near ")
		sb.WriteString(token.String())
	}
	return errors.New(sb.String())
}
