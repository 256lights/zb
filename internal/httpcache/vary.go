// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package httpcache

import (
	"iter"
	"net/http"
	"strings"
)

// varyValue is a list of HTTP Vary field lines.
type varyValue []string

// varyHeader returns a [varyValue] from the header.
func varyHeader(h http.Header) varyValue {
	return varyValue(h["Vary"])
}

// IsZero reports whether vv is empty.
func (vv varyValue) IsZero() bool {
	return len(vv) == 0 || len(vv) == 1 && strings.Trim(vv[0], " \t") == ""
}

// hasWildcard reports whether vv contains "*".
func (vv varyValue) hasWildcard() bool {
	for _, varyValue := range vv {
		for varyElem := range splitList(varyValue) {
			if varyElem == "*" {
				return true
			}
		}
	}
	return false
}

// fieldNames returns an iterator over the field names in vv.
// All field names are transformed using [http.CanonicalHeaderKey].
func (vv varyValue) fieldNames() iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, varyValue := range vv {
			for varyElem := range splitList(varyValue) {
				if varyElem != "*" {
					varyElem = http.CanonicalHeaderKey(varyElem)
					if !yield(varyElem) {
						return
					}
				}
			}
		}
	}
}

// headerFieldCombiner is the string recommended by [Section 5.3 of RFC 9110]
// to be used to join multiple values of the same HTTP header field.
//
// [Section 5.3 of RFC 9110]: https://www.rfc-editor.org/rfc/rfc9110.html#section-5.3
const headerFieldCombiner = ", "

// headerValuesEqual reports whether the [combined field values] of lines1 and lines2 are equal.
//
// [combined field values]: https://www.rfc-editor.org/rfc/rfc9110.html#section-5.2
func headerValuesEqual(lines1, lines2 []string) bool {
	if len(lines1) <= 1 && len(lines2) <= 1 {
		// Fast path: single string comparison.
		var s1, s2 string
		if len(lines1) != 0 {
			s1 = lines1[0]
		}
		if len(lines2) != 0 {
			s2 = lines2[0]
		}
		return s1 == s2
	}

	// Check sizes for equality first.
	n1 := (len(lines1) - 1) * len(headerFieldCombiner)
	for _, line := range lines1 {
		n1 += len(line)
	}
	n2 := (len(lines2) - 1) * len(headerFieldCombiner)
	for _, line := range lines2 {
		n2 += len(line)
	}
	if n1 != n2 {
		return false
	}

	next1, stop1 := iter.Pull(iterHeaderValueBytes(lines1))
	defer stop1()
	next2, stop2 := iter.Pull(iterHeaderValueBytes(lines2))
	defer stop2()
	for {
		c1, ok1 := next1()
		c2, ok2 := next2()
		switch {
		case !ok1 && !ok2:
			return true
		case c1 != c2 || ok1 != ok2:
			return false
		}
	}
}

func iterHeaderValueBytes(lines []string) iter.Seq[byte] {
	return func(yield func(byte) bool) {
		if len(lines) == 0 {
			return
		}
		for _, b := range []byte(lines[0]) {
			if !yield(b) {
				return
			}
		}
		for _, line := range lines[1:] {
			for _, b := range []byte(headerFieldCombiner) {
				if !yield(b) {
					return
				}
			}
			for _, b := range []byte(line) {
				if !yield(b) {
					return
				}
			}
		}
	}
}
