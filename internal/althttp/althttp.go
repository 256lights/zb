// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

/*
Package althttp provides access to non-HTTP protocols such as "file://" URLs
and common cloud service storage providers
in the form of [http.RoundTripper] implementations that present an HTTP GET/PUT API.
*/
package althttp

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"zb.256lights.llc/pkg/internal/fileurl"
)

// FileTransport is an [http.RoundTripper] that serves GET, HEAD, and PUT requests
// to the local filesystem for "file://" URLs.
type FileTransport = fileurl.Transport

// FileScheme is the URL scheme for [FileTransport].
const FileScheme = fileurl.Scheme

func errorResponse(req *http.Request, error string, code int) *http.Response {
	if error != "" {
		error += "\n"
	}
	return &http.Response{
		Request:       req,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		StatusCode:    code,
		Status:        http.StatusText(code),
		ContentLength: int64(len(error)),
		Header: http.Header{
			"Content-Type":           {"text/plain; charset=utf-8"},
			"X-Content-Type-Options": {"nosniff"},
			"Content-Length":         {strconv.Itoa(len(error))},
			"Date":                   {time.Now().UTC().Format(http.TimeFormat)},
		},
		Body: io.NopCloser(strings.NewReader(error)),
	}
}

func hasPreconditions(h http.Header) bool {
	return len(h.Values("If-Match")) > 0 ||
		len(h.Values("If-None-Match")) > 0 ||
		len(h.Values("If-Modified-Since")) > 0 ||
		len(h.Values("If-Unmodified-Since")) > 0
}
