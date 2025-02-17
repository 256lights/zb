// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// TableLibraryName is the conventional identifier for the [table manipulation library].
//
// [table manipulation library]: https://www.lua.org/manual/5.4/manual.html#6.6
const UTF8LibraryName = "utf8"

// maxUTF8 is the maximum permitted UTF-8 codepoint.
const maxUTF8 = 0x7fffffff

// OpenUTF8 is a [Function] that loads the [UTF-8 library].
// This function is intended to be used as an argument to [Require].
//
// All functions in the utf8 library are pure (as per [*State.PushPureFunction]).
//
// [UTF-8 library]: https://www.lua.org/manual/5.4/manual.html#6.5
func OpenUTF8(ctx context.Context, l *State) (int, error) {
	NewPureLib(l, map[string]Function{
		"char":        utf8Char,
		"charpattern": nil,
		"codepoint":   utf8Codepoint,
		"codes":       utf8Codes,
		"len":         utf8Len,
		"offset":      utf8Offset,
	})
	l.PushString("[\x00-\x7F\xC2-\xFD][\x80-\xBF]*")
	if err := l.RawSetField(-2, "charpattern"); err != nil {
		return 0, err
	}
	return 1, nil
}

func utf8Char(ctx context.Context, l *State) (int, error) {
	sb := new(strings.Builder)
	for i := range l.Top() {
		codePoint, err := CheckInteger(l, 1+i)
		if err != nil {
			return 0, err
		}
		if codePoint < 0 || codePoint > maxUTF8 {
			return 0, NewArgError(l, 1+i, "value out of range")
		}
		writeRune(sb, rune(codePoint))
	}
	l.PushString(sb.String())
	return 1, nil
}

func utf8Codes(ctx context.Context, l *State) (int, error) {
	lax := l.ToBoolean(2)
	s, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	if len(s) > 0 && !utf8.RuneStart(s[0]) {
		return 0, NewArgError(l, 1, errInvalidUTF8.Error())
	}
	l.PushPureFunction(0, func(ctx context.Context, l *State) (int, error) {
		return utf8CodesNext(ctx, l, lax)
	})
	l.PushValue(1)
	l.PushInteger(0)
	return 3, nil
}

func utf8CodesNext(ctx context.Context, l *State, lax bool) (int, error) {
	s, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	n, _ := l.ToInteger(2)
	for n >= 0 && n < int64(len(s)) && !utf8.RuneStart(s[n]) {
		n++
	}
	if n < 0 || n >= int64(len(s)) {
		return 0, nil
	}
	var c rune
	var size int
	if lax {
		c, size = decodeLaxUTF8RuneInString(s[n:])
	} else {
		c, size = utf8.DecodeRuneInString(s[n:])
	}
	if c == utf8.RuneError && size == 1 ||
		int(n)+size < len(s) && !utf8.RuneStart(s[int(n)+size]) {
		return 0, errInvalidUTF8
	}
	l.PushInteger(n + 1) // Reference implementation always returns n + 1.
	l.PushInteger(int64(c))
	return 2, nil
}

func utf8Codepoint(ctx context.Context, l *State) (int, error) {
	s, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	iArg := int64(1)
	if !l.IsNoneOrNil(2) {
		var err error
		iArg, err = CheckInteger(l, 2)
		if err != nil {
			return 0, err
		}
	}
	var i int
	switch {
	case iArg == 0 || iArg < -int64(len(s)):
		return 0, NewArgError(l, 2, "out of bounds")
	case iArg < 0:
		i = len(s) + int(iArg)
	default:
		i = int(iArg - 1)
	}
	jArg := int64(iArg)
	if !l.IsNoneOrNil(3) {
		var err error
		jArg, err = CheckInteger(l, 3)
		if err != nil {
			return 0, err
		}
	}
	var j int
	switch {
	case jArg > int64(len(s)):
		return 0, NewArgError(l, 3, "out of bounds")
	case jArg < -int64(len(s)):
		j = 0
	case jArg < 0:
		j = len(s) + int(jArg)
	default:
		j = int(jArg - 1)
	}
	decode := utf8.DecodeRuneInString
	if l.ToBoolean(4) {
		decode = decodeLaxUTF8RuneInString
	}

	if i > j {
		return 0, nil
	}
	if !l.CheckStack(j - i + 1) {
		return 0, fmt.Errorf("%sstack overflow (string slice too long)", Where(l, 1))
	}
	n := 0
	for i <= j && i < len(s) {
		c, size := decode(s[i:])
		if c == utf8.RuneError && size == 1 {
			return 0, errInvalidUTF8
		}
		l.PushInteger(int64(c))
		n++
		i += size
	}
	return n, nil
}

func utf8Len(ctx context.Context, l *State) (int, error) {
	s, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	iArg := int64(1)
	if !l.IsNoneOrNil(2) {
		var err error
		iArg, err = CheckInteger(l, 2)
		if err != nil {
			return 0, err
		}
	}
	var i int
	switch {
	case iArg == 0 || iArg > int64(len(s))+1 || iArg < -int64(len(s)):
		return 0, NewArgError(l, 2, "initial position out of bounds")
	case iArg < 0:
		i = int(int64(len(s)) + iArg)
	default:
		i = int(iArg) - 1
	}
	jArg := int64(-1)
	if !l.IsNoneOrNil(3) {
		var err error
		jArg, err = CheckInteger(l, 3)
		if err != nil {
			return 0, err
		}
	}
	var j int
	switch {
	case jArg < 0:
		j = int(max(int64(len(s))+jArg, 0))
	case jArg > int64(len(s)):
		return 0, NewArgError(l, 3, "final position out of bounds")
	default:
		j = int(jArg) - 1
	}
	decode := utf8.DecodeRuneInString
	if l.ToBoolean(4) {
		decode = decodeLaxUTF8RuneInString
	}

	n := 0
	for i <= j && i < len(s) {
		c, size := decode(s[i:])
		if c == utf8.RuneError && size == 1 {
			l.PushNil()
			l.PushInteger(int64(1 + i))
			return 2, nil
		}
		i += size
		n++
	}
	l.PushInteger(int64(n))
	return 1, nil
}

func utf8Offset(ctx context.Context, l *State) (int, error) {
	s, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	n, err := CheckInteger(l, 2)
	if err != nil {
		return 0, err
	}
	i := int64(1)
	if n < 0 {
		i = int64(len(s)) + 1
	}
	if !l.IsNoneOrNil(3) {
		var err error
		i, err = CheckInteger(l, 3)
		if err != nil {
			return 0, err
		}
	}
	i--
	if i < 0 || i > int64(len(s)) {
		return 0, NewArgError(l, 3, "position out of bounds")
	}

	if n == 0 {
		for 0 < i && i < int64(len(s)) && !utf8.RuneStart(s[i]) {
			i--
		}
		l.PushInteger(i + 1)
		return 1, nil
	}

	if i < int64(len(s)) && !utf8.RuneStart(s[i]) {
		return 0, fmt.Errorf("%sinitial position is a continuation byte", Where(l, 1))
	}

	if n < 0 {
		for n < 0 && i > 0 {
			for {
				i--
				if i <= 0 || utf8.RuneStart(s[i]) {
					break
				}
			}
			n++
		}
	} else {
		n-- // Do not move for first character.
		for n > 0 && i < int64(len(s)) {
			for {
				i++
				if i >= int64(len(s)) || utf8.RuneStart(s[i]) {
					break
				}
			}
			n--
		}
	}
	if n == 0 {
		l.PushInteger(i + 1)
	} else {
		l.PushNil()
	}
	return 1, nil
}

func decodeLaxUTF8RuneInString(s string) (r rune, size int) {
	if len(s) == 0 {
		return utf8.RuneError, 0
	}
	c := s[0]
	if isASCII(rune(c)) {
		return rune(c), 1
	}
	// Read continuation bytes.
	for size = 1; c&0x40 != 0; c, size = c<<1, size+1 {
		if size >= len(s) {
			return utf8.RuneError, 1
		}
		cc := s[size]
		if utf8.RuneStart(cc) {
			return utf8.RuneError, 1
		}
		r = (r << 6) | rune(cc&0x3f)
	}
	// Add first byte.
	r |= rune(c&0x7f) << ((size - 1) * 5)

	limits := [...]rune{1<<31 - 1, 0x80, 0x800, 0x10000, 0x200000, 0x4000000}
	if size-1 >= len(limits) || r > maxUTF8 || r < limits[size-1] {
		return utf8.RuneError, 1
	}
	return r, size
}

// writeRune encodes a rune as UTF-8,
// permitting runes up to 1<<31 - 1.
func writeRune(sb *strings.Builder, c rune) {
	if isASCII(c) {
		sb.WriteByte(byte(c))
		return
	}

	var buf [6]byte
	firstByteMax := byte(0x3f)
	n := 1
	for {
		buf[len(buf)-n] = byte(0x80 | (c & 0x3f))
		n++
		c >>= 6
		firstByteMax >>= 1
		if c <= rune(firstByteMax) {
			break
		}
	}
	buf[len(buf)-n] = (^firstByteMax << 1) | byte(c)
	sb.Write(buf[len(buf)-n:])
}

func isASCII(c rune) bool {
	return 0 <= c && c < 0x80
}

var errInvalidUTF8 = errors.New("invalid UTF-8 code")
