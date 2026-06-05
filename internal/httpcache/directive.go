// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package httpcache

import (
	"errors"
	"iter"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func cacheControlDirectives(header http.Header) iter.Seq[cacheControlDirective] {
	return func(yield func(cacheControlDirective) bool) {
		for _, value := range header["Cache-Control"] {
			for elem := range splitList(value) {
				d, ok := parseCacheControlDirective(elem)
				if ok {
					if !yield(d) {
						return
					}
				}
			}
		}
	}
}

func hasNoCacheDirective(directives iter.Seq[cacheControlDirective]) bool {
	for d := range directives {
		if d.nameMatches("no-cache") && d.rawArgument == "" {
			return true
		}
	}
	return false
}

func hasNoStoreDirective(directives iter.Seq[cacheControlDirective]) bool {
	for d := range directives {
		if d.nameMatches("no-store") && d.rawArgument == "" {
			return true
		}
	}
	return false
}

type cacheControlDirective struct {
	name        string
	rawArgument string
}

// parseCacheControlDirective parses a [cacheControlDirective] from the given string.
// The string must not contain leading or trailing whitespace.
func parseCacheControlDirective(s string) (_ cacheControlDirective, ok bool) {
	nameEnd := tokenEnd(s)
	if nameEnd == 0 {
		return cacheControlDirective{}, false
	}
	name := s[:nameEnd]
	argument, ok := strings.CutPrefix(s[nameEnd:], "=")
	if !ok {
		if argument != "" {
			return cacheControlDirective{}, false
		}
		return cacheControlDirective{name: name}, true
	}

	if stringEnd, ok := quotedStringEnd(argument); ok && stringEnd == len(argument) {
		return cacheControlDirective{name: name, rawArgument: argument}, true
	} else if stringEnd != 0 {
		return cacheControlDirective{}, false
	}
	if end := tokenEnd(argument); end == 0 || end != len(argument) {
		return cacheControlDirective{}, false
	}
	return cacheControlDirective{name: name, rawArgument: argument}, true
}

// nameMatches reports whether the directive's name matches the given string,
// insensitive to case.
func (d cacheControlDirective) nameMatches(want string) bool {
	return equalCaseInsensitive(d.name, want)
}

// argument parses d.rawArgument and returns its value.
func (d cacheControlDirective) argument() (_ string, ok bool) {
	if len(d.rawArgument) == 0 {
		return "", false
	}
	if end := tokenEnd(d.rawArgument); end > 0 {
		if end < len(d.rawArgument) {
			return "", false
		}
		return d.rawArgument, true
	}
	if s, ok := unquote(d.rawArgument); ok {
		return s, true
	}
	switch end, validQuote := quotedStringEnd(d.rawArgument); {
	case end == 0:
		return d.rawArgument, true
	case !validQuote || end != len(d.rawArgument):
		return "", false
	}
	inner := d.rawArgument[1 : len(d.rawArgument)-1]
	i := strings.IndexByte(inner, '\\')
	if i < 0 {
		return inner, true
	}
	sb := new(strings.Builder)
	sb.Grow(len(inner))
	sb.WriteString(inner[:i])
	sb.WriteByte(inner[i+1])
	i += 2
	for {
		if i >= len(inner) {
			return sb.String(), true
		}
		if inner[i] == '\\' {
			i++
		}
		sb.WriteByte(inner[i])
		i++
	}
}

// splitList splits an HTTP header [list value],
// handling [quoted strings].
//
// [list value]: https://www.rfc-editor.org/rfc/rfc9110.html#section-5.6.1
// [quoted strings]: https://www.rfc-editor.org/rfc/rfc9110.html#section-5.6.4
func splitList(value string) iter.Seq[string] {
	const ows = " \t"
	return func(yield func(string) bool) {
		i := 0
		for j := 0; j < len(value); j++ {
			switch value[j] {
			case ',':
				if !yield(strings.Trim(value[i:j], ows)) {
					return
				}
				i = j + 1
			case '"':
				j++
				for j < len(value) && value[j] != '"' {
					if value[j] == '\\' {
						j += 2
					} else {
						j++
					}
				}
			}
		}
		yield(strings.Trim(value[i:], ows))
	}
}

// parseDeltaSeconds parses an HTTP [delta-seconds rule].
//
// [delta-seconds rule]: https://www.rfc-editor.org/rfc/rfc9111.html#section-1.2.2
func parseDeltaSeconds(s string) (time.Duration, error) {
	const maxBits = 31
	n, err := strconv.ParseUint(s, 10, maxBits)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) {
			// "If a cache receives a delta-seconds value greater than the greatest integer it can represent,
			// or if any of its subsequent calculations overflows,
			// the cache MUST consider the value to be 2147483648 (2^31) [...]"
			return (1<<maxBits - 1) * time.Second, nil
		}
		return 0, err
	}
	return time.Duration(n) * time.Second, nil
}

func formatDeltaSeconds(d time.Duration) string {
	return strconv.FormatInt(int64(d/time.Second), 10)
}

// quotedStringEnd returns the length of the [quoted string] at the beginning of s.
//
// [quoted string]: https://www.rfc-editor.org/rfc/rfc9110.html#section-5.6.4
func quotedStringEnd(s string) (int, bool) {
	if len(s) < 1 || s[0] != '"' {
		return 0, false
	}
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '"':
			return i + 1, true
		case '\\':
			i++
		}
	}
	return len(s), false
}

// unquote returns the value of a [quoted string].
//
// [quoted string]: https://www.rfc-editor.org/rfc/rfc9110.html#section-5.6.4
func unquote(s string) (string, bool) {
	end, ok := quotedStringEnd(s)
	if !ok || end != len(s) {
		return "", false
	}
	inner := s[1 : len(s)-1]
	i := strings.IndexByte(inner, '\\')
	if i < 0 {
		return inner, true
	}
	sb := new(strings.Builder)
	sb.Grow(len(inner))
	sb.WriteString(inner[:i])
	sb.WriteByte(inner[i+1])
	i += 2
	for {
		if i >= len(inner) {
			return sb.String(), true
		}
		if inner[i] == '\\' {
			i++
		}
		sb.WriteByte(inner[i])
		i++
	}
}

func tokenEnd(s string) int {
	for i, b := range []byte(s) {
		if !isTokenChar(rune(b)) {
			return i
		}
	}
	return len(s)
}

const tokenChars = "" +
	"\x00\x00\x00\x00" +
	"\xfa" + // !#$%&'
	"\x6c" + // *+-.
	"\xff\x03" + // 0-9
	"\xfe\xff\xff\xc7" + // A-Z^_
	"\xff\xff\xff\x57" // `a-z|~

func isTokenChar(c rune) bool {
	i := uint(c) >> 3
	mask := byte(1 << (c & 0b111))
	return i < uint(len(tokenChars)) && tokenChars[i]&mask != 0
}

func equalCaseInsensitive(s1, s2 string) bool {
	if len(s1) != len(s2) {
		return false
	}
	for i := range len(s1) {
		if lower(s1[i]) != lower(s2[i]) {
			return false
		}
	}
	return true
}

func lower(b byte) byte {
	if 'A' <= b && b <= 'Z' {
		return b - 'A' + 'a'
	}
	return b
}
