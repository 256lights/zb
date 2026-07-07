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
