// Code generated by "stringer -type=TokenKind -linecomment"; DO NOT EDIT.

package lualex

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[ErrorToken-0]
	_ = x[IdentifierToken-1]
	_ = x[StringToken-2]
	_ = x[NumeralToken-3]
	_ = x[AndToken-4]
	_ = x[BreakToken-5]
	_ = x[DoToken-6]
	_ = x[ElseToken-7]
	_ = x[ElseifToken-8]
	_ = x[EndToken-9]
	_ = x[FalseToken-10]
	_ = x[ForToken-11]
	_ = x[FunctionToken-12]
	_ = x[GotoToken-13]
	_ = x[IfToken-14]
	_ = x[InToken-15]
	_ = x[LocalToken-16]
	_ = x[NilToken-17]
	_ = x[NotToken-18]
	_ = x[OrToken-19]
	_ = x[RepeatToken-20]
	_ = x[ReturnToken-21]
	_ = x[ThenToken-22]
	_ = x[TrueToken-23]
	_ = x[UntilToken-24]
	_ = x[WhileToken-25]
	_ = x[AddToken-26]
	_ = x[SubToken-27]
	_ = x[MulToken-28]
	_ = x[DivToken-29]
	_ = x[ModToken-30]
	_ = x[PowToken-31]
	_ = x[LenToken-32]
	_ = x[BitAndToken-33]
	_ = x[BitXorToken-34]
	_ = x[BitOrToken-35]
	_ = x[LShiftToken-36]
	_ = x[RShiftToken-37]
	_ = x[IntDivToken-38]
	_ = x[EqualToken-39]
	_ = x[NotEqualToken-40]
	_ = x[LessEqualToken-41]
	_ = x[GreaterEqualToken-42]
	_ = x[LessToken-43]
	_ = x[GreaterToken-44]
	_ = x[AssignToken-45]
	_ = x[LParenToken-46]
	_ = x[RParenToken-47]
	_ = x[LBraceToken-48]
	_ = x[RBraceToken-49]
	_ = x[LBracketToken-50]
	_ = x[RBracketToken-51]
	_ = x[LabelToken-52]
	_ = x[SemiToken-53]
	_ = x[ColonToken-54]
	_ = x[CommaToken-55]
	_ = x[DotToken-56]
	_ = x[ConcatToken-57]
	_ = x[VarargToken-58]
}

const _TokenKind_name = "ErrorTokenIdentifierTokenStringTokenNumeralTokenandbreakdoelseelseifendfalseforfunctiongotoifinlocalnilnotorrepeatreturnthentrueuntilwhile+-*/%^#&~|<<>>//==~=<=>=<>=(){}[]::;:,......"

var _TokenKind_index = [...]uint8{0, 10, 25, 36, 48, 51, 56, 58, 62, 68, 71, 76, 79, 87, 91, 93, 95, 100, 103, 106, 108, 114, 120, 124, 128, 133, 138, 139, 140, 141, 142, 143, 144, 145, 146, 147, 148, 150, 152, 154, 156, 158, 160, 162, 163, 164, 165, 166, 167, 168, 169, 170, 171, 173, 174, 175, 176, 177, 179, 182}

func (i TokenKind) String() string {
	if i < 0 || i >= TokenKind(len(_TokenKind_index)-1) {
		return "TokenKind(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _TokenKind_name[_TokenKind_index[i]:_TokenKind_index[i+1]]
}
