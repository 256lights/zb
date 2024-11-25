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

// envName is the name of the implicit first upvalue of every main chunk.
//
// Equivalent to `LUA_ENV` in upstream Lua.
const envName = "_ENV"

// depthLimit is the maximum recursion depth for syntax constructs.
//
// Equivalent to `LUAI_MAXCCALLS` in upstream Lua.
const depthLimit = 200

var errDepthExceeded = errors.New("recursion depth exceeded")

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
	p.setVariadic(fs, 0)

	p.advance()
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

	return fs.Prototype, nil
}

// parser is the in-progress state of a [Parse] call.
//
// Somewhat equivalent to `LexState` in upstream Lua,
// but actual lexical analysis is split out.
type parser struct {
	ls   *lualex.Scanner
	curr lualex.Token
	err  error
	next lualex.Token
	// lastLine is the line number of the previous token.
	lastLine int

	depth int

	activeVariables []variableDescription
	pendingGotos    []labelDescription
	labels          []labelDescription
}

// advance scans the next token.
//
// Equivalent to `luaX_next` in upstream Lua.
func (p *parser) advance() {
	if p.next.Kind != lualex.ErrorToken {
		p.lastLine = max(p.curr.Position.Line, 1)
		p.curr = p.next
		p.next = lualex.Token{}
		return
	}

	if p.err == nil {
		p.lastLine = max(p.curr.Position.Line, 1)
		p.curr, p.err = p.ls.Scan()
	}
}

// peek returns the token after the current one
// without advancing the parser.
//
// Equivalent to `luaX_lookahead` in upstream Lua.
func (p *parser) peek() lualex.Token {
	if p.next.Kind == lualex.ErrorToken {
		p.next, p.err = p.ls.Scan()
	}
	return p.next
}

// openFunction creates a new [funcState] and [blockControl]
// for the given function and its parent function.
//
// Equivalent to `open_func` in upstream Lua.
func (p *parser) openFunction(prev *funcState, f *Prototype) (*funcState, *blockControl) {
	fs := &funcState{
		prev:      prev,
		Prototype: f,

		previousLine: f.LineDefined,
		firstLocal:   len(p.activeVariables),
		firstLabel:   len(p.labels),
	}
	bl := p.enterBlock(fs, false)
	return fs, bl
}

// enterBlock creates a new [blockControl].
//
// Equivalent to `enterblock` in upstream Lua.
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

// closeFunction finalizes a [funcState] so that its [Prototype] is usable.
//
// Equivalent to `open_func` in upstream Lua.
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

// leaveBlock finalizes a [blockControl].
//
// Equivalent to `leaveblock` in upstream Lua.
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
		return syntaxError(fs.Source, lualex.Token{}, msg)
	}
	return nil
}

// moveGotosOut adjusts pending gotos to outer level of a block.
//
// Equivalent to `movegotosout` in upstream Lua.
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
//
// Equivalent to `statlist` in upstream Lua.
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

// statement parses a statement.
//
// Equivalent to `statement` in upstream Lua.
func (p *parser) statement(fs *funcState) error {
	p.depth++
	if p.depth > depthLimit {
		return errDepthExceeded
	}
	defer func() {
		p.depth--
	}()

	switch p.curr.Kind {
	case lualex.SemiToken:
		p.advance()
	case lualex.DoToken:
		start := p.curr.Position
		p.advance()
		if err := p.block(fs); err != nil {
			return err
		}
		if err := p.checkMatch(fs, start, lualex.DoToken, lualex.EndToken); err != nil {
			return err
		}
	case lualex.ReturnToken:
		p.advance()
		if err := p.returnStatement(fs); err != nil {
			return err
		}
	default:
		if err := p.exprStatement(fs); err != nil {
			return err
		}
	}

	// Free any temporary registers used in the statement.
	numVariablesInStack := p.numVariablesInStack(fs)
	if fs.firstFreeRegister > registerIndex(fs.MaxStackSize) {
		return fmt.Errorf("internal error: after statement: first free register (%d) is greater than high watermark (%d)",
			fs.firstFreeRegister, fs.MaxStackSize)
	}
	if fs.firstFreeRegister < numVariablesInStack {
		return fmt.Errorf("internal error: after statement: first free register (%d) is less than variable stack (%d)",
			fs.firstFreeRegister, numVariablesInStack)
	}
	fs.firstFreeRegister = numVariablesInStack

	return nil
}

// exprStatement parses a statement that begins with an expression
// (i.e. a function call or an assignment).
//
// Equivalent to `exprstat` in upstream Lua.
func (p *parser) exprStatement(fs *funcState) error {
	v, err := p.prefixExpression(fs)
	if err != nil {
		return err
	}
	switch p.curr.Kind {
	case lualex.AssignToken, lualex.CommaToken:
		return p.assignment(fs, lhsAssign{v: v}, 1)
	default:
		// Function call.
		if v.kind != expressionKindCall {
			return syntaxError(fs.Source, p.curr, "syntax error")
		}
		i := &fs.Code[v.pc()]
		var ok bool
		*i, ok = i.WithArgC(1)
		if !ok {
			return fmt.Errorf("internal error: call expression references %v instruction", i.OpCode())
		}
		return nil
	}
}

type lhsAssign struct {
	prev *lhsAssign
	v    expressionDescriptor
}

// assignment parses an assignment production after its first variable.
//
//	stat ::= varlist '=' explist | /* ... */
//	varlist ::= var {‘,’ var}
//
// Equivalent to `restassign` in upstream Lua.
func (p *parser) assignment(fs *funcState, lhs lhsAssign, numVariables int) error {
	// TODO(now): Check things.
	switch p.curr.Kind {
	case lualex.CommaToken:
		v, err := p.prefixExpression(fs)
		if err != nil {
			return err
		}
		if !v.kind.isIndexed() {
			// TODO(now): Check conflict
		}
		nv := lhsAssign{prev: &lhs, v: v}
		p.depth++
		if p.depth > depthLimit {
			return errDepthExceeded
		}
		err = p.assignment(fs, nv, numVariables+1)
		p.depth--
		if err != nil {
			return err
		}
	case lualex.AssignToken:
		p.advance()
		numExpressions, last, err := p.expressionList(fs)
		if err != nil {
			return err
		}
		if numExpressions == numVariables {
			last = p.setOneReturn(fs, last) // close last expression
			return p.codeStoreVariable(fs, lhs.v, last)
		}
		if err := p.adjustAssignment(fs, numVariables, numExpressions, last); err != nil {
			return err
		}
	default:
		return syntaxError(fs.Source, p.curr, "'=' expected")
	}

	return p.codeStoreVariable(fs, lhs.v, nonRelocatableExpression(fs.firstFreeRegister-1))
}

// adjustAssignment adjusts the number of results from an expression list
// with the given number of expressions
// to yield results for given number of variables.
//
// Equivalent to `adjust_assign` in upstream Lua.
func (p *parser) adjustAssignment(fs *funcState, numVariables, numExpressions int, last expressionDescriptor) error {
	needed := numVariables - numExpressions
	if last.kind.hasMultipleReturns() {
		extra := max(needed+1, 0)
		if err := p.setReturns(fs, last, extra); err != nil {
			return err
		}
	} else {
		if last.kind != expressionKindVoid {
			// Close last expression.
			var err error
			last, _, err = p.toNextRegister(fs, last)
			if err != nil {
				return err
			}
		}
		if needed > 0 {
			// Missing values; fill with nils.
			p.codeNil(fs, fs.firstFreeRegister, uint8(needed))
		}
	}
	if needed > 0 {
		if err := fs.reserveRegisters(needed); err != nil {
			return err
		}
	} else {
		// Remove extra values (this is a subtraction).
		fs.firstFreeRegister += registerIndex(needed)
	}
	return nil
}

// setVariadic marks the function as variadic.
//
// Equivalent to `setvararg` in upstream Lua.
func (p *parser) setVariadic(fs *funcState, numParams uint8) {
	fs.IsVararg = true
	p.code(fs, ABCInstruction(OpVarargPrep, numParams, 0, 0, false))
}

// returnStatement parses a return statement.
// The caller must have consumed the [lualex.ReturnToken].
//
//	retstat ::= return [explist] [‘;’]
//
// Equivalent to `retstat` in upstream Lua.
func (p *parser) returnStatement(fs *funcState) error {
	first := p.numVariablesInStack(fs)
	nret := 0
	if !isBlockFollow(p.curr.Kind) && p.curr.Kind != lualex.UntilToken && p.curr.Kind != lualex.SemiToken {
		var lastExpr expressionDescriptor
		var err error
		nret, lastExpr, err = p.expressionList(fs)
		if err != nil {
			return err
		}
		switch {
		case lastExpr.kind.hasMultipleReturns():
			if err := p.setReturns(fs, lastExpr, multiReturn); err != nil {
				return err
			}
			if lastExpr.kind == expressionKindCall && nret == 1 && !fs.blocks.insideTBC {
				// Tail call.
				i := fs.Code[lastExpr.pc()]
				if registerIndex(i.ArgA()) != p.numVariablesInStack(fs) {
					return fmt.Errorf("internal error: call-to-tailcall patching failed")
				}
				fs.Code[lastExpr.pc()] = ABCInstruction(OpTailCall, i.ArgA(), i.ArgB(), i.ArgC(), i.K())
			}
			nret = multiReturn
		case nret == 1:
			// Can use original slot.
			if _, first, err = p.toAnyRegister(fs, lastExpr); err != nil {
				return err
			}
		default:
			// Values must go to the top of the stack.
			if _, _, err := p.toNextRegister(fs, lastExpr); err != nil {
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
		p.advance()
	}
	return nil
}

// expressionList parses one or more comma-separated expressions.
//
// Equivalent to `explist` in upstream Lua.
func (p *parser) expressionList(fs *funcState) (n int, last expressionDescriptor, err error) {
	n = 1
	last, err = p.expression(fs)
	if err != nil {
		return n, voidExpression(), err
	}
	for ; p.curr.Kind == lualex.CommaToken; n++ {
		p.advance()
		if _, _, err := p.toNextRegister(fs, last); err != nil {
			return n, voidExpression(), err
		}
		last, err = p.expression(fs)
		if err != nil {
			return n, voidExpression(), err
		}
	}
	return n, last, nil
}

// expression parses an expression.
//
// Equivalent to `expr` in upstream Lua.
func (p *parser) expression(fs *funcState) (expressionDescriptor, error) {
	e, _, err := p.subExpression(fs, 0)
	return e, err
}

// subExpression parses expressions joined by binary operators
// where the binary operator's precedence is higher than the given limit.
// If the returned [binaryOperator] is not [binaryOperatorNone],
// then it is the first operator encountered that is lower than or equal to the given limit.
func (p *parser) subExpression(fs *funcState, limit int) (expressionDescriptor, binaryOperator, error) {
	p.depth++
	if p.depth > depthLimit {
		return voidExpression(), binaryOperatorNone, errDepthExceeded
	}
	defer func() {
		p.depth--
	}()

	var e expressionDescriptor
	if uop, ok := toUnaryOperator(p.curr.Kind); ok {
		line := p.curr.Position.Line
		p.advance()
		var err error
		e, _, err = p.subExpression(fs, unaryPrecedence)
		if err != nil {
			return voidExpression(), binaryOperatorNone, err
		}
		e, err = p.codePrefix(fs, uop, e, line)
		if err != nil {
			return voidExpression(), binaryOperatorNone, err
		}
	} else {
		var err error
		e, err = p.simpleExpression(fs)
		if err != nil {
			return voidExpression(), binaryOperatorNone, err
		}
	}

	// Expand while operators have priorities higher than limit.
	op, _ := toBinaryOperator(p.curr.Kind)
	for op != binaryOperatorNone && int(operatorPrecedence[op].left) > limit {
		line := p.curr.Position.Line
		p.advance()
		var err error
		e, err = p.codeInfix(fs, op, e)
		if err != nil {
			return voidExpression(), binaryOperatorNone, err
		}
		// Read sub-expression with higher priority.
		var e2 expressionDescriptor
		var nextOp binaryOperator
		e2, nextOp, err = p.subExpression(fs, int(operatorPrecedence[op].right))
		if err != nil {
			return voidExpression(), binaryOperatorNone, err
		}
		e, err = p.codePostfix(fs, op, e, e2, line)
		if err != nil {
			return voidExpression(), binaryOperatorNone, err
		}
		op = nextOp
	}

	return e, op, nil
}

// prefixExpression parses a prefixexp production.
//
//	prefixexp ::= var | functioncall | ‘(’ exp ‘)’
//	functioncall ::=  prefixexp args | prefixexp ‘:’ Name args
//	var ::=  Name | prefixexp ‘[’ exp ‘]’ | prefixexp ‘.’ Name
//
// Equivalent to `suffixedexp` in upstream Lua.
func (p *parser) prefixExpression(fs *funcState) (expressionDescriptor, error) {
	var v expressionDescriptor
	switch p.curr.Kind {
	case lualex.LParenToken:
		pos := p.curr.Position
		p.advance()
		var err error
		v, err = p.expression(fs)
		if err != nil {
			return voidExpression(), err
		}
		if err := p.checkMatch(fs, pos, lualex.LParenToken, lualex.RParenToken); err != nil {
			return voidExpression(), err
		}
		v = p.dischargeVars(fs, v)
	case lualex.IdentifierToken:
		var err error
		v, err = p.singleVariable(fs)
		if err != nil {
			return voidExpression(), err
		}
	default:
		return voidExpression(), syntaxError(fs.Source, p.curr, "unexpected symbol")
	}

	for {
		switch p.curr.Kind {
		case lualex.DotToken:
			var err error
			v, err = p.fieldSelector(fs, v)
			if err != nil {
				return voidExpression(), err
			}
		case lualex.LBracketToken:
			pos := p.curr.Position
			var err error
			v, err = p.toAnyRegisterOrUpvalue(fs, v)
			if err != nil {
				return voidExpression(), err
			}
			p.advance()
			k, err := p.expression(fs)
			if err != nil {
				return voidExpression(), err
			}
			k, err = p.toValue(fs, k)
			if err != nil {
				return voidExpression(), err
			}
			if err := p.checkMatch(fs, pos, lualex.LBracketToken, lualex.RBracketToken); err != nil {
				return voidExpression(), err
			}
			v, err = p.codeIndexed(fs, v, k)
			if err != nil {
				return voidExpression(), err
			}
		case lualex.ColonToken:
			p.advance()
			key, err := p.name(fs)
			if err != nil {
				return voidExpression(), err
			}
			v, err = p.codeSelf(fs, v, codeString(key))
			if err != nil {
				return voidExpression(), err
			}
			v, err = p.functionArguments(fs, v)
			if err != nil {
				return voidExpression(), err
			}
		case lualex.LParenToken, lualex.StringToken, lualex.LBraceToken:
			var err error
			v, _, err = p.toNextRegister(fs, v)
			if err != nil {
				return voidExpression(), err
			}
			v, err = p.functionArguments(fs, v)
			if err != nil {
				return voidExpression(), err
			}
		default:
			return v, nil
		}
	}
}

// fieldSelector parses a production of:
//
//	'.' NAME | ':' NAME
//
// Equivalent to `fieldsel` in upstream Lua.
func (p *parser) fieldSelector(fs *funcState, v expressionDescriptor) (expressionDescriptor, error) {
	v, err := p.toAnyRegisterOrUpvalue(fs, v)
	if err != nil {
		return voidExpression(), err
	}
	p.advance() // Skip the dot or colon.
	key, err := p.name(fs)
	if err != nil {
		return voidExpression(), err
	}
	return p.codeIndexed(fs, v, codeString(key))
}

// functionArguments parses an args production.
//
//	args ::=  ‘(’ [explist] ‘)’ | tableconstructor | LiteralString
//
// Equivalent to `funcargs` in upstream Lua.
func (p *parser) functionArguments(fs *funcState, f expressionDescriptor) (expressionDescriptor, error) {
	pos := p.curr.Position
	var args expressionDescriptor
	switch p.curr.Kind {
	case lualex.LParenToken:
		p.advance()
		if p.curr.Kind == lualex.RParenToken {
			// Empty argument list.
			args = voidExpression()
		} else {
			var err error
			_, args, err = p.expressionList(fs)
			if err != nil {
				return voidExpression(), err
			}
			if args.kind.hasMultipleReturns() {
				if err := p.setReturns(fs, args, multiReturn); err != nil {
					return voidExpression(), err
				}
			}
		}
		if err := p.checkMatch(fs, pos, lualex.LParenToken, lualex.RParenToken); err != nil {
			return voidExpression(), err
		}
	case lualex.LBraceToken:
		return p.constructor(fs)
	case lualex.StringToken:
		args = codeString(p.curr.Value)
		p.advance()
	default:
		return voidExpression(), syntaxError(fs.Source, p.curr, "function arguments expected")
	}

	baseRegister := f.register()
	var numParams int
	if args.kind.hasMultipleReturns() {
		numParams = multiReturn
	} else {
		if args.kind != expressionKindVoid {
			// Close last argument.
			p.toNextRegister(fs, args)
		}
		numParams = int(fs.firstFreeRegister) - (int(baseRegister) + 1)
	}
	pc := p.code(fs, ABCInstruction(OpCall, uint8(baseRegister), uint8(numParams+1), 2, false))
	fs.fixLineInfo(pos.Line)
	// Call removes function and arguments and leaves one result
	// (unless changed later).
	fs.firstFreeRegister = baseRegister + 1

	return callExpression(pc), nil
}

// constructor parses a "tableconstructor" production.
//
//	tableconstructor ::= ‘{’ [fieldlist] ‘}’
//	fieldlist ::= field {fieldsep field} [fieldsep]
//
// Equivalent to `constructor` in upstream Lua.
func (p *parser) constructor(fs *funcState) (expressionDescriptor, error) {
	start := p.curr.Position
	if p.curr.Kind != lualex.LBraceToken {
		return voidExpression(), syntaxError(fs.Source, p.curr, "'{' expected")
	}

	// Add placeholder instructions for creating the table.
	// We will fill in the instructions later with a call to [setTableSize].
	pc := len(fs.Code)
	for _, i := range newTableInstructions(0, 0, 0) {
		p.code(fs, i)
	}

	tableRegister, err := fs.reserveRegister()
	if err != nil {
		return voidExpression(), err
	}
	tableExpression := nonRelocatableExpression(tableRegister)

	lastListItem := voidExpression()
	arraySize, hashSize, toStore := 0, 0, 0
	p.advance()
	if p.curr.Kind != lualex.RBraceToken {
		for {
			if lastListItem.kind != expressionKindVoid {
				if _, _, err := p.toNextRegister(fs, lastListItem); err != nil {
					return voidExpression(), err
				}
				lastListItem = voidExpression()

				if toStore == fieldsPerFlush {
					if err := p.codeSetList(fs, tableRegister, arraySize, toStore); err != nil {
						return voidExpression(), err
					}
					arraySize += toStore
					toStore = 0
				}
			}

			switch p.curr.Kind {
			case lualex.IdentifierToken:
				// Can either be an expression or a record field.
				if p.peek().Kind == lualex.AssignToken {
					if err := p.recordField(fs, tableExpression); err != nil {
						return voidExpression(), err
					}
					hashSize++
				} else {
					var err error
					lastListItem, err = p.expression(fs)
					if err != nil {
						return voidExpression(), err
					}
					toStore++
				}
			case lualex.LBracketToken:
				if err := p.recordField(fs, tableExpression); err != nil {
					return voidExpression(), err
				}
				hashSize++
			default:
				var err error
				lastListItem, err = p.expression(fs)
				if err != nil {
					return voidExpression(), err
				}
				toStore++
			}

			if p.curr.Kind != lualex.CommaToken && p.curr.Kind != lualex.SemiToken {
				break
			}
			p.advance()
		}
	}
	if err := p.checkMatch(fs, start, lualex.LBraceToken, lualex.RBraceToken); err != nil {
		return voidExpression(), err
	}

	if toStore > 0 {
		if lastListItem.kind.hasMultipleReturns() {
			if err := p.setReturns(fs, lastListItem, multiReturn); err != nil {
				return voidExpression(), err
			}
			if err := p.codeSetList(fs, tableRegister, arraySize, multiReturn); err != nil {
				return voidExpression(), err
			}
			// Do not count last expression (unknown number of elements).
			toStore--
		} else if lastListItem.kind != expressionKindVoid {
			if _, _, err := p.toNextRegister(fs, lastListItem); err != nil {
				return voidExpression(), err
			}
			if err := p.codeSetList(fs, tableRegister, arraySize, toStore); err != nil {
				return voidExpression(), err
			}
		}

		arraySize += toStore
		toStore = 0
	}

	// Go back and fill in the new table instructions.
	ilist := newTableInstructions(tableRegister, arraySize, hashSize)
	copy(fs.Code[pc:], ilist[:])

	return tableExpression, nil
}

// recordField parses a field production.
//
//	field ::= ‘[’ exp ‘]’ ‘=’ exp | Name ‘=’ exp | exp
//
// Roughly equivalent to `recfield` in upstream Lua.
func (p *parser) recordField(fs *funcState, table expressionDescriptor) error {
	// Free temporary registers used.
	defer func(original registerIndex) {
		fs.firstFreeRegister = original
	}(fs.firstFreeRegister)

	var key expressionDescriptor
	switch p.curr.Kind {
	case lualex.IdentifierToken:
		key = codeString(p.curr.Value)
		p.advance()
	case lualex.LBracketToken:
		start := p.curr.Position
		p.advance()
		var err error
		key, err = p.expression(fs)
		if err != nil {
			return err
		}
		key, err = p.toValue(fs, key)
		if err != nil {
			return err
		}
		if err := p.checkMatch(fs, start, lualex.LBracketToken, lualex.RBracketToken); err != nil {
			return err
		}
	default:
		return syntaxError(fs.Source, p.curr, "name or '[' expected")
	}

	if p.curr.Kind != lualex.AssignToken {
		return syntaxError(fs.Source, p.curr, "'=' expected")
	}
	p.advance()

	index, err := p.codeIndexed(fs, table, key)
	if err != nil {
		return err
	}
	value, err := p.expression(fs)
	if err != nil {
		return err
	}
	if err := p.codeStoreVariable(fs, index, value); err != nil {
		return err
	}
	return nil
}

// singleVariable parses an identifier and resolves it as a variable.
//
// Equivalent to `singlevar` in upstream Lua.
func (p *parser) singleVariable(fs *funcState) (expressionDescriptor, error) {
	varname, err := p.name(fs)
	if err != nil {
		return voidExpression(), err
	}
	// Find local variable.
	if v, err := p.resolveName(fs, varname, true); err != nil || v.kind != expressionKindVoid {
		return v, err
	}
	// Global name: rewrite into _ENV access.
	v, err := p.resolveName(fs, envName, true)
	if err != nil {
		return voidExpression(), err
	}
	if v.kind == expressionKindVoid {
		return voidExpression(), fmt.Errorf("internal error: %s does not exist", envName)
	}
	v, err = p.toAnyRegisterOrUpvalue(fs, v)
	if err != nil {
		return voidExpression(), err
	}
	k := codeString(varname)
	return p.codeIndexed(fs, v, k)
}

// resolveName finds the variable with the given name.
// If it is an upvalue, add this upvalue into all intermediate functions.
// If the name could not be found, then the returned expression's kind is [expDescVoid].
//
// Equivalent to `singlevaraux` in upstream Lua.
func (p *parser) resolveName(fs *funcState, name string, base bool) (expressionDescriptor, error) {
	if fs == nil {
		return voidExpression(), nil
	}

	if v, ok := p.searchVariable(fs, name); ok {
		if v.kind == expressionKindLocal && !base {
			// Local will be used as an upvalue.
			fs.markUpvalue(v.localIndex(0))
		}
		return v, nil
	}
	// Not found as local at current level; try upvalues.
	if i, ok := fs.searchUpvalue(name); ok {
		return upvalueExpression(i), nil
	}

	// Not found? Try upper levels.
	v, err := p.resolveName(fs.prev, name, false)
	if err != nil {
		return voidExpression(), err
	}
	switch v.kind {
	case expressionKindLocal:
		if len(fs.Upvalues) >= maxUpvalues {
			return voidExpression(), fmt.Errorf("too many upvalues")
		}
		up := UpvalueDescriptor{
			Name:    name,
			Kind:    p.localVariableDescription(fs.prev, v.localIndex(0)).kind,
			Index:   uint8(v.register()),
			InStack: true,
		}
		fs.Upvalues = append(fs.Upvalues, up)
		return upvalueExpression(upvalueIndex(len(fs.Upvalues) - 1)), nil
	case expressionKindUpvalue:
		if len(fs.Upvalues) >= maxUpvalues {
			return voidExpression(), fmt.Errorf("too many upvalues")
		}
		up := UpvalueDescriptor{
			Name:  name,
			Kind:  fs.prev.Upvalues[v.upvalueIndex()].Kind,
			Index: uint8(v.upvalueIndex()),
		}
		fs.Upvalues = append(fs.Upvalues, up)
		return upvalueExpression(upvalueIndex(len(fs.Upvalues) - 1)), nil
	default:
		return v, nil
	}
}

// simpleExpression parses an expression without operators.
//
// Equivalent to `simpleexp` in upstream Lua.
func (p *parser) simpleExpression(fs *funcState) (expressionDescriptor, error) {
	switch p.curr.Kind {
	case lualex.NumeralToken:
		// TODO(soon): Get the actual algorithm.
		var e expressionDescriptor
		if strings.Contains(p.curr.Value, ".") {
			f, err := strconv.ParseFloat(p.curr.Value, 64)
			if err != nil {
				return voidExpression(), err
			}
			e = floatConstantExpression(f)
		} else {
			i, err := strconv.ParseInt(p.curr.Value, 0, 64)
			if err != nil {
				return voidExpression(), err
			}
			e = intConstantExpression(i)
		}
		p.advance()
		return e, nil
	case lualex.StringToken:
		e := codeString(p.curr.Value)
		p.advance()
		return e, nil
	case lualex.NilToken:
		p.advance()
		return newExpressionDescriptor(expressionKindNil), nil
	case lualex.TrueToken:
		p.advance()
		return newExpressionDescriptor(expressionKindTrue), nil
	case lualex.FalseToken:
		p.advance()
		return newExpressionDescriptor(expressionKindFalse), nil
	case lualex.VarargToken:
		if !fs.IsVararg {
			return voidExpression(), errors.New("cannot use '...' outside a vararg function")
		}
		p.advance()
		pc := p.code(fs, ABCInstruction(OpVararg, 0, 0, 1, false))
		return varargExpression(pc), nil
	case lualex.LBraceToken:
		return p.constructor(fs)
	case lualex.FunctionToken:
		return voidExpression(), errors.New("TODO(soon): function")
	default:
		return p.prefixExpression(fs)
	}
}

// name verifies that the current token is an identifier
// then advances to the next token
// and returns the identifier value.
//
// Equivalent to `str_checkname` in upstream Lua.
func (p *parser) name(fs *funcState) (string, error) {
	if p.curr.Kind != lualex.IdentifierToken {
		return "", syntaxError(fs.Source, p.curr, "name expected")
	}
	v := p.curr.Value
	p.advance()
	return v, nil
}

// checkMatch verifies that the current token is the closing token
// and advances past it.
// If the current token is not the closing token,
// then checkMatch returns an error.
//
// Equivalent to `check_match` in upstream Lua.
func (p *parser) checkMatch(fs *funcState, start lualex.Position, open, close lualex.TokenKind) error {
	if p.curr.Kind == close {
		p.advance()
		return nil
	}
	var msg string
	if p.curr.Position.Line == start.Line {
		msg = fmt.Sprintf("'%v' expected", close)
	} else {
		msg = fmt.Sprintf("'%v' expected (to close '%v' at %v)", close, open, start)
	}
	return syntaxError(fs.Source, p.curr, msg)
}

// searchVariable looks for an active variable with the given name in the function.
//
// Equivalent to `searchvar` in upstream Lua.
func (p *parser) searchVariable(fs *funcState, n string) (_ expressionDescriptor, found bool) {
	for i := int(fs.numActiveVariables) - 1; i >= 0; i-- {
		vd := p.localVariableDescription(fs, i)
		if vd.name == n {
			if vd.kind == CompileTimeConstant {
				return constLocalExpression(fs.firstLocal + i), true
			}
			return localExpression(vd.ridx, uint16(i)), true
		}
	}
	return voidExpression(), false
}

// removeVariables closes the scope for all variables up to the given level.
//
// Equivalent to `removevars` in upstream Lua.
func (p *parser) removeVariables(fs *funcState, toLevel int) {
	p.activeVariables = p.activeVariables[:len(p.activeVariables)-(int(fs.numActiveVariables)-toLevel)]
	for int(fs.numActiveVariables) > toLevel {
		fs.numActiveVariables--
		if v := p.localDebugInfo(fs, int(fs.numActiveVariables)); v != nil {
			v.EndPC = len(fs.Code)
		}
	}
}

// localDebugInfo returns the debug information for current variable vidx.
//
// Equivalent to `localdebuginfo` in upstream Lua.
func (p *parser) localDebugInfo(fs *funcState, vidx int) *LocalVariable {
	vd := p.localVariableDescription(fs, vidx)
	if vd.kind == CompileTimeConstant {
		// Constants don't have debug information.
		return nil
	}
	return &fs.LocalVariables[vd.pidx]
}

// registerLevel converts a compiler index level to its corresponding register.
// It searches for the highest variable below that level
// that is in a register
// and uses its register index ('ridx') plus one.
//
// Equivalent to `reglevel` in upstream Lua.
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
//
// Equivalent to `luaY_nvarstack` in upstream Lua.
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

// localVariableDescription describes the i'th local variable
// in the given function.
//
// Equivalent to `getlocalvardesc` in upstream Lua.
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
//
// Equivalent to `createlabel` in upstream Lua.
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
//
// Equivalent to `solvegotos` in upstream Lua.
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
//
// Equivalent to `solvegoto` in upstream Lua.
func (p *parser) solveGoto(fs *funcState, g int, lb *labelDescription) error {
	gt := &p.pendingGotos[g]
	if gt.numActiveVariables < lb.numActiveVariables {
		// It entered a scope.
		varName := p.localVariableDescription(fs, int(gt.numActiveVariables)).name
		msg := fmt.Sprintf("<goto %s> at line %d jumps into the scope of local '%s'", gt.name, gt.position.Line, varName)
		return syntaxError(fs.Source, lualex.Token{}, msg)
	}
	if err := fs.patchList(gt.pc, lb.pc, noRegister, lb.pc); err != nil {
		return syntaxError(fs.Source, p.curr, err.Error())
	}
	p.pendingGotos = slices.Delete(p.pendingGotos, g, g+1)
	return nil
}

// syntaxError creates an error with the given parser context.
//
// Equivalent to `lexerror`/`luaX_syntaxerror` in upstream Lua.
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
//
// Mostly equivalent to `block_follow` in upstream Lua,
// but punts the withuntil parameter behavior to the caller.
func isBlockFollow(k lualex.TokenKind) bool {
	return k == lualex.ElseToken ||
		k == lualex.ElseifToken ||
		k == lualex.EndToken ||
		k == lualex.ErrorToken
}
