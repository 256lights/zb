// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

//go:generate go tool stringer -type=TokenKind -linecomment

package lualex

import "fmt"

// Token represents a single lexical element in a Lua source file.
type Token struct {
	Kind     TokenKind
	Position Position
	// Value holds information for
	// an [IdentifierToken], a [StringToken], or a [NumeralToken].
	Value string
}

// String formats the token as it would appear in Lua source.
// String returns "<eof>" for [ErrorToken].
func (tok Token) String() string {
	switch tok.Kind {
	case ErrorToken:
		return "<eof>"
	case StringToken:
		return Quote(tok.Value)
	case IdentifierToken, NumeralToken:
		return tok.Value
	default:
		return tok.Kind.String()
	}
}

// Position represents a position in a textual source file.
type Position struct {
	// Line is the 1-based line number.
	Line int
	// Column is the 1-based column number.
	// Columns are based in bytes.
	// Zero indicates that the position only has line number information.
	Column int
}

// Pos returns a new position with the given line number and column.
// It panics if the resulting Position would not be valid
// (as reported by [Position.IsValid]).
func Pos(line, col int) Position {
	pos := Position{Line: line, Column: col}
	if !pos.IsValid() {
		panic("invalid Pos()")
	}
	return pos
}

// String formats the position as "line:col".
func (pos Position) String() string {
	if !pos.IsValid() {
		return "<invalid position>"
	}
	if pos.Column == 0 {
		return fmt.Sprintf("%d", pos.Line)
	}
	return fmt.Sprintf("%d:%d", pos.Line, pos.Column)
}

// IsValid reports whether pos has a positive line number
// and a non-negative column.
// (A zero column indicates line-only position information.)
func (pos Position) IsValid() bool {
	return pos.Line > 0 && pos.Column >= 0
}

// TokenKind is an enumeration of valid [Token] types.
// The zero value is [ErrorToken].
type TokenKind int

// [TokenKind] values.
const (
	// ErrorToken indicates an invalid token.
	ErrorToken TokenKind = iota
	// IdentifierToken indicates a name.
	// The Value field of [Token] will contain the identifier.
	IdentifierToken
	// StringToken indicates a literal string.
	// The Value field of [Token] will contain the parsed value of the string.
	StringToken
	// NumeralToken indicates a numeric constant.
	// The Value field of [Token] will contain the constant as written.
	NumeralToken

	// Keywords

	AndToken      // and
	BreakToken    // break
	DoToken       // do
	ElseToken     // else
	ElseifToken   // elseif
	EndToken      // end
	FalseToken    // false
	ForToken      // for
	FunctionToken // function
	GotoToken     // goto
	IfToken       // if
	InToken       // in
	LocalToken    // local
	NilToken      // nil
	NotToken      // not
	OrToken       // or
	RepeatToken   // repeat
	ReturnToken   // return
	ThenToken     // then
	TrueToken     // true
	UntilToken    // until
	WhileToken    // while

	// Operators

	AddToken          // +
	SubToken          // -
	MulToken          // *
	DivToken          // /
	ModToken          // %
	PowToken          // ^
	LenToken          // #
	BitAndToken       // &
	BitXorToken       // ~
	BitOrToken        // |
	LShiftToken       // <<
	RShiftToken       // >>
	IntDivToken       // //
	EqualToken        // ==
	NotEqualToken     // ~=
	LessEqualToken    // <=
	GreaterEqualToken // >=
	LessToken         // <
	GreaterToken      // >
	AssignToken       // =
	LParenToken       // (
	RParenToken       // )
	LBraceToken       // {
	RBraceToken       // }
	LBracketToken     // [
	RBracketToken     // ]
	LabelToken        // ::
	SemiToken         // ;
	ColonToken        // :
	CommaToken        // ,
	DotToken          // .
	ConcatToken       // ..
	VarargToken       // ...
)

var keywords = map[string]TokenKind{
	"and":      AndToken,
	"break":    BreakToken,
	"do":       DoToken,
	"else":     ElseToken,
	"elseif":   ElseifToken,
	"end":      EndToken,
	"false":    FalseToken,
	"for":      ForToken,
	"function": FunctionToken,
	"goto":     GotoToken,
	"if":       IfToken,
	"in":       InToken,
	"local":    LocalToken,
	"nil":      NilToken,
	"not":      NotToken,
	"or":       OrToken,
	"repeat":   RepeatToken,
	"return":   ReturnToken,
	"then":     ThenToken,
	"true":     TrueToken,
	"until":    UntilToken,
	"while":    WhileToken,
}
