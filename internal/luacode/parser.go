// Copyright (C) 1994-2024 Lua.org, PUC-Rio.
// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"zb.256lights.llc/pkg/internal/lualex"
)

const envName = "_ENV"

// minStackSize is the initial stack size for any function.
// Registers zero and one are always valid.
const minStackSize = 2

// Parse converts a Lua source file into virtual machine bytecode.
func Parse(name Source, r io.ByteScanner) (*Prototype, error) {
	p := &parser{
		ls:       lualex.NewScanner(r),
		lastLine: 1,
	}

	fs, _ := p.openFunction(nil, &Prototype{
		Source:       name,
		MaxStackSize: minStackSize,
		Upvalues: []UpvalueDescriptor{
			{
				Name:    envName,
				InStack: true,
				Index:   0,
				Kind:    RegularVariable,
			},
		},
	})
	// Main function is always declared vararg.
	p.setVarArg(fs, 0)

	p.next()
	if err := p.block(fs); err != nil {
		return nil, err
	}
	if p.curr.Kind != lualex.ErrorToken {
		return nil, syntaxError(name, p.curr, "<eof> expected")
	}
	if p.err != nil && p.err != io.EOF {
		return nil, p.err
	}
	if err := p.closeFunction(fs); err != nil {
		return nil, err
	}

	return fs.f, nil
}

type parser struct {
	ls   *lualex.Scanner
	curr lualex.Token
	err  error
	// lastLine is the line number of the previous token.
	lastLine int

	activeVariables []variableDescription
	pendingGotos    []labelDescription
	labels          []labelDescription
}

func (p *parser) next() {
	if p.err == nil {
		p.lastLine = max(p.curr.Position.Line, 1)
		p.curr, p.err = p.ls.Scan()
	}
}

func (p *parser) openFunction(prev *funcState, f *Prototype) (*funcState, *blockControl) {
	fs := &funcState{
		prev: prev,
		f:    f,

		previousLine: f.LineDefined,
		firstLocal:   len(p.activeVariables),
		firstLabel:   len(p.labels),
	}
	bl := p.enterBlock(fs, false)
	return fs, bl
}

// enterBlock creates a new blockControl.
func (p *parser) enterBlock(fs *funcState, isLoop bool) *blockControl {
	bl := &blockControl{
		isLoop:             isLoop,
		numActiveVariables: fs.numActiveVariables,
		firstLabel:         len(p.labels),
		firstGoto:          len(p.pendingGotos),
		upval:              false,
		insideTBC:          fs.blocks != nil && fs.blocks.insideTBC,
		prev:               fs.blocks,
	}
	fs.blocks = bl
	return bl
}

func (p *parser) closeFunction(fs *funcState) error {
	p.codeReturn(fs, p.numVariablesInStack(fs), 0)
	if err := p.leaveBlock(fs); err != nil {
		return err
	}
	if err := fs.finish(); err != nil {
		return err
	}
	// TODO(maybe): Clip arrays?
	return nil
}

func (p *parser) leaveBlock(fs *funcState) error {
	bl := fs.blocks
	// Get the level outside the block.
	stackLevel := p.registerLevel(fs, int(bl.numActiveVariables))
	// Remove block locals.
	p.removeVariables(fs, int(bl.numActiveVariables))
	hasClose := false
	if bl.isLoop {
		// Has to fix pending breaks.
		var err error
		hasClose, err = p.createLabel(fs, "break", 0, false)
		if err != nil {
			return err
		}
	}
	if !hasClose && bl.prev != nil && bl.upval {
		// Still needs a close.
		p.code(fs, ABCInstruction(OpClose, uint8(stackLevel), 0, 0, false))
	}
	fs.firstFreeRegister = stackLevel
	p.labels = slices.Delete(p.labels, bl.firstLabel, len(p.labels))
	fs.blocks = bl.prev
	if bl.prev != nil {
		// Nested block: updating pending gotos to enclosing block.
		p.moveGotosOut(fs, bl)
	} else if bl.firstGoto < len(p.pendingGotos) {
		// There are still pending gotos.
		gt := p.pendingGotos[bl.firstGoto]
		var msg string
		if gt.name == "break" {
			msg = fmt.Sprintf("break outside loop at %v", gt.position)
		} else {
			msg = fmt.Sprintf("no visible label '%s' for <goto> at %v", gt.name, gt.position)
		}
		return syntaxError(fs.f.Source, lualex.Token{}, msg)
	}
	return nil
}

// moveGotosOut adjusts pending gotos to outer level of a block.
func (p *parser) moveGotosOut(fs *funcState, bl *blockControl) {
	for i := bl.firstGoto; i < len(p.pendingGotos); i++ {
		gt := &p.pendingGotos[i]
		if p.registerLevel(fs, int(gt.numActiveVariables)) > p.registerLevel(fs, int(bl.numActiveVariables)) {
			// If we're leaving a variable scope, the jump may need a close.
			gt.close = gt.close || bl.upval
		}
		gt.numActiveVariables = bl.numActiveVariables
	}
}

// block parses a block production.
//
//	block ::= {stat} [retstat]
func (p *parser) block(fs *funcState) error {
	for !isBlockFollow(p.curr.Kind) && p.curr.Kind != lualex.UntilToken {
		if p.curr.Kind == lualex.ReturnToken {
			return p.statement(fs)
		}
		if err := p.statement(fs); err != nil {
			return err
		}
	}
	return nil
}

func (p *parser) statement(fs *funcState) error {
	switch p.curr.Kind {
	case lualex.SemiToken:
		p.next()
		return nil
	case lualex.ReturnToken:
		p.next()
		return p.retStat(fs)
	default:
		// functioncall | assignment
		return errors.New("TODO(now)")
	}
}

func (p *parser) setVarArg(fs *funcState, numParams uint8) {
	fs.f.IsVararg = true
	p.code(fs, ABCInstruction(OpVarargPrep, numParams, 0, 0, false))
}

// retStat parses a return statement.
// The caller must have consumed the [lualex.ReturnToken].
func (p *parser) retStat(fs *funcState) error {
	first := p.numVariablesInStack(fs)
	nret := 0
	if !isBlockFollow(p.curr.Kind) && p.curr.Kind != lualex.UntilToken && p.curr.Kind != lualex.SemiToken {
		var lastExpr expDesc
		var err error
		nret, lastExpr, err = p.expList(fs)
		if err != nil {
			return err
		}
		switch {
		case lastExpr.kind.hasMultipleReturns():
			if err := p.setReturns(fs, lastExpr, multiReturn); err != nil {
				return err
			}
			if lastExpr.kind == expKindCall && nret == 1 && !fs.blocks.insideTBC {
				// Tail call.
				i := fs.f.Code[lastExpr.pc()]
				if registerIndex(i.ArgA()) != p.numVariablesInStack(fs) {
					return fmt.Errorf("internal error: call-to-tailcall patching failed")
				}
				fs.f.Code[lastExpr.pc()] = ABCInstruction(OpTailCall, i.ArgA(), i.ArgB(), i.ArgC(), i.K())
			}
			nret = multiReturn
		case nret == 1:
			// Can use original slot.
			if _, first, err = p.exp2anyreg(fs, lastExpr); err != nil {
				return err
			}
		default:
			// Values must go to the top of the stack.
			if _, _, err := p.exp2nextReg(fs, lastExpr); err != nil {
				return err
			}
			if got := int(fs.firstFreeRegister) - int(first); got != nret {
				return fmt.Errorf("internal error: retStat did not lay out values on stack correctly")
			}
		}
	}

	p.codeReturn(fs, first, nret)

	// Skip optional semicolon.
	if p.curr.Kind == lualex.SemiToken {
		p.next()
	}
	return nil
}

// expList parses one or more comma-separated expressions.
func (p *parser) expList(fs *funcState) (n int, last expDesc, err error) {
	n = 1
	last, err = p.exp(fs)
	if err != nil {
		return n, voidExpDesc(), err
	}
	for ; p.curr.Kind == lualex.CommaToken; n++ {
		p.next()
		if _, _, err := p.exp2nextReg(fs, last); err != nil {
			return n, voidExpDesc(), err
		}
		last, err = p.exp(fs)
		if err != nil {
			return n, voidExpDesc(), err
		}
	}
	return n, last, nil
}

// exp parses an expression.
func (p *parser) exp(fs *funcState) (expDesc, error) {
	return p.simpleExp(fs)
}

func (p *parser) simpleExp(fs *funcState) (expDesc, error) {
	switch p.curr.Kind {
	case lualex.NumeralToken:
		// TODO(soon): Get the actual algorithm.
		var e expDesc
		if strings.Contains(p.curr.Value, ".") {
			f, err := strconv.ParseFloat(p.curr.Value, 64)
			if err != nil {
				return voidExpDesc(), err
			}
			e = newFloatConstExpDesc(f)
		} else {
			i, err := strconv.ParseInt(p.curr.Value, 0, 64)
			if err != nil {
				return voidExpDesc(), err
			}
			e = newIntConstExpDesc(i)
		}
		p.next()
		return e, nil
	case lualex.StringToken:
		e := codeString(p.curr.Value)
		p.next()
		return e, nil
	case lualex.NilToken:
		p.next()
		return newExpDesc(expKindNil), nil
	case lualex.TrueToken:
		p.next()
		return newExpDesc(expKindTrue), nil
	case lualex.FalseToken:
		p.next()
		return newExpDesc(expKindFalse), nil
	case lualex.VarargToken:
		if !fs.f.IsVararg {
			return voidExpDesc(), errors.New("cannot use '...' outside a vararg function")
		}
		p.next()
		pc := p.code(fs, ABCInstruction(OpVararg, 0, 0, 1, false))
		return newVarargExpDesc(pc), nil
	default:
		return voidExpDesc(), errors.New("TODO(now): constructor, function, suffixedexp")
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
	ridx := p.localVariableDescription(fs, int(vidx)).ridx
	return newLocalExpDesc(ridx, vidx)
}

// removeVariables closes the scope for all variables up to the given level.
func (p *parser) removeVariables(fs *funcState, toLevel int) {
	p.activeVariables = p.activeVariables[:len(p.activeVariables)-(int(fs.numActiveVariables)-toLevel)]
	for int(fs.numActiveVariables) > toLevel {
		fs.numActiveVariables--
		if v := p.localDebugInfo(fs, int(fs.numActiveVariables)); v != nil {
			v.EndPC = len(fs.f.Code)
		}
	}
}

// localDebugInfo returns the debug information for current variable vidx.
func (p *parser) localDebugInfo(fs *funcState, vidx int) *LocalVariable {
	vd := p.localVariableDescription(fs, vidx)
	if vd.kind == CompileTimeConstant {
		// Constants don't have debug information.
		return nil
	}
	return &fs.f.LocalVariables[vd.pidx]
}

// registerLevel converts a compiler index level to its corresponding register.
// It searches for the highest variable below that level
// that is in a register
// and uses its register index ('ridx') plus one.
func (p *parser) registerLevel(fs *funcState, nvar int) registerIndex {
	for nvar > 0 {
		nvar--
		prevVar := p.localVariableDescription(fs, nvar)
		if prevVar.kind != CompileTimeConstant {
			return prevVar.ridx + 1
		}
	}
	return 0
}

// numVariablesInStack returns the number of variables in the register stack
// for the given function.
func (p *parser) numVariablesInStack(fs *funcState) registerIndex {
	return p.registerLevel(fs, int(fs.numActiveVariables))
}

// variableDescription is a description of an active local variable.
type variableDescription struct {
	name string
	kind VariableKind
	// ridx is the register holding the variable.
	ridx registerIndex
	// pidx is the index of the variable in the Prototype's LocalVars slice.
	pidx uint16
	// k is the constant value (if any).
	k Value
}

func (p *parser) localVariableDescription(fs *funcState, i int) *variableDescription {
	return &p.activeVariables[fs.firstLocal+i]
}

// labelDescription is a description of pending goto statements and label statements.
type labelDescription struct {
	name string
	// pc is the position in code.
	pc int
	// position is the source position where the label appeared.
	position lualex.Position
	// numActiveVariables is the number of active variables in that position.
	numActiveVariables uint8
	// close is the goto that escapes upvalues.
	close bool
}

// createLabel create a new label with the given name at the given line.
// last tells whether label is the last non-op statement in its block.
// Solves all pending gotos to this new label
// and adds a close instruction if necessary.
// createLabel returns true if and only if it added a close instruction.
func (p *parser) createLabel(fs *funcState, name string, line int, last bool) (addedClose bool, err error) {
	n := fs.numActiveVariables
	if last {
		n = fs.blocks.numActiveVariables
	}
	p.labels = append(p.labels, labelDescription{
		name:               name,
		position:           lualex.Position{Line: line},
		numActiveVariables: n,
		pc:                 fs.label(),
	})
	needsClose, err := p.solveGotos(fs, &p.labels[len(p.labels)-1])
	if err != nil {
		return false, err
	}
	if !needsClose {
		return false, nil
	}
	p.code(fs, ABCInstruction(OpClose, uint8(p.numVariablesInStack(fs)), 0, 0, false))
	return true, nil
}

// solveGotos solves forward jumps:
// it checks whether new label lb matches any pending gotos in the current block
// and solves them.
// Return true if any of the gotos need to close upvalues.
func (p *parser) solveGotos(fs *funcState, lb *labelDescription) (needsClose bool, err error) {
	for i := fs.blocks.firstGoto; i < len(p.pendingGotos); {
		if p.pendingGotos[i].name != lb.name {
			i++
			continue
		}
		needsClose = needsClose || p.pendingGotos[i].close
		// Will remove the i'th pending goto from the list.
		if err := p.solveGoto(fs, i, lb); err != nil {
			return needsClose, err
		}
	}
	return needsClose, nil
}

// solveGoto solves the pending goto at index g to given label
// and removes it from the list of pending gotos.
// If the pending goto jumps into the scope of some variable, solveGoto returns an error.
func (p *parser) solveGoto(fs *funcState, g int, lb *labelDescription) error {
	gt := &p.pendingGotos[g]
	if gt.numActiveVariables < lb.numActiveVariables {
		// It entered a scope.
		varName := p.localVariableDescription(fs, int(gt.numActiveVariables)).name
		msg := fmt.Sprintf("<goto %s> at line %d jumps into the scope of local '%s'", gt.name, gt.position.Line, varName)
		return syntaxError(fs.f.Source, lualex.Token{}, msg)
	}
	if err := fs.patchList(gt.pc, lb.pc, noRegister, lb.pc); err != nil {
		return syntaxError(fs.f.Source, p.curr, err.Error())
	}
	p.pendingGotos = slices.Delete(p.pendingGotos, g, g+1)
	return nil
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

// isBlockFollow reports whether a token terminates a block.
func isBlockFollow(k lualex.TokenKind) bool {
	return k == lualex.ElseToken ||
		k == lualex.ElseifToken ||
		k == lualex.EndToken ||
		k == lualex.ErrorToken
}
