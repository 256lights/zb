// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package aterm implements the subset of the ASCII ATerm format used by Nix.
// Specifically, this package parses strings, lists, and tuples.
package aterm

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// A Token holds a string or a delimiter.
type Token struct {
	Kind  TokenKind
	Value string
}

// String returns the token in ATerm text format.
func (tok Token) String() string {
	switch tok.Kind {
	case 0:
		return "Token()"
	case String:
		return string(AppendString(nil, tok.Value))
	case LParen, RParen, LBracket, RBracket:
		return string(tok.Kind)
	default:
		var buf []byte
		buf = append(buf, "Token("...)
		buf = AppendString(buf, string(tok.Kind))
		buf = append(buf, ","...)
		buf = AppendString(buf, tok.Value)
		buf = append(buf, ")"...)
		return string(buf)
	}
}

// TokenKind is an ATerm text format delimiter.
// Used to differentiate [Token] values.
type TokenKind byte

// Defined token kinds.
const (
	String   TokenKind = '"'
	LParen   TokenKind = '('
	RParen   TokenKind = ')'
	LBracket TokenKind = '['
	RBracket TokenKind = ']'
)

func (kind TokenKind) String() string {
	return string(kind)
}

var closingTokens = map[TokenKind]TokenKind{
	LParen:   RParen,
	LBracket: RBracket,
}

// Scanner reads ATerm text format tokens from a stream.
type Scanner struct {
	r      io.ByteReader
	err    error
	curr   Token
	stack  []TokenKind
	first  bool // no comma required
	unread bool
}

// NewScanner returns a new scanner that reads from r.
func NewScanner(r io.ByteReader) *Scanner {
	return &Scanner{
		r:     r,
		first: true,
	}
}

// ReadToken reads the next token from the underlying reader.
// ReadToken returns [io.EOF] if and only if the scanner has read a single complete value.
func (s *Scanner) ReadToken() (Token, error) {
	if s.unread {
		s.unread = false
		return s.curr, nil
	}
	if s.err != nil {
		return Token{}, s.err
	}

	s.curr = Token{}
	if len(s.stack) == 0 && !s.first {
		// If we already read a value, don't read past that and return EOF.
		// This allows the caller to use the underlying reader on trailing data.
		s.err = io.EOF
		return Token{}, s.err
	}
	b, err := s.r.ReadByte()
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	if err != nil {
		return Token{}, err
	}

	// Consume comma if required.
	comma := false
	if len(s.stack) > 0 && !s.first {
		term := byte(s.stack[len(s.stack)-1])
		switch b {
		case ',':
			b, err = s.r.ReadByte()
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			if err != nil {
				return Token{}, err
			}
			comma = true
		case term:
			s.stack = slices.Delete(s.stack, len(s.stack)-1, len(s.stack))
			s.curr = Token{Kind: TokenKind(b)}
			s.first = false
			return s.curr, nil
		default:
			s.err = fmt.Errorf("parse aterm: unexpected %q (expected %q or ',')", b, term)
			return Token{}, s.err
		}
	}

	switch b {
	case '[', '(':
		s.stack = append(s.stack, closingTokens[TokenKind(b)])
		s.curr = Token{Kind: TokenKind(b)}
		s.first = true
	case '"':
		value, err := parseString(s.r)
		if err != nil {
			// Interrupting a string midway through is a fatal error since we lose state.
			s.err = err
			return Token{}, err
		}
		s.curr = Token{Kind: String, Value: value}
		s.first = false
	case ']', ')':
		if comma {
			s.err = fmt.Errorf("parse aterm: unexpected %q (expected value)", b)
			return Token{}, s.err
		}
		if len(s.stack) == 0 {
			s.err = fmt.Errorf("parse aterm: unexpected %q (does not match)", b)
			return Token{}, s.err
		}
		if term := byte(s.stack[len(s.stack)-1]); b != term {
			s.err = fmt.Errorf("parse aterm: unexpected %q (expected %q)", b, term)
			return Token{}, s.err
		}
		s.stack = slices.Delete(s.stack, len(s.stack)-1, len(s.stack))
		s.curr = Token{Kind: TokenKind(b)}
		s.first = false
	default:
		s.err = fmt.Errorf("parse aterm: unexpected character %q (expected value)", b)
		return Token{}, s.err
	}
	return s.curr, nil
}

var errInvalidUnreadToken = errors.New("parse aterm: invalid use of UnreadToken")

// UnreadToken causes the next call to [Scanner.ReadToken]
// to return the last token read.
// If the last operation was not a successful call to ReadToken,
// UnreadToken will return an error.
func (s *Scanner) UnreadToken() error {
	if s.unread || s.curr.Kind == 0 {
		return errInvalidUnreadToken
	}
	s.unread = true
	return nil
}

const maxStringLength = 4096

func parseString(r io.ByteReader) (s string, err error) {
	sb := new(strings.Builder)
	for {
		c, err := r.ReadByte()
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		if err != nil {
			return sb.String(), fmt.Errorf("parse aterm string: %w", err)
		}

		if c == '"' {
			return sb.String(), nil
		}
		if sb.Len() >= maxStringLength {
			return sb.String(), fmt.Errorf("parse aterm string: too large")
		}

		switch c {
		case '\\':
			c, err := r.ReadByte()
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			if err != nil {
				return sb.String(), fmt.Errorf("parse aterm string: %w", err)
			}

			switch c {
			case '"', '\\':
				sb.WriteByte(c)
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			default:
				if c < ' ' || c >= 127 {
					return "", fmt.Errorf("parse aterm string: non-text escape character %q", c)
				}
				return "", fmt.Errorf("parse aterm string: unknown escape sequence '\\%c'", c)
			}
		default:
			sb.WriteByte(c)
		}
	}
}

// AppendString appends the string to dst as an ATerm text format double-quoted string.
func AppendString(dst []byte, s string) []byte {
	size := len(s) + len(`""`)
	for _, c := range []byte(s) {
		if c == '"' || c == '\\' || c == '\n' || c == '\r' || c == '\t' {
			size++
		}
	}

	dst = slices.Grow(dst, size)
	dst = append(dst, '"')
	for _, c := range []byte(s) {
		switch c {
		case '"', '\\':
			dst = append(dst, '\\', c)
		case '\n':
			dst = append(dst, `\n`...)
		case '\r':
			dst = append(dst, `\r`...)
		case '\t':
			dst = append(dst, `\t`...)
		default:
			dst = append(dst, c)
		}
	}
	dst = append(dst, '"')
	return dst
}
