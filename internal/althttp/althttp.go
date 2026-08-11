// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

/*
Package althttp provides access to non-HTTP protocols such as "file://" URLs
and common cloud service storage providers
in the form of [http.RoundTripper] implementations that present an HTTP GET/PUT API.
*/
package althttp

import (
	"cmp"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"zb.256lights.llc/pkg/internal/fileurl"
	"zb.256lights.llc/pkg/internal/httpencoding"
	"zb.256lights.llc/pkg/internal/xhttp"
)

// FileTransport is an [http.RoundTripper] that serves GET, HEAD, and PUT requests
// to the local filesystem for "file://" URLs.
type FileTransport = fileurl.Transport

// FileScheme is the URL scheme for [FileTransport].
const FileScheme = fileurl.Scheme

// decodeResponse performs Content-Encoding negotiation
// and rewrites the response body if necessary.
func decodeResponse(req *http.Request, resp *http.Response) {
	contentEncoding := resp.Header.Get("Content-Encoding")
	if contentEncoding == "" {
		return
	}
	acceptEncoding := req.Header.Values("Accept-Encoding")
	if len(acceptEncoding) > 0 && xhttp.EncodingQuality(acceptEncoding, contentEncoding) != 0 {
		return
	}
	decoded, err := httpencoding.Decode(resp.Body, contentEncoding)
	if httpencoding.IsUnsupported(err) {
		return
	}
	if err != nil {
		resp.Body.Close()
		*resp = *errorResponse(req, err.Error(), http.StatusInternalServerError)
		return
	}

	resp.Uncompressed = true
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
	resp.Body = &readMultiCloser{
		Reader:  decoded,
		closers: [len(readMultiCloser{}.closers)]io.Closer{decoded, resp.Body},
	}
}

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
		Status:        xhttp.Status(code),
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

type readMultiCloser struct {
	io.Reader
	closers [2]io.Closer
}

func (rmc *readMultiCloser) Close() error {
	var firstError error
	for _, c := range rmc.closers {
		if c == nil {
			continue
		}
		err := c.Close()
		firstError = cmp.Or(firstError, err)
	}
	return firstError
}
