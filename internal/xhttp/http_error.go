// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package xhttp

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

type httpError struct {
	response  http.Response
	body      []byte
	bodyError error
}

// ErrorFromResponse turns an [*http.Response] into an error.
// The error message will include the HTTP status code
// and the body if ErrorFromResponse deems it is text.
func ErrorFromResponse(resp *http.Response) error {
	body, bodyError := io.ReadAll(http.MaxBytesReader(nil, resp.Body, 256<<10)) // 256 KiB
	// Clone after read so we potentially get Trailer.
	resp = cloneResponse(resp)
	resp.Body = nil
	err := &httpError{
		response:  *resp,
		body:      body,
		bodyError: bodyError,
	}
	if 100 <= resp.StatusCode && resp.StatusCode < 400 {
		return fmt.Errorf("unexpected %w", err)
	}
	return err
}

func (e *httpError) Error() string {
	sb := new(strings.Builder)
	sb.WriteString("http ")
	sb.WriteString(cmp.Or(e.response.Status, Status(e.response.StatusCode)))

	if body := bytes.TrimRight(e.body, "\n\r"); len(body) > 0 && !bytes.ContainsAny(body, "\x00") {
		if bytes.ContainsAny(body, "\n\r") {
			sb.WriteString(":\n")
		} else {
			sb.WriteString(": ")
		}
		// Remove carriage returns and replace with newlines where necessary.
		sb.Grow(len(body))
		for i, b := range body {
			switch {
			case b == '\r' && !(i+1 < len(body) && body[i+1] == '\n'):
				sb.WriteByte('\n')
			case b != '\r':
				sb.WriteByte(b)
			}
		}
		if _, truncated := errors.AsType[*http.MaxBytesError](e.bodyError); truncated {
			sb.WriteString("(...)")
		}
	}

	return sb.String()
}

// ErrorStatusCode returns the status code of an error created by [ErrorFromResponse]
// and reports whether the error is or wraps an error created by [ErrorFromResponse].
// If the error is nil, then ErrorStatusCode returns (200 (OK), false).
// If the error was not created by [ErrorFromResponse],
// then ErrorStatusCode returns (500 (Internal Server Error), false).
func ErrorStatusCode(err error) (statusCode int, ok bool) {
	if err == nil {
		return http.StatusOK, false
	}
	h, ok := errors.AsType[*httpError](err)
	if !ok {
		return http.StatusInternalServerError, false
	}
	return h.response.StatusCode, true
}

// ResponseFromError returns a copy of the response passed to [ErrorFromResponse].
// ResponseFromError returns (nil, false) if the error is not an error created by [ErrorFromResponse]
// or an error that wraps such an error.
// The Body field of the response does not need to be closed.
func ResponseFromError(err error) (_ *http.Response, ok bool) {
	h, ok := errors.AsType[*httpError](err)
	if !ok {
		return nil, false
	}
	resp := cloneResponse(&h.response)
	var body io.Reader = bytes.NewReader(h.body)
	if h.bodyError != nil {
		body = io.MultiReader(body, errorReader{h.bodyError})
	}
	resp.Body = io.NopCloser(body)
	return resp, true
}

func cloneResponse(r *http.Response) *http.Response {
	r = new(*r)
	r.Header = r.Header.Clone()
	r.Trailer = r.Trailer.Clone()
	r.TransferEncoding = slices.Clone(r.TransferEncoding)
	return r
}

type errorReader struct {
	err error
}

func (er errorReader) Read(p []byte) (int, error) {
	return 0, er.err
}

func (er errorReader) WriteTo(w io.Writer) (int64, error) {
	return 0, er.err
}
