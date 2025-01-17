// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"

	"zb.256lights.llc/pkg/internal/luacode"
	"zb.256lights.llc/pkg/internal/lualex"
	"zb.256lights.llc/pkg/sets"
)

// StringLibraryName is the conventional identifier for the [string manipulation library].
//
// [string manipulation library]: https://www.lua.org/manual/5.4/manual.html#6.4
const StringLibraryName = "string"

// OpenString is a [Function] that loads the [string manipulation library].
// This function is intended to be used as an argument to [Require].
//
// # Differences from de facto C implementation
//
//   - Patterns do not support backreferences (i.e. %0 - %9),
//     balances (i.e. %b), or frontiers (%f).
//     Attempting to use any of these pattern items will raise an error.
//   - Character sets with classes in ranges (e.g. [%a-z]) raise an error
//     instead of silently exhibiting undefined behavior.
//
// [string manipulation library]: https://www.lua.org/manual/5.4/manual.html#6.4
func OpenString(ctx context.Context, l *State) (int, error) {
	NewLib(l, map[string]Function{
		"byte": stringByte,
		"char": stringChar,
		// "dump":     stringDump,
		"find":     stringFind,
		"format":   stringFormat,
		"gmatch":   stringGMatch,
		"gsub":     stringGSub,
		"len":      stringLen,
		"lower":    stringLower,
		"match":    stringMatch,
		"pack":     stringPack,
		"packsize": stringPackSize,
		"rep":      stringRepeat,
		"reverse":  stringReverse,
		"sub":      stringSub,
		"unpack":   stringUnpack,
		"upper":    stringUpper,
	})

	// Create string metatable.
	operators := []luacode.ArithmeticOperator{
		luacode.Add,
		luacode.Subtract,
		luacode.Multiply,
		luacode.Modulo,
		luacode.Power,
		luacode.Divide,
		luacode.IntegerDivide,
		luacode.UnaryMinus,
	}
	metaMethods := make(map[string]Function, len(operators)+1)
	metaMethods[luacode.TagMethodIndex.String()] = nil
	for _, op := range operators {
		op := op // Capture constant instead of loop variable.
		metaMethods[op.TagMethod().String()] = func(ctx context.Context, l *State) (int, error) {
			return stringArithmetic(ctx, l, op)
		}
	}

	NewLib(l, metaMethods)
	l.PushValue(-2)
	l.RawSetField(-2, "__index")

	// Set string metatable.
	l.PushString("")
	l.PushValue(-2)
	l.SetMetatable(-2)

	l.Pop(2) // Pop string and metatable.

	return 1, nil
}

func stringByte(ctx context.Context, l *State) (int, error) {
	s, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	pi := int64(1)
	if !l.IsNoneOrNil(2) {
		var err error
		pi, err = CheckInteger(l, 2)
		if err != nil {
			return 0, err
		}
	}
	start, inBounds := stringIndexArg(pi, len(s))
	if !inBounds {
		return 0, nil
	}
	end, err := stringEndArg(l, 3, pi, len(s))
	if err != nil {
		return 0, err
	}
	if start >= end {
		return 0, nil
	}
	n := end - start
	if !l.CheckStack(n) {
		return 0, fmt.Errorf("%sstring slice too long", Where(l, 1))
	}
	for i := range n {
		l.PushInteger(int64(s[start+i]))
	}
	return n, nil
}

func stringChar(ctx context.Context, l *State) (int, error) {
	n := l.Top()
	sb := new(strings.Builder)
	sb.Grow(n)
	for i := range n {
		c, err := CheckInteger(l, 1+i)
		if err != nil {
			return 0, err
		}
		if c < 0 || c > 0xff {
			return 0, NewArgError(l, i, "value out of range")
		}
		sb.WriteByte(byte(c))
	}
	l.PushString(sb.String())
	return 1, nil
}

func stringFind(ctx context.Context, l *State) (int, error) {
	s, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	pattern, err := CheckString(l, 2)
	if err != nil {
		return 0, err
	}
	initArg := int64(1)
	if !l.IsNoneOrNil(3) {
		var err error
		initArg, err = CheckInteger(l, 3)
		if err != nil {
			return 0, err
		}
	}
	init, initOK := stringIndexArg(initArg, len(s))
	if !initOK {
		l.PushNil()
		return 1, nil
	}

	if l.ToBoolean(4) || !strings.ContainsAny(pattern, `^$*+?.([%-`) {
		// Plain search.
		i := strings.Index(s[init:], pattern)
		if i == -1 {
			l.PushNil()
			return 1, nil
		}
		l.PushInteger(int64(init) + int64(i) + 1)
		l.PushInteger(int64(init) + int64(i) + int64(len(pattern)))
		return 2, nil
	}

	re, positionCaptures, err := patternToRegexp(pattern)
	if err != nil {
		return 0, fmt.Errorf("%s%v", Where(l, 1), err)
	}
	matches := re.FindStringSubmatchIndex(s[init:])
	if len(matches) == 0 {
		l.PushNil()
		return 1, nil
	}
	l.PushInteger(int64(init) + int64(matches[0]) + 1)
	l.PushInteger(int64(init) + int64(matches[1]) + 1)
	n, err := pushSubmatches(l, init, matches[2:], positionCaptures)
	if err != nil {
		return 0, fmt.Errorf("%s%v", Where(l, 1), err)
	}
	return 2 + n, nil
}

func stringFormat(ctx context.Context, l *State) (int, error) {
	format, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}

	top := l.Top()
	arg := 2
	sb := new(strings.Builder)
	sctx := make(sets.Set[string])
	for len(format) > 0 {
		var spec string
		var err error
		spec, format, err = cutFormatSpecifier(format)
		if err != nil {
			return 0, NewArgError(l, 1, err.Error())
		}
		if !strings.HasPrefix(spec, "%") {
			sb.WriteString(spec)
			// Because we're writing portions of the format string,
			// include its context in the result.
			sctx.AddSeq(l.StringContext(1).All())
			continue
		}
		switch c := spec[len(spec)-1]; c {
		case 'd', 'i':
			if arg > top {
				return 0, NewArgError(l, arg, "no value")
			}
			n, err := CheckInteger(l, arg)
			if err != nil {
				return 0, err
			}
			if c != 'd' {
				spec = spec[:len(spec)-1] + "d"
			}
			fmt.Fprintf(sb, spec, n)
		case 'o', 'x', 'X':
			if arg > top {
				return 0, NewArgError(l, arg, "no value")
			}
			n, err := CheckInteger(l, arg)
			if err != nil {
				return 0, err
			}
			fmt.Fprintf(sb, spec, uint64(n))
		case 'c':
			if arg > top {
				return 0, NewArgError(l, arg, "no value")
			}
			n, err := CheckInteger(l, arg)
			if err != nil {
				return 0, err
			}
			options := spec[1 : len(spec)-1]
			widthString, leftJustify := strings.CutPrefix(options, "-")
			var width int
			if widthString != "" {
				var err error
				width, err = strconv.Atoi(widthString)
				if err != nil {
					return 0, err
				}
			}

			if !leftJustify {
				for range width - 1 {
					sb.WriteByte(' ')
				}
			}
			sb.WriteByte(byte(n))
			if leftJustify {
				for range width - 1 {
					sb.WriteByte(' ')
				}
			}
		case 'u':
			if arg > top {
				return 0, NewArgError(l, arg, "no value")
			}
			n, err := CheckInteger(l, arg)
			if err != nil {
				return 0, err
			}
			spec = spec[:len(spec)-1] + "d"
			fmt.Fprintf(sb, spec, uint64(n))
		case 'a', 'A', 'e', 'E', 'f', 'g', 'G':
			if arg > top {
				return 0, NewArgError(l, arg, "no value")
			}
			n, err := CheckNumber(l, arg)
			if err != nil {
				return 0, err
			}
			if c == 'a' || c == 'A' {
				// Hexadecimal float. Go uses 'x'/'X'.
				spec = spec[:len(spec)-1] + string(c+('X'-'A'))
			}
			// TODO(now): Special floats.
			fmt.Fprintf(sb, spec, n)
		case 'p':
			if arg > top {
				return 0, NewArgError(l, arg, "no value")
			}
			p := l.ID(arg)
			if p == 0 {
				spec = spec[:len(spec)-1] + "s"
				fmt.Fprintf(sb, spec, "(null)")
			} else {
				spec = "%#" + spec[1:len(spec)-1] + "x"
				fmt.Fprintf(sb, spec, p)
			}
		case 'q':
			if arg > top {
				return 0, NewArgError(l, arg, "no value")
			}
			k, ok := ToConstant(l, arg)
			if !ok {
				return 0, NewArgError(l, arg, "value has no literal form")
			}
			sb.WriteString(k.String())
			if k.IsString() {
				sctx.AddSeq(l.StringContext(arg).All())
			}
		case 's':
			if arg > top {
				return 0, NewArgError(l, arg, "no value")
			}
			s, argContext, err := ToString(ctx, l, arg)
			if err != nil {
				return 0, err
			}
			fmt.Fprintf(sb, spec, s)
			sctx.AddSeq(argContext.All())
		default:
			sb.WriteByte(c)
			// Because we're writing portions of the format string,
			// include its context in the result.
			sctx.AddSeq(l.StringContext(1).All())
			continue // Don't advance arg.
		}
		arg++
	}

	l.PushStringContext(sb.String(), sctx)
	return 1, nil
}

func cutFormatSpecifier(s string) (spec, tail string, err error) {
	if s == "" {
		return "", "", nil
	}
	if s[0] != '%' {
		return s[:1], s[1:], nil
	}
	optionsStart := 1
	optionsLength := findRunEnd(s[optionsStart:], "-+# 0123456789.")
	if optionsLength >= 22 {
		return s, "", fmt.Errorf("invalid format (too long)")
	}
	optionsEnd := optionsStart + optionsLength
	if optionsEnd >= len(s) {
		return s, "", fmt.Errorf("invalid conversion '%s' to 'format'", s)
	}
	options := s[optionsStart:optionsEnd]
	end := optionsEnd + 1
	spec, tail = s[:end], s[end:]

	// Validate format specifier.
	ok := true
	var allowedFlags string
	precisionAllowed := true
	switch s[optionsEnd] {
	case 'c', 'p':
		allowedFlags = "-"
		precisionAllowed = false
	case 's':
		allowedFlags = "-"
	case 'd', 'i':
		allowedFlags = "-+0 "
	case 'u':
		allowedFlags = "-0"
	case 'o', 'x', 'X':
		allowedFlags = "-#0"
	case 'a', 'A', 'e', 'E', 'f', 'g', 'G':
		allowedFlags = "-+#0 "
	case 'q', '%':
		if optionsLength != 0 {
			return spec, tail, fmt.Errorf("specifier '%s' cannot have modifiers", spec)
		}
	default:
		ok = false
	}
	if !ok || !checkFormatOptions(options, allowedFlags, precisionAllowed) {
		return spec, tail, fmt.Errorf("invalid conversion '%s' to 'format'", spec)
	}
	return spec, tail, nil
}

func checkFormatOptions(options string, allowedFlags string, precisionAllowed bool) bool {
	flagsLength := findRunEnd(options, allowedFlags)
	width := options[flagsLength:]
	if strings.HasPrefix(width, "0") {
		return false
	}
	const digits = "0123456789"
	digits1 := findRunEnd(width, digits)
	if digits1 > 2 {
		return false
	}
	if digits1 == len(width) {
		return true
	}
	if !precisionAllowed || width[digits1] != '.' {
		return false
	}
	precision := width[digits1+1:]
	digits2 := findRunEnd(precision, digits)
	return digits2 == len(precision) && digits2 <= 2
}

func findRunEnd(s string, charset string) int {
	n := 0
	for n < len(s) && strings.IndexByte(charset, s[n]) != -1 {
		n++
	}
	return n
}

const gsubReplacementArg = 3

func stringGMatch(ctx context.Context, l *State) (int, error) {
	s, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	pattern, err := CheckString(l, 2)
	if err != nil {
		return 0, err
	}
	initArg := int64(1)
	if !l.IsNoneOrNil(3) {
		var err error
		initArg, err = CheckInteger(l, 3)
		if err != nil {
			return 0, err
		}
	}
	init, initOK := stringIndexArg(initArg, len(s))
	if !initOK {
		l.PushNil()
		return 1, nil
	}

	re, positionCaptures, err := patternToRegexp(pattern)
	if err != nil {
		return 0, fmt.Errorf("%s%v", Where(l, 1), err)
	}
	matches := re.FindAllStringSubmatchIndex(s[init:], -1)

	l.PushValue(1)
	l.PushClosure(1, func(ctx context.Context, l *State) (int, error) {
		if len(matches) == 0 {
			return 0, nil
		}

		m := matches[0]
		matches = matches[1:]

		positionCapturesArg := positionCaptures
		if len(m) > 2 {
			// Only return captures.
			m = m[2:]
		} else {
			// Whole match.
			positionCapturesArg = nil
		}
		l.SetTop(0)
		l.PushValue(UpvalueIndex(1))
		n, err := pushSubmatches(l, init, m, positionCapturesArg)
		if err != nil {
			return 0, fmt.Errorf("%s%v", Where(l, 1), err)
		}
		return n, nil
	})
	return 1, nil
}

func stringGSub(ctx context.Context, l *State) (int, error) {
	src, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	srcContext := l.StringContext(1)
	pattern, err := CheckString(l, 2)
	if err != nil {
		return 0, err
	}
	maxReplacements := -1
	if !l.IsNoneOrNil(4) {
		maxReplacementsArg, err := CheckInteger(l, 4)
		if err != nil {
			return 0, err
		}
		if maxReplacementsArg <= 0 {
			// Return original string.
			l.PushValue(1)
			l.PushInteger(0)
			return 2, nil
		}
		if uint64(maxReplacementsArg) < uint64(len(src)+1) {
			maxReplacements = int(maxReplacementsArg)
		}
	}

	var replace gsubReplaceFunc
	switch replacementType := l.Type(gsubReplacementArg); replacementType {
	case TypeNumber, TypeString:
		replace = gsubString(l)
	case TypeFunction:
		replace = gsubFunction
	case TypeTable:
		replace = gsubTable
	default:
		return 0, NewTypeError(l, 3, "string/function/table")
	}

	re, positionCaptures, err := patternToRegexp(pattern)
	if err != nil {
		return 0, fmt.Errorf("%s%v", Where(l, 1), err)
	}

	state := &gsubState{
		src:              src,
		srcContext:       srcContext,
		positionCaptures: positionCaptures,
		result:           new(strings.Builder),
		resultContext:    make(sets.Set[string]),
	}
	// TODO(https://go.dev/issue/61902): Iterate over matches instead of allocating.
	matches := re.FindAllStringSubmatchIndex(src, maxReplacements)
	lastMatchEnd := 0
	changed := false
	for _, match := range matches {
		state.copySource(lastMatchEnd, match[0])
		lastMatchEnd = match[1]
		changedByMatch, err := replace(ctx, l, state, match)
		if err != nil {
			return 0, err
		}
		changed = changed || changedByMatch
	}
	state.copySource(lastMatchEnd, len(src))

	if !changed {
		l.PushValue(1)
	} else {
		l.PushStringContext(state.result.String(), state.resultContext)
	}
	l.PushInteger(int64(len(matches)))
	return 2, nil
}

type gsubState struct {
	src             string
	srcContext      sets.Set[string]
	addedSrcContext bool

	positionCaptures *sets.Bit

	result        *strings.Builder
	resultContext sets.Set[string]
}

func (state *gsubState) copySource(start, end int) {
	if start < end {
		state.result.WriteString(state.src[start:end])
		if !state.addedSrcContext {
			state.resultContext.AddSeq(state.srcContext.All())
			state.addedSrcContext = true
		}
	}
}

type gsubReplaceFunc func(ctx context.Context, l *State, state *gsubState, match []int) (changed bool, err error)

func gsubString(l *State) gsubReplaceFunc {
	replacementString, _ := l.ToString(gsubReplacementArg)
	replacementContext := l.StringContext(gsubReplacementArg)
	addedReplacementContext := false

	return func(ctx context.Context, l *State, state *gsubState, match []int) (changed bool, err error) {
		state.result.Grow(len(replacementString))
		for i := 0; i < len(replacementString); i++ {
			if b := replacementString[i]; b != '%' {
				state.result.WriteByte(b)
				if !addedReplacementContext {
					state.resultContext.AddSeq(replacementContext.All())
					addedReplacementContext = true
				}
				continue
			}
			i++

			var b byte
			if i < len(replacementString) {
				b = replacementString[i]
			}
			switch {
			case b == '%':
				state.result.WriteByte('%')
				if !addedReplacementContext {
					state.resultContext.AddSeq(replacementContext.All())
					addedReplacementContext = true
				}
			case b == '0':
				state.copySource(match[0], match[1])
			case '1' <= b && b <= '9':
				i := int(b - '0')
				if i*2 > len(match) {
					return false, fmt.Errorf("%sinvalid capture index %%%d", Where(l, 1), i)
				}
				start, end := match[i*2], match[i*2+1]
				if state.positionCaptures.Has(uint(i - 1)) {
					s, _ := luacode.IntegerValue(int64(start)).Unquoted()
					state.result.WriteString(s)
				} else {
					state.copySource(start, end)
				}
			default:
				return false, fmt.Errorf("%sinvalid use of %% in replacement string", Where(l, 1))
			}
		}

		return replacementString != "%0", nil
	}
}

func gsubTable(ctx context.Context, l *State, state *gsubState, match []int) (changed bool, err error) {
	defer l.SetTop(l.Top())
	matchStart, matchEnd := match[0], match[1]
	if len(match) > 2 {
		match = match[2:]
	}
	l.PushStringContext(state.src[match[0]:match[1]], state.srcContext)
	if _, err := l.Table(ctx, gsubReplacementArg); err != nil {
		return false, err
	}
	if !l.ToBoolean(-1) {
		state.copySource(matchStart, matchEnd)
		return false, nil
	}
	if t := l.Type(-1); t != TypeString {
		return false, fmt.Errorf("%sinvalid replacement value (a %s)", Where(l, 1), t.String())
	}
	s, _ := l.ToString(-1)
	state.result.WriteString(s)
	state.resultContext.AddSeq(l.StringContext(-1).All())
	return true, nil
}

func gsubFunction(ctx context.Context, l *State, state *gsubState, match []int) (changed bool, err error) {
	defer l.SetTop(l.Top())
	matchStart, matchEnd := match[0], match[1]
	if len(match) > 2 {
		match = match[2:]
	}
	l.PushValue(gsubReplacementArg)
	n, err := pushSubmatches(l, 0, match, nil)
	if err != nil {
		return false, err
	}
	if err := l.Call(ctx, n, 1, 0); err != nil {
		return false, err
	}
	if !l.ToBoolean(-1) {
		state.copySource(matchStart, matchEnd)
		return false, nil
	}
	if t := l.Type(-1); t != TypeString {
		return false, fmt.Errorf("%sinvalid replacement value (a %s)", Where(l, 1), t.String())
	}
	s, _ := l.ToString(-1)
	state.result.WriteString(s)
	state.resultContext.AddSeq(l.StringContext(-1).All())
	return true, nil
}

func stringLen(ctx context.Context, l *State) (int, error) {
	s, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	l.PushInteger(int64(len(s)))
	return 1, nil
}

func stringLower(ctx context.Context, l *State) (int, error) {
	s, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	sctx := l.StringContext(1)
	l.PushStringContext(strings.ToLower(s), sctx)
	return 1, nil
}

func stringMatch(ctx context.Context, l *State) (int, error) {
	s, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	pattern, err := CheckString(l, 2)
	if err != nil {
		return 0, err
	}
	initArg := int64(1)
	if !l.IsNoneOrNil(3) {
		var err error
		initArg, err = CheckInteger(l, 3)
		if err != nil {
			return 0, err
		}
	}
	init, initOK := stringIndexArg(initArg, len(s))
	if !initOK {
		l.PushNil()
		return 1, nil
	}

	re, positionCaptures, err := patternToRegexp(pattern)
	if err != nil {
		return 0, fmt.Errorf("%s%v", Where(l, 1), err)
	}
	matches := re.FindStringSubmatchIndex(s[init:])
	switch {
	case len(matches) == 0:
		l.PushNil()
		return 1, nil
	case len(matches) > 2:
		// Only return captures.
		n, err := pushSubmatches(l, init, matches[2:], positionCaptures)
		if err != nil {
			return 0, fmt.Errorf("%s%v", Where(l, 1), err)
		}
		return n, nil
	default:
		// No captures; return whole match.
		n, err := pushSubmatches(l, init, matches, nil)
		if err != nil {
			return 0, fmt.Errorf("%s%v", Where(l, 1), err)
		}
		return n, nil
	}
}

func stringRepeat(ctx context.Context, l *State) (int, error) {
	s, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	n, err := CheckInteger(l, 2)
	if err != nil {
		return 0, err
	}
	var sep string
	if !l.IsNoneOrNil(3) {
		var err error
		sep, err = CheckString(l, 3)
		if err != nil {
			return 0, err
		}
	}

	if n <= 0 {
		l.PushString("")
		return 1, nil
	}
	if len(s)+len(sep) < len(s) || int64(len(s)+len(sep)) > math.MaxInt/n {
		return 0, fmt.Errorf("%sresulting string too large", Where(l, 1))
	}
	sb := new(strings.Builder)
	sb.Grow(int(n)*len(s) + int(n-1)*len(sep))
	for range n - 1 {
		sb.WriteString(s)
		sb.WriteString(sep)
	}
	sb.WriteString(s)
	l.PushStringContext(sb.String(), l.StringContext(1))
	return 1, nil
}

func stringReverse(ctx context.Context, l *State) (int, error) {
	s, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	sctx := l.StringContext(1)
	sb := new(strings.Builder)
	sb.Grow(len(s))
	for i := len(s) - 1; i >= 0; i-- {
		sb.WriteByte(s[i])
	}
	l.PushStringContext(sb.String(), sctx)
	return 1, nil
}

func stringSub(ctx context.Context, l *State) (int, error) {
	s, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	sctx := l.StringContext(1)
	iArg, err := CheckInteger(l, 2)
	if err != nil {
		return 0, err
	}
	i, inBounds := stringIndexArg(iArg, len(s))
	if !inBounds {
		l.PushStringContext("", sctx)
		return 1, nil
	}
	j, err := stringEndArg(l, 3, int64(len(s)), len(s))
	if err != nil {
		return 0, err
	}
	if i >= j {
		l.PushStringContext("", sctx)
		return 1, nil
	}

	l.PushStringContext(s[i:j], sctx)
	return 1, nil
}

func stringUpper(ctx context.Context, l *State) (int, error) {
	s, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	sctx := l.StringContext(1)
	l.PushStringContext(strings.ToUpper(s), sctx)
	return 1, nil
}

func patternToRegexp(pattern string) (re *regexp.Regexp, positionCaptures *sets.Bit, err error) {
	sb := new(strings.Builder)
	sb.Grow(len(pattern))

	pattern, anchorStart := strings.CutPrefix(pattern, "^")
	if anchorStart {
		sb.WriteByte('^')
	}

	for i, capture := 0, 0; i < len(pattern); {
		// Character class.
		switch pattern[i] {
		case '%':
			runeStart := i + 1
			c, runeSize := utf8.DecodeRuneInString(pattern[runeStart:])
			if runeSize == 0 {
				return nil, nil, errors.New("malformed pattern (ends with '%')")
			}
			runeEnd := runeStart + runeSize
			if c == utf8.RuneError {
				sb.WriteString(pattern[runeStart:runeEnd])
			} else if '0' <= c && c <= '9' {
				return nil, nil, errors.New("patterns with backreferences not supported")
			} else if c == 'b' {
				return nil, nil, errors.New("patterns with balances not supported")
			} else if c == 'f' {
				return nil, nil, errors.New("patterns with frontiers not supported")
			} else if cre := characterClasses[c]; cre != "" {
				sb.WriteString("[")
				sb.WriteString(cre)
				sb.WriteString("]")
			} else if cre = characterClasses[toLowerASCII(c)]; cre != "" {
				sb.WriteString("[^")
				sb.WriteString(cre)
				sb.WriteString("]")
			} else {
				if isRegexpSpecial(c) {
					sb.WriteByte('\\')
				}
				sb.WriteString(pattern[runeStart:runeEnd])
			}
			i = runeEnd
		case '(':
			sb.WriteByte(pattern[i])
			i++
			if i < len(pattern) && pattern[i] == ')' {
				// Position capture. Go ahead and process.
				sb.WriteByte(')')
				i++
				if positionCaptures == nil {
					positionCaptures = new(sets.Bit)
				}
				positionCaptures.Add(uint(capture))
			}
			capture++
			// Captures are not character classes,
			// so no modifiers.
			continue
		case ')':
			sb.WriteByte(pattern[i])
			i++
			// Captures are not character classes,
			// so no modifiers.
			continue
		case '.':
			sb.WriteByte(pattern[i])
			i++
		case '[':
			n, err := characterSet(sb, pattern[i:])
			if err != nil {
				return nil, nil, err
			}
			i += n
		case '$':
			if i == len(pattern)-1 {
				sb.WriteByte('$')
				i++
				continue
			}
			fallthrough
		default:
			c, runeSize := utf8.DecodeRuneInString(pattern[i:])
			if isRegexpSpecial(c) {
				sb.WriteByte('\\')
			}
			sb.WriteString(pattern[i : i+runeSize])
			i += runeSize
		}

		// Modifier.
		if i < len(pattern) {
			switch pattern[i] {
			case '*', '+', '?':
				sb.WriteByte(pattern[i])
				i++
			case '-':
				sb.WriteString("*?")
				i++
			}
		}
	}

	re, err = regexp.Compile(sb.String())
	return re, positionCaptures, err
}

var characterClasses = map[rune]string{
	'a': `\pL`,             // letters
	'A': `\PL`,             // not letters
	'c': `\p{Cc}`,          // control characters
	'C': `\P{Cc}`,          // not control characters
	'd': `\p{Nd}`,          // digits
	'D': `\P{Nd}`,          // not digits
	'g': `\pL\pM\pN\pP\pS`, // printable characters except space
	'l': `\p{Ll}`,          // lowercase letters
	'L': `\P{Ll}`,          // not lowercase letters
	'p': `\pP`,             // punctuation
	'P': `\PP`,             // not punctuation
	's': `\pZ\t\n\r`,       // space
	'u': `\p{Lu}`,          // uppercase letters
	'U': `\P{Lu}`,          // not uppercase letters
	'w': `\pL\pN`,          // alphanumeric characters
	'x': `0-9a-fA-F`,       // hexadecimal digits
}

func characterSet(sb *strings.Builder, pattern string) (end int, err error) {
	pattern, ok := strings.CutPrefix(pattern, "[")
	if !ok {
		return end, errors.New("character set must start with '['")
	}
	end += len("[")
	pattern, negate := strings.CutPrefix(pattern, "^")
	if negate {
		end += len("^")
	}

	// Scan through pattern once to validate and to determine structure to use.
	setLen := 0
	var alternatives *strings.Builder
	// Patterns starting with an end bracket treat that character as a normal character.
	for setLen < len(pattern) && (pattern[setLen] != ']' || setLen == 0) {
		switch pattern[setLen] {
		case '%':
			setLen++
			c, runeSize := utf8.DecodeRuneInString(pattern[setLen:])
			setLen += runeSize
			if runeSize == 0 {
				return end + setLen, errors.New("malformed pattern (ends with '%')")
			}
			if characterClasses[c] == "" && characterClasses[toLowerASCII(c)] != "" {
				// There isn't a single character class that maps to the Lua class.
				// For non-negated sets, we can represent this as [...]|[^class]...
				if negate {
					// ... unless this is a negated set.
					// Because other items in the set are subtractive,
					// we can't use this trick.
					return end + setLen, fmt.Errorf("cannot use %%%c in negated set", c)
				}
				if alternatives == nil {
					alternatives = new(strings.Builder)
				}
			}
			if strings.HasPrefix(pattern[setLen:], "-") {
				return end + setLen + 1, fmt.Errorf("malformed pattern (%%%c used in range)", c)
			}
		default:
			_, runeSize := utf8.DecodeRuneInString(pattern[setLen:])
			setLen += runeSize
			if strings.HasPrefix(pattern[setLen:], "-%") {
				classStart := setLen + len("-%")
				c, runeSize := utf8.DecodeRuneInString(pattern[classStart:])
				if runeSize == 0 {
					return end + classStart, errors.New("malformed pattern ('%' used in range)")
				}
				return end + classStart + runeSize, fmt.Errorf("malformed pattern (%%%c used in range)", c)
			}
		}
	}
	if !strings.HasPrefix(pattern[setLen:], "]") {
		return end, errors.New("malformed pattern (missing ']')")
	}
	end += setLen + len("]")

	// Now write the regular expression.
	if alternatives != nil {
		sb.WriteString("(?:")
	}
	sb.WriteString("[")
	if negate {
		sb.WriteString("^")
	}
	for i := 0; i < setLen; {
		switch pattern[i] {
		case '[', ']', '\\':
			sb.WriteByte('\\')
			sb.WriteByte(pattern[i])
			i++
		case '%':
			i++
			c, runeSize := utf8.DecodeRuneInString(pattern[i:])
			if cre := characterClasses[c]; cre != "" {
				sb.WriteString(cre)
			} else if cre = characterClasses[toLowerASCII(c)]; cre != "" {
				alternatives.WriteString("|[^")
				alternatives.WriteString(cre)
				alternatives.WriteString("]")
			} else {
				if c == '\\' || c == '[' || c == ']' || c == '-' {
					sb.WriteByte('\\')
				}
				sb.WriteString(pattern[i : i+runeSize])
			}
			i += runeSize
		default:
			// Hyphens are already validated for character classes,
			// so they aren't escaped.
			_, runeSize := utf8.DecodeRuneInString(pattern[i:])
			runeEnd := i + runeSize
			sb.WriteString(pattern[i:runeEnd])
			i = runeEnd
		}
	}
	sb.WriteString("]")
	if alternatives != nil {
		sb.WriteString(alternatives.String())
		sb.WriteString(")")
	}

	return end, nil
}

func isRegexpSpecial(c rune) bool {
	return strings.ContainsRune(`\.+*?()|[]{}^$`, c)
}

func pushSubmatches(l *State, init int, submatches []int, positionCaptures *sets.Bit) (int, error) {
	const sArg = 1
	s, _ := l.ToString(sArg)
	sctx := l.StringContext(sArg)

	captureCount := len(submatches) / 2
	if !l.CheckStack(captureCount) {
		return 0, errors.New("too many captures")
	}
	for i, capture := 0, 0; i < len(submatches); i, capture = i+2, capture+1 {
		if positionCaptures.Has(uint(capture)) {
			l.PushInteger(int64(init) + int64(submatches[i]) + 1)
		} else {
			start := init + submatches[i]
			end := init + submatches[i+1]
			l.PushStringContext(s[start:end], sctx)
		}
	}
	return captureCount, nil
}

// maxPackIntegerSize is the maximum size of integers
// that can be written with [stringPack] et al.
const maxPackIntegerSize = 16

func stringPackSize(ctx context.Context, l *State) (int, error) {
	format, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}

	p := newPackParser(format)
	var totalSize int
	for {
		_, size, pad, hasFixedSize, err := p.next(totalSize)
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, NewArgError(l, 1, err.Error())
		}
		if !hasFixedSize {
			return 0, NewArgError(l, 1, "variable-length format")
		}
		size += pad
		if totalSize > math.MaxInt-size {
			return 0, NewArgError(l, 1, "format result too large")
		}
		totalSize += size
	}

	l.PushInteger(int64(totalSize))
	return 1, nil
}

func stringPack(ctx context.Context, l *State) (int, error) {
	format, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}

	p := newPackParser(format)
	sb := new(strings.Builder)
	sctx := make(sets.Set[string])
	arg := 2
	var buf [maxPackIntegerSize]byte
	for {
		opt, size, pad, _, err := p.next(sb.Len())
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, NewArgError(l, 1, err.Error())
		}
		sb.Grow(pad)
		for range pad {
			sb.WriteByte(0)
		}
		switch opt {
		case 'b', 'h', 'l', 'j', 'i':
			// Signed integer.
			n, err := CheckInteger(l, arg)
			if err != nil {
				return 0, err
			}
			if size < 8 {
				if limit := int64(1) << (size*8 - 1); n < -limit || n >= limit {
					return 0, NewArgError(l, arg, "integer overflow")
				}
			}
			arg++
			packInteger(buf[:size], uint64(n), p.bigEndian)
			if n < 0 && size > 8 {
				// Sign-extend.
				fillSlice := buf[8:size]
				for i := range fillSlice {
					fillSlice[i] = 0xff
				}
			}
			sb.Write(buf[:size])
		case 'B', 'H', 'L', 'J', 'T', 'I':
			// Unsigned integer.
			n, err := CheckInteger(l, arg)
			if err != nil {
				return 0, err
			}
			if size < 8 && uint64(n) >= uint64(1)<<(size*8) {
				return 0, NewArgError(l, arg, "unsigned overflow")
			}
			arg++
			packInteger(buf[:size], uint64(n), p.bigEndian)
			sb.Write(buf[:size])
		case 'f':
			// float32
			f, err := CheckNumber(l, arg)
			if err != nil {
				return 0, err
			}
			arg++
			packInteger(buf[:4], uint64(math.Float32bits(float32(f))), p.bigEndian)
			sb.Write(buf[:4])
		case 'd', 'n':
			// float64
			f, err := CheckNumber(l, arg)
			if err != nil {
				return 0, err
			}
			arg++
			packInteger(buf[:8], math.Float64bits(f), p.bigEndian)
			sb.Write(buf[:8])
		case 'c':
			// Fixed-size string.
			s, err := CheckString(l, arg)
			if err != nil {
				return 0, err
			}
			if len(s) > size {
				return 0, NewArgError(l, arg, "string longer than given size")
			}
			sctx.AddSeq(l.StringContext(arg).All())
			arg++
			sb.WriteString(s)
			// Zero-pad to size.
			for range size - len(s) {
				sb.WriteByte(0)
			}
		case 's':
			// Length-prefixed string.
			s, err := CheckString(l, arg)
			if err != nil {
				return 0, err
			}
			if size < intSize && len(s) >= 1<<(size*8) {
				return 0, NewArgError(l, arg, "string length does not fit in given size")
			}
			sctx.AddSeq(l.StringContext(arg).All())
			arg++
			packInteger(buf[:size], uint64(len(s)), p.bigEndian)
			sb.Write(buf[:size])
			sb.WriteString(s)
		case 'z':
			// Zero-terminated string.
			s, err := CheckString(l, arg)
			if err != nil {
				return 0, err
			}
			if strings.IndexByte(s, 0) != -1 {
				return 0, NewArgError(l, arg, "string contains zeros")
			}
			sctx.AddSeq(l.StringContext(arg).All())
			arg++
			sb.WriteString(s)
			sb.WriteByte(0)
		case 'x':
			sb.WriteByte(0)
		}
	}

	l.PushStringContext(sb.String(), sctx)
	return 1, nil
}

func packInteger(b []byte, x uint64, bigEndian bool) {
	if bigEndian {
		for i := range b {
			b[len(b)-1-i] = byte(x)
			x >>= 8
		}
	} else {
		for i := range b {
			b[i] = byte(x)
			x >>= 8
		}
	}
}

func stringUnpack(ctx context.Context, l *State) (int, error) {
	format, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	data, err := CheckString(l, 2)
	if err != nil {
		return 0, err
	}
	sctx := l.StringContext(2)
	posArg := int64(1)
	if !l.IsNoneOrNil(3) {
		posArg, err = CheckInteger(l, 3)
		if err != nil {
			return 0, err
		}
	}
	pos, posInBounds := stringIndexArg(posArg, len(data))
	if !posInBounds {
		return 0, NewArgError(l, 3, "initial position out of string")
	}

	p := newPackParser(format)
	resultCount := 0
	for {
		opt, size, pad, _, err := p.next(pos)
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, NewArgError(l, 1, err.Error())
		}
		if pad+size > len(data)-pos {
			return 0, NewArgError(l, 2, "data string too short")
		}
		if !l.CheckStack(2) {
			return 0, fmt.Errorf("%sstack overflow (too many results)", Where(l, 1))
		}

		pos += pad
		resultCount++
		// TODO(now)
		switch opt {
		case 'c':
			l.PushStringContext(data[pos:pos+size], sctx)
			pos += size
		case 'z':
			n := strings.IndexByte(data[pos:], 0)
			if n == -1 {
				return 0, NewArgError(l, 2, "unfinished string for format 'z'")
			}
			l.PushStringContext(data[pos:pos+n], sctx)
			pos += n + 1
		default:
			// Padding.
			pos += size
		}
	}

	l.PushInteger(int64(pos) + 1)
	return resultCount + 1, nil
}

const isBigEndianNative = runtime.GOARCH == "armbe" ||
	runtime.GOARCH == "arm64be" ||
	runtime.GOARCH == "m68k" ||
	runtime.GOARCH == "mips" ||
	runtime.GOARCH == "mips64" ||
	runtime.GOARCH == "mips64p32" ||
	runtime.GOARCH == "ppc" ||
	runtime.GOARCH == "ppc64" ||
	runtime.GOARCH == "s390" ||
	runtime.GOARCH == "s390x" ||
	runtime.GOARCH == "shbe" ||
	runtime.GOARCH == "sparc" ||
	runtime.GOARCH == "sparc64"

type packParser struct {
	s         string
	bigEndian bool
	maxAlign  int
}

func newPackParser(s string) *packParser {
	return &packParser{
		s:         s,
		bigEndian: isBigEndianNative,
		maxAlign:  1,
	}
}

func (p *packParser) next(pos int) (c byte, size, pad int, hasFixedSize bool, err error) {
	c, n, err := p.nextOption()
	if err == io.EOF {
		return 0, 0, 0, true, err
	}
	if err != nil {
		return c, 0, 0, false, err
	}
	size, align, hasFixedSize := packOptionSize(c, n)
	pad, ok := numBytesToPad(pos, min(align, p.maxAlign))
	if !ok {
		return c, size, pad, hasFixedSize, errors.New("format asks for alignment not power of 2")
	}
	return c, size, pad, hasFixedSize, nil
}

func (p *packParser) nextOption() (c byte, n int, err error) {
	for len(p.s) > 0 {
		c = p.s[0]
		p.s = p.s[1:]
		switch c {
		case 'b', 'B', 'h', 'H', 'l', 'L', 'j', 'J', 'T', 'f', 'n', 'd', 'z', 'x':
			return c, 0, nil
		case 'i', 'I', 's', 'c':
			n, err = p.num()
			if err == io.EOF {
				switch c {
				case 'i', 'I', 's':
					n = intSize
				case 'c':
					return c, 0, errors.New("missing size for format option 'c'")
				default:
					panic("unreachable")
				}
			}
			if err != nil {
				return c, 0, err
			}
			if c != 'c' && !(1 <= n && n <= maxPackIntegerSize) {
				return c, 0, fmt.Errorf("integral size (%d) out of limits [1, %d]", n, maxPackIntegerSize)
			}
			return c, n, nil
		case '!':
			n, err = p.num()
			if err == io.EOF {
				// Assume max alignment is 8 bytes.
				n = 8
			}
			if err != nil {
				return '!', 0, err
			}
			p.maxAlign = n
		case 'X':
			// An empty item that aligns according to the next option.
			if len(p.s) == 0 || p.s[0] == 'c' {
				return 'X', 0, errors.New("invalid next option for option 'X'")
			}
			c = p.s[0]
			p.s = p.s[1:]
			if c == 'i' || c == 'I' || c == 's' {
				n, err = p.num()
				if err == io.EOF {
					n = intSize
				}
			}
			n, _, _ = packOptionSize(c, n)
			if n == 0 {
				return 'X', 0, errors.New("invalid next option for option 'X'")
			}
			return 'X', n, nil
		case '<':
			p.bigEndian = false
		case '>':
			p.bigEndian = true
		case '=':
			p.bigEndian = isBigEndianNative
		case ' ':
			// Ignore and advance to next option.
		default:
			return c, 0, fmt.Errorf("invalid format option %q", c)
		}
	}

	return 0, 0, io.EOF
}

func (p *packParser) num() (int, error) {
	end := 0
	for end < len(p.s) && '0' <= p.s[end] && p.s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, io.EOF
	}

	n, err := strconv.Atoi(p.s[:end])
	p.s = p.s[end:]
	if err != nil {
		return 0, err
	}
	return n, nil
}

const intSize = (32 << (^uint(0) >> 63)) / 8 // 4 or 8

func packOptionSize(c byte, n int) (size, alignment int, hasFixedSize bool) {
	switch c {
	case 'b', 'B', 'x':
		// byte
		return 1, 1, true
	case 'h', 'H':
		// short
		return 2, 2, true
	case 'l', 'L':
		// long
		return 4, 4, true
	case 'j', 'J':
		// lua_Integer/lua_Unsigned
		return 8, 8, true
	case 'T':
		// size_t
		return intSize, intSize, true
	case 'f':
		// float
		return 4, 4, true
	case 'd', 'n':
		// double/lua_Number
		return 8, 8, true
	case 'i', 'I':
		// Integer of N bytes.
		return n, n, true
	case 'c':
		// Fixed-size string of N bytes.
		return n, 1, true
	case 's':
		// Length-prefixed string.
		return n, n, false
	case '<', '>', '=', '!':
		// Endianness and alignment markers.
		return 0, 1, true
	case 'X':
		// Padding.
		return 0, max(n, 1), true
	default:
		return 0, 1, false
	}
}

func stringArithmetic(ctx context.Context, l *State, op luacode.ArithmeticOperator) (int, error) {
	toNumber := func(arg int) bool {
		if l.Type(arg) == TypeNumber {
			l.PushValue(arg)
			return true
		}

		s, ok := l.ToString(arg)
		if !ok {
			return false
		}
		if i, err := lualex.ParseInt(s); err == nil {
			l.PushInteger(i)
			return true
		}
		if f, err := lualex.ParseNumber(s); err == nil {
			l.PushNumber(f)
			return true
		}
		return false
	}

	if toNumber(1) && toNumber(2) {
		if err := l.Arithmetic(ctx, op); err != nil {
			return 0, err
		}
		return 1, nil
	}

	l.SetTop(2)
	mtName := op.TagMethod().String()
	if l.Type(2) == TypeString || Metafield(l, 2, mtName) == TypeNil {
		return 0, fmt.Errorf("%sattempt to %s a '%v' with a '%v'",
			Where(l, 1), mtName[2:], l.Type(-2), l.Type(-1))
	}
	l.Insert(-3)
	if err := l.Call(ctx, 2, 1, 0); err != nil {
		return 0, err
	}
	return 1, nil
}

func stringIndexArg(i int64, n int) (_ int, inBounds bool) {
	switch {
	case i < 0:
		return int(max(int64(n)+i, 0)), true
	case i == 0 || i == 1:
		return 0, true
	case i > int64(n):
		return n, false
	default:
		return int(i) - 1, true
	}
}

func stringEndArg(l *State, arg int, defaultValue int64, n int) (int, error) {
	i := defaultValue
	if !l.IsNoneOrNil(arg) {
		var err error
		i, err = CheckInteger(l, arg)
		if err != nil {
			return 0, err
		}
	}
	switch {
	case i < 0:
		return int(max(int64(n)+i+1, 0)), nil
	default:
		return int(min(i, int64(n))), nil
	}
}

func toLowerASCII(c rune) rune {
	if 'A' <= c && c <= 'Z' {
		return c - 'A' + 'a'
	}
	return c
}

func numBytesToPad(n, align int) (_ int, isPowerOfTwo bool) {
	mask := align - 1
	isPowerOfTwo = align&mask == 0
	if !isPowerOfTwo {
		return 0, false
	}
	return (align - (n & mask)) & mask, true
}

const packAlignErrorMessage = "format asks for alignment not power of 2"
