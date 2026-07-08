// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

/*
Package xhttp provides functions for handling more complicated aspects of the HTTP protocol.
*/
package xhttp

import (
	"iter"
	"net/http"
	"strings"
	"time"
)

// HeaderFieldCombiner is the string recommended by [RFC 9110 Section 5.3]
// to be used to join multiple values of the same HTTP header field.
//
// [RFC 9110 Section 5.3]: https://www.rfc-editor.org/rfc/rfc9110.html#section-5.3
const HeaderFieldCombiner = ", "

// IsSafeMethod reports whether the request method's semantics are read-only
// according to [RFC 9110 Section 9.2.1].
//
// [RFC 9110 Section 9.2.1]: https://www.rfc-editor.org/rfc/rfc9110.html#section-9.2.1
func IsSafeMethod(req *http.Request) bool {
	return req.Method == "" ||
		req.Method == http.MethodGet ||
		req.Method == http.MethodHead ||
		req.Method == http.MethodOptions ||
		req.Method == http.MethodTrace
}

// IsFinalStatusCode reports whether the given HTTP status code is [final].
//
// [final]: https://www.rfc-editor.org/info/rfc9110/#section-15
func IsFinalStatusCode(code int) bool {
	return 200 <= code && code < 600
}

// SplitList splits an HTTP header [list value],
// handling [quoted strings].
//
// [list value]: https://www.rfc-editor.org/rfc/rfc9110.html#section-5.6.1
// [quoted strings]: https://www.rfc-editor.org/rfc/rfc9110.html#section-5.6.4
func SplitList(value string) iter.Seq[string] {
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

// HeaderValuesEqual reports whether the [combined field values] of lines1 and lines2 are equal.
//
// [combined field values]: https://www.rfc-editor.org/rfc/rfc9110.html#section-5.2
func HeaderValuesEqual(lines1, lines2 []string) bool {
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
	n1 := (len(lines1) - 1) * len(HeaderFieldCombiner)
	for _, line := range lines1 {
		n1 += len(line)
	}
	n2 := (len(lines2) - 1) * len(HeaderFieldCombiner)
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
			for _, b := range []byte(HeaderFieldCombiner) {
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

// headerValue is equivalent to [http.Header.Get],
// but skips the overhead of calling [http.CanonicalHeaderKey].
// Its key parameter should always be a constant string.
func headerValue(h http.Header, key string) string {
	v := h[key]
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

// dateHeader is equivalent to [http.ParseTime] on a header.
// but skips the overhead of calling [http.CanonicalHeaderKey].
// Its key parameter should always be a constant string.
func dateHeader(h http.Header, key string) (t time.Time, ok bool) {
	v := h[key]
	if len(v) == 0 {
		return time.Time{}, false
	}
	t, err := http.ParseTime(v[0])
	return t, err == nil
}
