// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package httpcache

import (
	"io"
	"net/http"
	"time"
)

type storedResponse struct {
	id                 int64
	statusCode         int
	header             http.Header
	requestedAt        time.Time
	responseReceivedAt time.Time
	responseBodySize   int64
}

func (resp *storedResponse) toResponse(body io.ReadCloser) *http.Response {
	if resp == nil || !resp.responseReceived() {
		return nil
	}

	header := resp.header.Clone()
	ensureDateHeader(header, resp.responseReceivedAt)

	return &http.Response{
		StatusCode:    resp.statusCode,
		Status:        http.StatusText(resp.statusCode),
		Header:        header,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Body:          body,
		ContentLength: resp.responseBodySize,
	}
}

func (resp *storedResponse) responseReceived() bool {
	if resp == nil {
		return false
	}
	return !resp.responseReceivedAt.IsZero()
}

func (resp *storedResponse) date() time.Time {
	if resp == nil {
		return time.Time{}
	}
	if t, ok := dateHeader(resp.header, "Date"); ok {
		return t
	}
	return resp.responseReceivedAt
}

func (resp *storedResponse) ageAt(t time.Time) time.Duration {
	if !resp.responseReceived() {
		return 0
	}
	date := resp.date()
	age, _ := parseDeltaSeconds(headerValue(resp.header, "Age"))
	correctedInitialAge := max(0, resp.responseReceivedAt.Sub(date), age+resp.responseReceivedAt.Sub(resp.requestedAt))
	residentTime := t.Sub(resp.responseReceivedAt)
	return correctedInitialAge + residentTime
}

func (resp *storedResponse) freshnessLifetime() time.Duration {
	if resp == nil {
		return 0
	}

	found := false
	canCache := false
	var result time.Duration
	for directive := range cacheControlDirectives(resp.header) {
		switch {
		case directive.nameMatches("max-age"):
			arg, _ := directive.argument()
			if d, err := parseDeltaSeconds(arg); err == nil {
				result = d
				found = true
				canCache = true
			}
		case directive.nameMatches("private") || directive.nameMatches("public"):
			canCache = true
		}
	}
	if found {
		return result
	}

	if expires, ok := dateHeader(resp.header, "Expires"); ok {
		if date := resp.date(); !date.IsZero() {
			return expires.Sub(date)
		}
	}

	if canCache || resp.responseReceived() && isCacheableStatusCode(resp.statusCode) {
		// Heuristic freshness!
		// https://www.rfc-editor.org/rfc/rfc9111.html#section-4.2.2
		if lastModified, ok := dateHeader(resp.header, "Last-Modified"); ok {
			if date := resp.date(); !date.IsZero() {
				return date.Sub(lastModified) / 10
			}
		}
		return 30 * time.Second
	}

	return 0
}

func (resp *storedResponse) entityTag() (_ entityTag, ok bool) {
	return entityTagFromHeader(resp.header)
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

// ensureDateHeader adds the Date header to the given time
// if one is not set.
func ensureDateHeader(h http.Header, date time.Time) {
	dateValues := h["Date"]
	if len(dateValues) > 0 && dateValues[0] != "" {
		return
	}
	// Reuse slice storage if possible.
	clear(dateValues)
	dateValues = dateValues[:0]
	h["Date"] = append(dateValues, date.UTC().Format(http.TimeFormat))
}

// isFinalStatusCode reports whether the given HTTP status code is [final].
//
// [final]: https://www.rfc-editor.org/info/rfc9110/#section-15
func isFinalStatusCode(code int) bool {
	return 200 <= code && code < 600
}

// isCacheableStatusCode reports whether the given HTTP status code
// is [heuristically cacheable].
//
// [heuristically cacheable]: https://www.rfc-editor.org/info/rfc9110/#section-15.1
func isCacheableStatusCode(code int) bool {
	return code == http.StatusOK ||
		code == http.StatusNonAuthoritativeInfo ||
		code == http.StatusNoContent ||
		code == http.StatusPartialContent ||
		code == http.StatusMultipleChoices ||
		code == http.StatusMovedPermanently ||
		code == http.StatusPermanentRedirect ||
		code == http.StatusNotFound ||
		code == http.StatusMethodNotAllowed ||
		code == http.StatusGone ||
		code == http.StatusRequestURITooLong ||
		code == http.StatusNotImplemented
}
