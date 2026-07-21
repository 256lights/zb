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

	"zb.256lights.llc/pkg/internal/xhttp"
)

// cacheControlRequestDirectives is the parsed form of the Cache-Control request header field.
// See [Section 5.2.1 of RFC 9111] for details.
//
// [Section 5.2.1 of RFC 9111]: https://www.rfc-editor.org/rfc/rfc9111.html#section-5.2.1
type cacheControlRequestDirectives struct {
	maxAge       time.Duration
	maxStale     time.Duration
	anyStale     bool
	minFresh     time.Duration
	noCache      bool
	noStore      bool
	onlyIfCached bool
}

func newCacheControlRequestDirectives(seq iter.Seq[cacheControlDirective]) *cacheControlRequestDirectives {
	result := &cacheControlRequestDirectives{
		maxAge:   -1 * time.Second,
		maxStale: -1 * time.Second,
		minFresh: -1 * time.Second,
	}
	for d := range seq {
		switch {
		case d.nameMatches("max-age"):
			arg, _ := d.argument()
			if maxAge, err := parseDeltaSeconds(arg); err == nil {
				result.maxAge = maxAge
			}
		case d.nameMatches("max-stale") && d.rawArgument == "":
			result.anyStale = true
		case d.nameMatches("max-stale") && d.rawArgument != "":
			arg, _ := d.argument()
			if maxStale, err := parseDeltaSeconds(arg); err == nil {
				result.maxStale = maxStale
			}
		case d.nameMatches("min-fresh"):
			arg, _ := d.argument()
			if minFresh, err := parseDeltaSeconds(arg); err == nil {
				result.minFresh = minFresh
			}
		case d.nameMatches("no-store") && d.rawArgument == "":
			result.noStore = true
		case d.nameMatches("no-cache") && d.rawArgument == "":
			result.noCache = true
		case d.nameMatches("only-if-cached") && d.rawArgument == "":
			result.onlyIfCached = true
		}
	}
	return result
}

func (rd *cacheControlRequestDirectives) hasMaxAge() bool {
	return rd != nil && rd.maxAge >= 0
}

func (rd *cacheControlRequestDirectives) hasMaxStale() bool {
	return rd != nil && rd.maxStale >= 0
}

func (rd *cacheControlRequestDirectives) hasMinFresh() bool {
	return rd != nil && rd.minFresh >= 0
}

func cacheControlDirectives(header http.Header) iter.Seq[cacheControlDirective] {
	return func(yield func(cacheControlDirective) bool) {
		for _, value := range header["Cache-Control"] {
			for elem := range xhttp.SplitList(value) {
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
		if !xhttp.IsTokenChar(rune(b)) {
			return i
		}
	}
	return len(s)
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
