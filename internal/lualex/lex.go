// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package lualex provides a scanner to split a byte stream
// into [Lua lexical elements].
//
// [Lua lexical elements]: https://www.lua.org/manual/5.4/manual.html#3.1
package lualex

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// A Scanner parses Lua tokens from a byte stream.
type Scanner struct {
	r    io.ByteScanner
	next Position
	prev Position
	err  error

	equals int
}

// NewScanner returns a [Scanner] that reads from r.
// NewScanner does not buffer r.
func NewScanner(r io.ByteScanner) *Scanner {
	return &Scanner{
		r:    r,
		next: Position{Line: 1, Column: 1},
	}
}

// Scan reads the next [Token] from the stream.
// If Scan returns an error,
// then the returned token will be an [ErrorToken]
// with the Position field set to the approximate position of the error.
// If an ErrorToken does not have a valid Position,
// then it indicates an error returned from the underlying reader
// or an otherwise unrecoverable error.
func (s *Scanner) Scan() (Token, error) {
	if s.err != nil {
		return Token{}, s.err
	}
	if s.equals > 0 {
		pos := Position{Line: s.next.Line, Column: s.next.Column - s.equals}
		if s.equals == 1 {
			s.equals--
			return Token{Kind: AssignToken, Position: pos}, nil
		}
		s.equals -= 2
		return Token{Kind: EqualToken, Position: pos}, nil
	}

	for {
		b, err := s.readByte()
		if err != nil {
			return Token{}, err
		}
		switch {
		case isSpace(b):
			// Ignore.
		case isLetter(b) || b == '_':
			// Identifier or keyword.
			pos := s.prev
			sb := new(strings.Builder)
			sb.WriteByte(b)
			for {
				b, err := s.readByte()
				if err != nil {
					break
				}
				if b != '_' && !isLetter(b) && !isDigit(b) {
					s.unreadByte()
					break
				}
				sb.WriteByte(b)
			}
			value := sb.String()
			if kind, isKeyword := keywords[value]; isKeyword {
				return Token{Kind: kind, Position: pos}, nil
			}
			return Token{Kind: IdentifierToken, Position: pos, Value: value}, nil
		case isDigit(b):
			s.unreadByte()
			start := s.next
			value, err := s.numeral(false)
			if err != nil {
				return Token{Kind: ErrorToken, Position: start}, err
			}
			return Token{Kind: NumeralToken, Position: start, Value: value}, nil
		case b == '\'' || b == '"':
			pos := s.prev
			value, err := s.shortLiteralString(b)
			if err != nil {
				s.err = err
				return Token{Kind: ErrorToken, Position: pos}, err
			}
			return Token{Kind: StringToken, Position: pos, Value: value}, nil
		case b == '+':
			return Token{Kind: AddToken, Position: s.prev}, nil
		case b == '-':
			// Look ahead for double-hyphen before returning.
			pos := s.prev
			b, err := s.readByte()
			if err != nil {
				return Token{Kind: SubToken, Position: pos}, nil
			}
			if b != '-' {
				s.unreadByte()
				return Token{Kind: SubToken, Position: pos}, nil
			}

			if n, err := s.longOpenBracket(); err == nil {
				// Long comment.
				if err := s.findClosingLongBracket(discardByteWriter{}, n); err != nil {
					s.err = err
					return Token{Kind: ErrorToken, Position: pos}, err
				}
			} else {
				// Short comment.
				for {
					b, err := s.readByte()
					if err != nil {
						return Token{}, err
					}
					if b == '\n' {
						break
					}
				}
			}
		case b == '*':
			return Token{Kind: MulToken, Position: s.prev}, nil
		case b == '/':
			pos := s.prev
			b, err := s.readByte()
			if err != nil {
				return Token{Kind: DivToken, Position: pos}, nil
			}
			if b == '/' {
				return Token{Kind: IntDivToken, Position: pos}, nil
			}
			s.unreadByte()
			return Token{Kind: DivToken, Position: pos}, nil
		case b == '%':
			return Token{Kind: ModToken, Position: s.prev}, nil
		case b == '^':
			return Token{Kind: PowToken, Position: s.prev}, nil
		case b == '#':
			return Token{Kind: LenToken, Position: s.prev}, nil
		case b == '&':
			return Token{Kind: BitAndToken, Position: s.prev}, nil
		case b == '~':
			pos := s.prev
			b, err := s.readByte()
			if err != nil {
				return Token{Kind: BitXorToken, Position: pos}, nil
			}
			if b == '=' {
				return Token{Kind: NotEqualToken, Position: pos}, nil
			}
			s.unreadByte()
			return Token{Kind: BitXorToken, Position: pos}, nil
		case b == '|':
			return Token{Kind: BitOrToken, Position: s.prev}, nil
		case b == '<':
			pos := s.prev
			b, err := s.readByte()
			if err != nil {
				return Token{Kind: LessToken, Position: pos}, nil
			}
			switch b {
			case '<':
				return Token{Kind: LShiftToken, Position: pos}, nil
			case '=':
				return Token{Kind: LessEqualToken, Position: pos}, nil
			default:
				s.unreadByte()
				return Token{Kind: LessToken, Position: pos}, nil
			}
		case b == '>':
			pos := s.prev
			b, err := s.readByte()
			if err != nil {
				return Token{Kind: GreaterToken, Position: pos}, nil
			}
			switch b {
			case '<':
				return Token{Kind: RShiftToken, Position: pos}, nil
			case '=':
				return Token{Kind: GreaterEqualToken, Position: pos}, nil
			default:
				s.unreadByte()
				return Token{Kind: GreaterToken, Position: pos}, nil
			}
		case b == '=':
			pos := s.prev
			b, err := s.readByte()
			if err != nil {
				return Token{Kind: AssignToken, Position: pos}, nil
			}
			if b == '=' {
				return Token{Kind: EqualToken, Position: pos}, nil
			}
			s.unreadByte()
			return Token{Kind: AssignToken, Position: pos}, nil
		case b == '(':
			return Token{Kind: LParenToken, Position: s.prev}, nil
		case b == ')':
			return Token{Kind: RParenToken, Position: s.prev}, nil
		case b == '{':
			return Token{Kind: LBraceToken, Position: s.prev}, nil
		case b == '}':
			return Token{Kind: RBraceToken, Position: s.prev}, nil
		case b == '[':
			pos := s.prev
			s.unreadByte()

			n, err := s.longOpenBracket()
			if err != nil {
				// Open bracket with zero or more equals signs following it.
				s.equals = n
				return Token{Kind: LBracketToken, Position: pos}, nil
			}

			llw := new(longLiteralWriter)
			if err := s.findClosingLongBracket(llw, n); err != nil {
				s.err = err
				return Token{Kind: ErrorToken, Position: pos}, err
			}
			return Token{Kind: StringToken, Position: pos, Value: llw.String()}, nil
		case b == ']':
			return Token{Kind: RBracketToken, Position: s.prev}, nil
		case b == ':':
			pos := s.prev
			b, err := s.readByte()
			if err != nil {
				return Token{Kind: ColonToken, Position: pos}, nil
			}
			if b == ':' {
				return Token{Kind: LabelToken, Position: pos}, nil
			}
			s.unreadByte()
			return Token{Kind: ColonToken, Position: pos}, nil
		case b == ';':
			return Token{Kind: SemiToken, Position: s.prev}, nil
		case b == ',':
			return Token{Kind: CommaToken, Position: s.prev}, nil
		case b == '.':
			pos := s.prev
			b, err := s.readByte()
			if err != nil {
				return Token{Kind: DotToken, Position: pos}, nil
			}
			switch {
			case b == '.':
				b, err = s.readByte()
				if err != nil {
					return Token{Kind: ConcatToken, Position: pos}, nil
				}
				if b != '.' {
					s.unreadByte()
					return Token{Kind: ConcatToken, Position: pos}, nil
				}
				return Token{Kind: VarargToken, Position: pos}, nil
			case isDigit(b):
				s.unreadByte()
				value, err := s.numeral(true)
				if err != nil {
					return Token{Kind: ErrorToken, Position: pos}, err
				}
				return Token{Kind: NumeralToken, Position: pos, Value: value}, nil
			default:
				s.unreadByte()
				return Token{Kind: DotToken, Position: pos}, nil
			}
		default:
			s.err = fmt.Errorf("%v: unexpected %q", s.prev, b)
			return Token{Kind: ErrorToken, Position: s.prev}, s.err
		}
	}
}

func (s *Scanner) shortLiteralString(end byte) (string, error) {
	sb := new(strings.Builder)
	for {
		b, err := s.readByteNoEOF()
		if err != nil {
			return sb.String(), err
		}
		switch {
		case b == end:
			return sb.String(), nil
		case b == '\n' || b == '\r':
			return sb.String(), fmt.Errorf("%v: unescaped newline in string", s.prev)
		case b != '\\':
			sb.WriteByte(b)
			continue
		}

		// Backslash escape.
		b, err = s.readByteNoEOF()
		if err != nil {
			return sb.String(), err
		}
		switch b {
		case 'a':
			sb.WriteByte('\a')
		case 'b':
			sb.WriteByte('\b')
		case 'f':
			sb.WriteByte('\f')
		case 'n':
			sb.WriteByte('\n')
		case 'r':
			sb.WriteByte('\r')
		case 't':
			sb.WriteByte('\t')
		case 'v':
			sb.WriteByte('\v')
		case '\\', '\'', '"':
			sb.WriteByte(b)
		case '\n', '\r':
			b2, err := s.readByteNoEOF()
			if err != nil {
				return sb.String(), err
			}
			if !(b == '\n' && b2 == '\r') && !(b == '\r' && b2 == '\n') {
				s.unreadByte()
			}
			sb.WriteByte('\n')
		case 'z':
			// "'\z' skips the following span of whitespace characters, including line breaks"
			for {
				b, err := s.readByteNoEOF()
				if err != nil {
					return sb.String(), err
				}
				if !isSpace(b) {
					s.unreadByte()
					break
				}
			}
		case 'x':
			var nibbles [2]byte
			for i := range nibbles {
				digit, err := s.readByteNoEOF()
				if err != nil {
					return sb.String(), err
				}
				nibbles[i], err = hexDigit(digit)
				if err != nil {
					return sb.String(), fmt.Errorf("%v: %v", s.prev, err)
				}
			}
			sb.WriteByte(nibbles[0]<<4 | nibbles[1])
		case 'u':
			// \u{XXX}, 1+ hex digits to UTF-8 limited to 2^31
			b, err := s.readByteNoEOF()
			if err != nil {
				return sb.String(), err
			}
			if b != '{' {
				return sb.String(), fmt.Errorf("%v: unexpected %q (want '{')", s.prev, b)
			}
			var r rune
			start := s.next
			for first := true; ; first = false {
				b, err := s.readByteNoEOF()
				if err != nil {
					return sb.String(), err
				}
				if b == '}' {
					if first {
						return sb.String(), fmt.Errorf("%v: unexpected '}' (want hex digit)", s.prev)
					}
					break
				}
				nibble, err := hexDigit(b)
				if err != nil {
					return sb.String(), fmt.Errorf("%v: %v", s.prev, err)
				}
				if r > 0x7FFFFFFF>>4 {
					return sb.String(), fmt.Errorf("%v: utf-8 value too large", start)
				}
				r = r<<4 | rune(nibble)
			}
			sb.WriteRune(r)
		default:
			if !isDigit(b) {
				return sb.String(), fmt.Errorf("%v: invalid escape sequence", s.prev)
			}
			// Decimal escape (1-3 digits).
			start := s.prev
			result := uint16(b - '0')
			for range 2 {
				b, err := s.readByteNoEOF()
				if err != nil {
					return sb.String(), err
				}
				if !isDigit(b) {
					s.unreadByte()
					break
				}
				result = 10*result + uint16(b-'0')
			}
			if result > 0xff {
				return sb.String(), fmt.Errorf("%v: decimal escape too large", start)
			}
			sb.WriteByte(byte(result))
		}
	}
}

func (s *Scanner) numeral(dot bool) (string, error) {
	sb := new(strings.Builder)
	isHex := false
	if dot {
		sb.WriteByte('.')
	} else {
		first, err := s.readByte()
		if err != nil {
			return "", err
		}
		if !isDigit(first) {
			s.unreadByte()
			return "", fmt.Errorf("%v: unexpected %q (want numeral)", s.next, first)
		}
		sb.WriteByte(first)

		second, err := s.readByte()
		if err != nil {
			return sb.String(), nil
		}
		isHex = first == '0' && (second == 'x' || second == 'X')
		if isHex {
			sb.WriteByte(second)
		} else {
			s.unreadByte()
		}

	whole:
		for {
			b, err := s.readByte()
			switch {
			case err != nil:
				return sb.String(), nil
			case isDigit(b) || isHex && isHexDigit(b):
				sb.WriteByte(b)
			case isExponentDelim(b, isHex):
				sb.WriteByte(b)
				err := s.exponent(sb)
				return sb.String(), err
			case b == '.':
				sb.WriteByte(b)
				break whole
			case isLetter(b):
				s.unreadByte()
				return sb.String(), fmt.Errorf("%v: unexpected %q (numeral followed by letter)", s.next, b)
			default:
				s.unreadByte()
				return sb.String(), nil
			}
		}
	}

	// Fractional part.
	for {
		b, err := s.readByte()
		switch {
		case err != nil:
			return sb.String(), nil
		case isDigit(b) || isHex && isHexDigit(b):
			sb.WriteByte(b)
		case isExponentDelim(b, isHex):
			sb.WriteByte(b)
			err := s.exponent(sb)
			return sb.String(), err
		case isLetter(b):
			s.unreadByte()
			return sb.String(), fmt.Errorf("%v: unexpected %q (numeral followed by letter)", s.next, b)
		default:
			s.unreadByte()
			return sb.String(), nil
		}
	}
}

func (s *Scanner) exponent(sb *strings.Builder) error {
	b, err := s.readByteNoEOF()
	if err != nil {
		return err
	}
	switch {
	case b == '-' || b == '+':
		sb.WriteByte(b)
		b, err = s.readByteNoEOF()
		if err != nil {
			return err
		}
		if !isDigit(b) {
			s.unreadByte()
			return fmt.Errorf("%v: unexpected %q (want digit or sign)", s.prev, b)
		}
		sb.WriteByte(b)
	case !isDigit(b):
		s.unreadByte()
		return fmt.Errorf("%v: unexpected %q (want digit or sign)", s.prev, b)
	default:
		sb.WriteByte(b)
	}

	// Optional remaining digits.
	for {
		b, err := s.readByte()
		if err != nil {
			return nil
		}
		if !isDigit(b) {
			s.unreadByte()
			return nil
		}
		sb.WriteByte(b)
	}
}

func (s *Scanner) longOpenBracket() (int, error) {
	b, err := s.readByte()
	if err != nil {
		return 0, err
	}
	if b != '[' {
		s.unreadByte()
		return 0, fmt.Errorf("%v: unexpected %q (want '[')", s.next, b)
	}

	n := 0
	for {
		b, err := s.readByteNoEOF()
		if err != nil {
			return n, err
		}
		switch b {
		case '=':
			n++
		case '[':
			return n, nil
		default:
			s.unreadByte()
			return n, fmt.Errorf("%v: unexpected %q (want '[' or '=')", s.next, b)
		}
	}
}

// findClosingLongBracket copies bytes from s.r to w
// until a closing long bracket of level n is found.
// (For example, a closing long bracket of level 4 is "]====]".)
func (s *Scanner) findClosingLongBracket(w io.ByteWriter, n int) error {
	writePartial := func(n int) error {
		if err := w.WriteByte(']'); err != nil {
			return err
		}
		for i := 0; i < n; i++ {
			if err := w.WriteByte('='); err != nil {
				return err
			}
		}
		return nil
	}

searchStart:
	for {
		// Find initial closing bracket.
		b, err := s.readByteNoEOF()
		if err != nil {
			return err
		}
		if b != ']' {
			if err := w.WriteByte(b); err != nil {
				return err
			}
			continue
		}

		// Consume equal signs.
		for i := 0; i < n; i++ {
			b, err := s.readByteNoEOF()
			if err != nil {
				writePartial(i)
				return err
			}
			if b != '=' {
				s.unreadByte()
				if err := writePartial(i); err != nil {
					return err
				}
				continue searchStart
			}
		}

		// Read second closing bracket (hopefully).
		b, err = s.readByteNoEOF()
		if err != nil {
			writePartial(n)
			return err
		}
		if b == ']' {
			return nil
		}
		writePartial(n)
		if err := w.WriteByte(b); err != nil {
			return err
		}
	}
}

func (s *Scanner) readByte() (byte, error) {
	b, err := s.r.ReadByte()
	if err != nil {
		return b, err
	}
	s.prev = s.next
	switch b {
	case '\n':
		s.next.Line++
		s.next.Column = 1
	case '\t':
		s.next.Column++
		const tabWidth = 8
		for s.next.Column%tabWidth != 0 {
			s.next.Column++
		}
	default:
		s.next.Column++
	}
	return b, nil
}

func (s *Scanner) readByteNoEOF() (byte, error) {
	b, err := s.readByte()
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return b, err
}

func (s *Scanner) unreadByte() error {
	if err := s.r.UnreadByte(); err != nil {
		return err
	}
	s.next = s.prev
	return nil
}

// Quote returns a double-quoted Lua string literal representing s.
func Quote(s string) string {
	sb := new(strings.Builder)
	sb.Grow(len(s) + 2)
	sb.WriteByte('"')
	for {
		c, size := utf8.DecodeRuneInString(s)
		switch {
		case size == 0:
			sb.WriteByte('"')
			return sb.String()
		case c == utf8.RuneError && size == 1:
			sb.WriteString(`\x`)
			for _, digit := range toHexDigits(s[0]) {
				sb.WriteByte(digit)
			}
		case c == '\\' || c == '"':
			sb.WriteByte('\\')
			sb.WriteRune(c)
		case isPrint(c):
			sb.WriteRune(c)
		case c == '\a':
			sb.WriteString(`\a`)
		case c == '\b':
			sb.WriteString(`\b`)
		case c == '\f':
			sb.WriteString(`\f`)
		case c == '\n':
			sb.WriteString(`\n`)
		case c == '\r':
			sb.WriteString(`\r`)
		case c == '\t':
			sb.WriteString(`\t`)
		case c == '\v':
			sb.WriteString(`\v`)
		default:
			sb.WriteString(`\u{`)
			fmt.Fprintf(sb, "%x", c)
			sb.WriteString(`}`)
		}
		s = s[size:]
	}
}

// Unquote interprets s as a single-quoted, double-quoted, or bracket-delimited Lua string literal,
// returning the string value that s quotes.
func Unquote(s string) (string, error) {
	if len(s) < 2 {
		return "", errUnquoteSyntax
	}
	sr := strings.NewReader(s)
	var unquoted string
	switch s[0] {
	case '\'', '"':
		sr.ReadByte()
		scanner := NewScanner(sr)
		var err error
		unquoted, err = scanner.shortLiteralString(s[0])
		if err != nil {
			return "", errUnquoteSyntax
		}
	case '[':
		scanner := NewScanner(sr)
		level, err := scanner.longOpenBracket()
		if err != nil {
			return "", errUnquoteSyntax
		}
		sb := new(strings.Builder)
		err = scanner.findClosingLongBracket(sb, level)
		if err != nil {
			return "", errUnquoteSyntax
		}
		unquoted = sb.String()
	default:
		return "", errUnquoteSyntax
	}

	if sr.Len() > 0 {
		return "", errUnquoteSyntax
	}
	return unquoted, nil
}

var errUnquoteSyntax = errors.New("invalid syntax")

// isSpace reports whether the given byte represents a space in Lua source code.
// According to the [reference],
// "[i]n source code, Lua recognizes as spaces the standard ASCII whitespace characters
// space, form feed, newline, carriage return, horizontal tab, and vertical tab."
//
// [reference]: https://www.lua.org/manual/5.4/manual.html#:~:text=In%20source%20code%2C%20Lua%20recognizes%20as%20spaces,.
func isSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\t' || c == '\r' || c == '\f' || c == '\v'
}

func isLetter(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

func isDigit(c byte) bool {
	return '0' <= c && c <= '9'
}

func isHexDigit(c byte) bool {
	return isDigit(c) || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

func toHexDigits(x byte) [2]byte {
	var result [2]byte
	if hi := x >> 4; hi < 0xa {
		result[0] = hi + '0'
	} else {
		result[0] = hi - 0xa + 'a'
	}
	if lo := x & 0xf; lo < 0xa {
		result[1] = lo + '0'
	} else {
		result[1] = lo - 0xa + 'a'
	}
	return result
}

func isPrint(c rune) bool {
	return 0x20 <= c && c < 0x7f
}

func isExponentDelim(c byte, isHex bool) bool {
	return (!isHex && (c == 'E' || c == 'e')) || (isHex && (c == 'P' || c == 'p'))
}

func hexDigit(c byte) (byte, error) {
	switch {
	case isDigit(c):
		return c - '0', nil
	case 'a' <= c && c <= 'f':
		return c - 'a' + 0xa, nil
	case 'A' <= c && c <= 'F':
		return c - 'A' + 0xa, nil
	default:
		return 0, fmt.Errorf("unexpected %q (want hex digit)", c)
	}
}

type longLiteralWriter struct {
	sb   strings.Builder
	prev byte
}

func (llw *longLiteralWriter) WriteByte(c byte) error {
	switch {
	case llw.prev == '\r' && c == '\n' || llw.prev == '\n' && c == '\r':
		llw.sb.WriteByte('\n')
		llw.prev = 0
	case c == '\n' || c == '\r':
		if llw.prev != 0 {
			llw.sb.WriteByte('\n')
		}
		llw.prev = c
	case llw.prev != 0:
		llw.sb.WriteByte('\n')
		llw.prev = 0
		fallthrough
	default:
		llw.sb.WriteByte(c)
	}
	return nil
}

func (llw *longLiteralWriter) String() string {
	if llw.prev != 0 {
		llw.sb.WriteByte('\n')
		llw.prev = 0
	}
	return llw.sb.String()
}

type discardByteWriter struct{}

func (discardByteWriter) WriteByte(c byte) error { return nil }
