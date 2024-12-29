// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package lualex

import (
	"strconv"
	"strings"
)

// ParseInt converts the given string to a 64-bit signed integer
// according to the [lexical rules of Lua].
// Surrounding whitespace is permitted,
// and any error returned will be of type [*strconv.NumError].
//
// [lexical rules of Lua]: https://lua.org/manual/5.4/manual.html#3.1
func ParseInt(s string) (int64, error) {
	s = trimSpace(s)
	neg, withoutSign := cutSign(s)
	if strings.Contains(withoutSign, "_") {
		return 0, &strconv.NumError{
			Func: "ParseInt",
			Num:  s,
			Err:  strconv.ErrSyntax,
		}
	}
	switch h, isHex := cutHexPrefix(withoutSign); {
	case isHex && !neg:
		return strconv.ParseInt(h, 16, 64)
	case isHex && neg:
		return strconv.ParseInt("-"+h, 16, 64)
	default:
		return strconv.ParseInt(s, 10, 64)
	}
}

// ParseNumber converts the given string to a 64-bit floating-point number
// according to the [lexical rules of Lua].
// Surrounding whitespace is permitted,
// and any error returned will be of type [*strconv.NumError].
//
// [lexical rules of Lua]: https://lua.org/manual/5.4/manual.html#3.1
func ParseNumber(s string) (float64, error) {
	s = trimSpace(s)
	_, withoutSign := cutSign(s)
	if strings.EqualFold(withoutSign, "Inf") ||
		strings.EqualFold(withoutSign, "Infinity") ||
		strings.EqualFold(withoutSign, "NaN") ||
		strings.Contains(withoutSign, "_") {
		return 0, &strconv.NumError{
			Func: "ParseNumber",
			Num:  s,
			Err:  strconv.ErrSyntax,
		}
	}
	toParse := s
	if (strings.HasPrefix(withoutSign, "0x") || strings.HasPrefix(withoutSign, "0X")) &&
		!strings.ContainsAny(s, "pP") {
		// Go hex float literals must have an exponent.
		toParse = s + "p0"
	}
	f, err := strconv.ParseFloat(toParse, 64)
	if err != nil {
		err.(*strconv.NumError).Num = s
	}
	return f, err
}

func cutHexPrefix(s string) (rest string, hex bool) {
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		return s[2:], true
	}
	return s, false
}

func cutSign(s string) (neg bool, rest string) {
	switch {
	case len(s) == 0:
		return false, s
	case s[0] == '+':
		return false, s[1:]
	case s[0] == '-':
		return true, s[1:]
	default:
		return false, s
	}
}

func trimSpace(s string) string {
	for len(s) > 0 && isSpace(s[0]) {
		s = s[1:]
	}
	for len(s) > 0 && isSpace(s[len(s)-1]) {
		s = s[:len(s)-1]
	}
	return s
}
