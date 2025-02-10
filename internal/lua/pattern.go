// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"errors"
	"iter"
	"strings"

	"zb.256lights.llc/pkg/sets"
)

// Special types of [patternState].
// These strings are impossible to parse as pattern items,
// but are otherwise insignificant.
const (
	patternStateSplit        = "||"
	patternStateSuffixAnchor = "$$"
)

// pattern is a compiled [Lua pattern].
//
// [Lua pattern]: https://www.lua.org/manual/5.4/manual.html#6.4.1
type pattern struct {
	start            patternState
	numCaptures      int
	positionCaptures sets.Bit
}

// patternState is a non-deterministic finite automaton (NFA) state
// in a [pattern].
// See https://swtch.com/~rsc/regexp/regexp1.html for a discussion of the theory.
type patternState struct {
	// item is a pattern item, a parenthesis,
	// or one of the constants [patternStateSplit] or [patternStateSuffixAnchor].
	item string
	// out is the set of the outgoing edges of this state
	// in descending priority order.
	// The second edge is only used for [patternStateSplit].
	out [2]*patternState
}

func parsePattern(p string) (*pattern, error) {
	result := new(pattern)
	p, anchored := strings.CutPrefix(p, "^")
	var out [2]**patternState
	if anchored {
		result.start.item = "("
		out[0] = &result.start.out[0]
	} else {
		// Start with the equivalent of ".*(".
		result.start.item = patternStateSplit
		startState := &patternState{item: "("}
		result.start.out[0] = startState
		result.start.out[1] = &patternState{
			item: ".",
			out:  [2]*patternState{&result.start},
		}
		out[0] = &startState.out[0]
	}

	captureDepth := 0
	patch := func(s *patternState) {
		for _, ptr := range out {
			if ptr != nil {
				*ptr = s
			}
		}
		clear(out[:])
	}
	for len(p) > 0 {
		switch {
		case strings.HasPrefix(p, "()"):
			result.positionCaptures.Add(uint(result.numCaptures))
			fallthrough
		case p[0] == '(':
			result.numCaptures++
			if result.numCaptures > 32 {
				return nil, errors.New("too many captures")
			}
			captureDepth++
			newState := &patternState{item: "("}
			patch(newState)
			out[0] = &newState.out[0]
			p = p[1:]
		case p[0] == ')':
			if captureDepth <= 0 {
				return nil, errors.New("invalid pattern capture")
			}
			captureDepth--
			newState := &patternState{item: ")"}
			patch(newState)
			out[0] = &newState.out[0]
			p = p[1:]
		case p == "$":
			newState := &patternState{item: patternStateSuffixAnchor}
			patch(newState)
			out[0] = &newState.out[0]
			p = p[1:]
		case strings.HasPrefix(p, "%b"):
			return nil, errors.New("patterns with balances not supported")
		case len(p) >= 2 && p[0] == '%' && isASCIIDigit(rune(p[1])):
			return nil, errors.New("patterns with backreferences not supported")
		case strings.HasPrefix(p, "%f"):
			afterEscape := p[len("%f"):]
			if !strings.HasPrefix(afterEscape, "[") {
				return nil, errors.New("missing '[' after '%f' in pattern")
			}
			n, err := characterClassEnd(afterEscape)
			if err != nil {
				return nil, err
			}
			n += len("%f")
			newState := &patternState{item: p[:n]}
			patch(newState)
			out[0] = &newState.out[0]
			p = p[n:]
		default:
			// Character class followed by optional modifier.
			n, err := characterClassEnd(p)
			if err != nil {
				return nil, err
			}
			newState := &patternState{item: p[:n]}

			var modifier byte
			if n < len(p) {
				modifier = p[n]
			}
			switch modifier {
			case '?':
				splitState := &patternState{
					item: patternStateSplit,
					out:  [2]*patternState{newState},
				}
				patch(splitState)
				out[0] = &newState.out[0]
				out[1] = &splitState.out[1]
				p = p[n+1:]
			case '+':
				patch(newState)
				out[0] = &newState.out[0]
				newState = &patternState{item: newState.item}
				fallthrough
			case '*':
				// Zero or more, prefer longer.
				splitState := &patternState{
					item: patternStateSplit,
					out:  [2]*patternState{newState},
				}
				newState.out[0] = splitState
				patch(splitState)
				out[0] = &splitState.out[1]
				p = p[n+1:]
			case '-':
				// Zero or more, prefer shorter.
				splitState := &patternState{
					item: patternStateSplit,
					out:  [2]*patternState{nil, newState},
				}
				newState.out[0] = splitState
				patch(splitState)
				out[0] = &splitState.out[0]
				p = p[n+1:]
			default:
				patch(newState)
				out[0] = &newState.out[0]
				p = p[n:]
			}
		}
	}

	if captureDepth > 0 {
		return nil, errors.New("unfinished capture")
	}
	// Close out match.
	patch(&patternState{item: ")"})

	return result, nil
}

// isPrefixAnchored reports whether the pattern passed to [parsePattern]
// started with "^".
func (p *pattern) isPrefixAnchored() bool {
	return p.start.item == "("
}

// findAll iterates over all matches of the pattern in the string.
func (p *pattern) findAll(s string) iter.Seq[[]int] {
	if p.isPrefixAnchored() {
		// A prefix pattern can only match once.
		return func(yield func([]int) bool) {
			if captures := p.find(s, 0); len(captures) > 0 {
				yield(captures)
			}
		}
	}

	return func(yield func([]int) bool) {
		for pos := 0; pos <= len(s); {
			captures := p.find(s, pos)
			if len(captures) == 0 {
				return
			}
			end := captures[1]
			if !yield(captures) {
				return
			}
			// Always advance at least one character.
			pos = max(end, pos+1)
		}
	}
}

// find returns the first match of the pattern
// at or after the given index in the string,
// or nil if there is no match.
// The first two elements of the returned match
// are the start and end indices of the match,
// and subsequent pairs are the start and end indices of captures.
func (p *pattern) find(s string, pos int) []int {
	type matchState struct {
		state    *patternState
		captures []int
	}

	if pos > len(s) {
		return nil
	}

	capturesCap := (p.numCaptures + 1) * 2
	var currList, nextList []matchState
	var addState func(matchState)
	addState = func(curr matchState) {
		// Advance past zero-length states.
		for {
			switch {
			case curr.state != nil && curr.state.item == "(":
				curr.captures = append(curr.captures, pos, -1)
				curr.state = curr.state.out[0]
			case curr.state != nil && curr.state.item == ")":
				// Fill in the end index of the most recently opened capture.
				i := lastIndex(curr.captures, -1)
				if i == -1 {
					panic("unmatched parenthesis")
				}
				curr.captures[i] = pos
				curr.state = curr.state.out[0]
			case curr.state != nil && curr.state.item == patternStateSplit:
				capturesCopy := append(make([]int, 0, capturesCap), curr.captures...)
				// TODO(soon): Remove recursive call or check depth.
				addState(matchState{
					state:    curr.state.out[0],
					captures: curr.captures,
				})
				curr.captures = capturesCopy
				curr.state = curr.state.out[1]
			case curr.state != nil && curr.state.item == patternStateSuffixAnchor:
				if pos < len(s) {
					return
				}
				curr.state = curr.state.out[0]
			case curr.state != nil && strings.HasPrefix(curr.state.item, "%f["):
				set := curr.state.item[len("%f[") : len(curr.state.item)-1]
				var prev, next byte
				if pos > 0 {
					prev = s[pos-1]
				}
				if pos < len(s) {
					next = s[pos]
				}
				if matchBracketClass(prev, set) || !matchBracketClass(next, set) {
					return
				}
				curr.state = curr.state.out[0]
			default:
				nextList = append(nextList, curr)
				return
			}
		}
	}

	// Initial state.
	addState(matchState{
		state:    &p.start,
		captures: make([]int, 0, capturesCap),
	})
	currList, nextList = nextList, currList

	for ; pos < len(s) && len(currList) > 0; currList, nextList = nextList, currList {
		// Short-circuit: If the highest priority state is a match,
		// don't bother stepping through everything else.
		if currList[0].state == nil {
			return currList[0].captures
		}

		clear(nextList)
		nextList = nextList[:0]
		c := s[pos]
		pos++

		// Step every current state.
		for _, curr := range currList {
			if curr.state == nil {
				nextList = append(nextList, curr)
			} else if matchByte(c, curr.state.item) {
				addState(matchState{
					state:    curr.state.out[0],
					captures: curr.captures,
				})
			}
		}
	}

	for _, curr := range currList {
		if curr.state == nil {
			return curr.captures
		}
	}
	return nil
}

func matchByte(b byte, characterClass string) bool {
	switch p := characterClass[0]; p {
	case '.':
		return true
	case '%':
		return len(characterClass) >= 2 && matchEscapedClass(b, characterClass[1])
	case '[':
		set, ok := strings.CutSuffix(characterClass[1:], "]")
		return ok && matchBracketClass(b, set)
	default:
		return b == p
	}
}

// matchBracketClass reports whether b matches the Lua pattern character class
// written as the set string surrounded by brackets.
// For example, matchBracketClass(b, "^abc") checks whether b matches "[^abc]".
func matchBracketClass(b byte, set string) bool {
	set, invert := strings.CutPrefix(set, "^")
	for i := 0; i < len(set); i++ {
		switch p := set[i]; {
		case p == '%':
			// Escaped class.
			i++
			if i >= len(set) {
				return false
			}
			if matchEscapedClass(b, set[i]) {
				return !invert
			}
		case i+2 < len(set) && set[i+1] == '-':
			// Range.
			if p <= b && b <= set[i+2] {
				return !invert
			}
			i += 2
		case b == p:
			return !invert
		}
	}
	return invert
}

// matchEscapedClass reports whether b matches the Lua pattern character class
// written as a percent sign followed by the byte p.
func matchEscapedClass(b byte, p byte) bool {
	var matched bool
	switch toLowerASCII(rune(p)) {
	case 'a':
		matched = isASCIILetter(rune(b))
	case 'c':
		matched = isASCIIControl(rune(b))
	case 'd':
		matched = isASCIIDigit(rune(b))
	case 'g':
		matched = isASCIIGraphic(rune(b))
	case 'l':
		matched = isASCIILowercase(rune(b))
	case 'p':
		matched = isASCIIPunctuation(rune(b))
	case 's':
		matched = isASCIISpace(rune(b))
	case 'u':
		matched = isASCIIUppercase(rune(b))
	case 'w':
		matched = isASCIILetter(rune(b)) || isASCIIDigit(rune(b))
	case 'x':
		matched = isHexDigit(rune(b))
	default:
		return b == p
	}
	return matched == isASCIILowercase(rune(p))
}

// characterClassEnd returns the length of the Lua pattern character class
// at the start of pattern.
// characterClassEnd returns 0 if and only if pattern is empty.
//
// Character classes are a byte, an escape indicating a set of characters,
// or a bracketed character class.
func characterClassEnd(pattern string) (end int, err error) {
	switch {
	case len(pattern) == 0:
		return 0, nil
	case pattern[0] == '%':
		if len(pattern) < 2 {
			return -1, errors.New("malformed pattern (ends with '%')")
		}
		return 2, nil
	case pattern[0] == '[':
		end := 1
		if strings.HasPrefix(pattern[end:], "^") {
			end++
		}
		if strings.HasPrefix(pattern[end:], "]") {
			// Don't let ']' in first position terminate class.
			end++
		}
		for ; end < len(pattern); end++ {
			switch pattern[end] {
			case '%':
				// Skip escape.
				end++
			case ']':
				return end + 1, nil
			}
		}
		return -1, errors.New("malformed pattern (missing ']')")
	default:
		return 1, nil
	}
}

func isASCIILetter(c rune) bool {
	return isASCIIUppercase(c) || isASCIILowercase(c)
}

func isASCIIControl(c rune) bool {
	return 0 <= c && c <= 0x1f || c == 0x7f
}

func isASCIISpace(c rune) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

func isASCIIPunctuation(c rune) bool {
	return 0x21 <= c && c <= 0x2f || // !"#$%&'()*+,-./
		0x3a <= c && c <= 0x40 || // :;<=>?@
		0x5b <= c && c <= 0x60 || // [\]^_`
		0x7b <= c && c <= 0x7e // {|}~
}

// isASCIIGraphic reports whether c is an ASCII character with a graphic representation.
// Here, this means all ASCII characters in Unicode categories L, M, N, P, and S.
func isASCIIGraphic(c rune) bool {
	return 0x21 <= c && c <= 0x7e
}

func isASCIIDigit(c rune) bool {
	return '0' <= c && c <= '9'
}

func isASCIIUppercase(c rune) bool {
	return 'A' <= c && c <= 'Z'
}

func isASCIILowercase(c rune) bool {
	return 'a' <= c && c <= 'z'
}

func toLowerASCII(c rune) rune {
	if isASCIIUppercase(c) {
		return c - 'A' + 'a'
	}
	return c
}

func isHexDigit(c rune) bool {
	return isASCIIDigit(c) ||
		'a' <= c && c <= 'f' ||
		'A' <= c && c <= 'F'
}

func lastIndex[S ~[]E, E comparable](s S, v E) int {
	for i := len(s) - 1; i >= 0; i-- {
		if v == s[i] {
			return i
		}
	}
	return -1
}
