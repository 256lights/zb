// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package fileurl

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dsnet/compress/brotli"
)

// Transport is an [http.RoundTripper] that serves GET, HEAD, and PUT requests
// to the local filesystem for "file://" URLs.
type Transport struct {
}

// RoundTrip implements [http.RoundTripper].
func (t Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		defer req.Body.Close()
	}
	if req.URL.Scheme != Scheme {
		return nil, http.ErrSkipAltProtocol
	}

	switch req.Method {
	case "", http.MethodGet, http.MethodHead:
		return t.get(req), nil
	case http.MethodPut:
		return t.put(req), nil
	default:
		resp := errorResponse(req, "", http.StatusMethodNotAllowed)
		resp.Header["Allow"] = []string{"GET, HEAD, PUT"}
		return resp, nil
	}
}

func (t Transport) get(req *http.Request) *http.Response {
	path, err := ToPath(req.URL)
	if err != nil {
		return errorResponse(req, err.Error(), http.StatusNotFound)
	}
	return serveResponse(req, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		f, err := os.Open(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				http.NotFound(w, req)
			} else {
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
			return
		}
		defer f.Close()

		info, err := f.Stat()
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		http.ServeContent(w, req, filepath.Base(path), info.ModTime(), f)
	}))
}

func (t Transport) put(req *http.Request) (resp *http.Response) {
	path, err := ToPath(req.URL)
	if err != nil {
		return errorResponse(req, err.Error(), http.StatusNotFound)
	}

	flag := os.O_WRONLY | os.O_CREATE
	if ifMatch := req.Header.Values("If-Match"); len(ifMatch) == 0 {
		flag |= os.O_CREATE
	} else if len(ifMatch) != 1 || ifMatch[0] != "*" {
		return errorResponse(req, "If-Match not supported", http.StatusPreconditionFailed)
	} else if len(req.Header.Values("If-None-Match")) > 0 {
		return errorResponse(req, "If-Match and If-None-Match cannot be combined", http.StatusPreconditionFailed)
	}
	if req.Header.Get("If-None-Match") == "*" {
		flag |= os.O_EXCL
	}

	body, err := decodeBody(req.Body, req.Header.Get("Content-Encoding"))
	if err != nil {
		var code int
		if _, isUnknown := errors.AsType[contentEncodingError](err); isUnknown {
			code = http.StatusUnsupportedMediaType
		} else {
			code = http.StatusBadRequest
		}
		resp := errorResponse(req, err.Error(), code)
		resp.Header.Set("Accept-Encoding", acceptEncoding)
		return resp
	}
	defer body.Close()

	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return errorResponse(req, "Could not create parent directory", http.StatusInternalServerError)
	}
	f, err := os.OpenFile(path, flag, 0o666)
	if err != nil {
		if flag&os.O_EXCL != 0 && errors.Is(err, os.ErrExist) {
			return errorResponse(req, "Already exists", http.StatusPreconditionFailed)
		}
		if flag&os.O_CREATE == 0 && errors.Is(err, os.ErrNotExist) {
			return errorResponse(req, "Does not exist", http.StatusPreconditionFailed)
		}
		return errorResponse(req, "Could not open file", http.StatusInternalServerError)
	}
	defer func() {
		f.Close()
		if flag&(os.O_CREATE|os.O_EXCL) == (os.O_CREATE|os.O_EXCL) && resp.StatusCode != http.StatusCreated {
			// If the request failed and we know that we created this file
			// during this request, then remove it.
			os.Remove(path)
		}
	}()

	if ifUnmodifiedSince := req.Header.Get("If-Unmodified-Since"); ifUnmodifiedSince != "" {
		if t, err := http.ParseTime(ifUnmodifiedSince); err == nil {
			info, err := f.Stat()
			if err != nil {
				return errorResponse(req, "Stat failed", http.StatusInternalServerError)
			}
			if info.ModTime().After(t) {
				return errorResponse(req, "Modified since "+ifUnmodifiedSince, http.StatusPreconditionFailed)
			}
		}
	}
	if err := f.Truncate(0); err != nil {
		return errorResponse(req, "Copy failed", http.StatusInternalServerError)
	}
	if _, err := io.Copy(f, body); err != nil {
		return errorResponse(req, "Copy failed", http.StatusInternalServerError)
	}

	var modTime time.Time
	if info, err := f.Stat(); err != nil {
		modTime = info.ModTime()
	}
	if err := f.Close(); err != nil {
		return errorResponse(req, "Copy failed", http.StatusInternalServerError)
	}

	code := http.StatusNoContent
	if flag&(os.O_CREATE|os.O_EXCL) == (os.O_CREATE | os.O_EXCL) {
		code = http.StatusCreated
	}
	responseHeader := http.Header{
		"Date": {time.Now().UTC().Format(http.TimeFormat)},
	}
	if !modTime.IsZero() {
		responseHeader.Set("Last-Modified", modTime.UTC().Format(http.TimeFormat))
	}
	return &http.Response{
		Request:    req,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		StatusCode: code,
		Status:     http.StatusText(code),
		Header:     responseHeader,
		Body:       io.NopCloser(bytes.NewReader(nil)),
	}
}

// acceptEncoding is the value of an [Accept-Encoding header]
// that advertises the algorithms that [decodeBody] supports.
//
// [Accept-Encoding header]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Accept-Encoding
const acceptEncoding = "br,gzip,deflate"

func decodeBody(r io.Reader, contentEncoding string) (io.ReadCloser, error) {
	switch contentEncoding {
	case "":
		return io.NopCloser(r), nil
	case "br":
		return brotli.NewReader(r, nil)
	case "gzip", "x-gzip":
		return gzip.NewReader(r)
	case "deflate":
		return flate.NewReader(r), nil
	default:
		return nil, contentEncodingError{contentEncoding}
	}
}

type contentEncodingError struct {
	contentEncoding string
}

func (cee contentEncodingError) Error() string {
	return fmt.Sprintf("unsupported Content-Encoding %s", cee.contentEncoding)
}

// serveResponse calls h.ServeHTTP and converts the result into an [*http.Response].
func serveResponse(req *http.Request, h http.Handler) *http.Response {
	headerWritten := make(chan int)
	var rbody io.ReadCloser
	var wbody io.WriteCloser
	serveDone := make(chan struct{})
	if req.Method == http.MethodHead {
		rbody = &customReadCloser{
			Reader: bytes.NewReader(nil),
			close: func() error {
				<-serveDone
				return nil
			},
		}
		wbody = discardCloser{}
	} else {
		var pr *io.PipeReader
		pr, wbody = io.Pipe()
		rbody = &customReadCloser{
			Reader: pr,
			close: func() error {
				err := pr.Close()
				<-serveDone
				return err
			},
		}
	}
	header := make(http.Header)
	go func() {
		defer func() {
			wbody.Close()
			close(serveDone)
		}()
		w := &pipedResponseWriter{
			headerWritten: headerWritten,
			header:        header,
			body:          wbody,
		}
		h.ServeHTTP(w, req)
	}()

	statusCode := <-headerWritten
	resp := &http.Response{
		Request:    req,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Status:     http.StatusText(statusCode),
		StatusCode: statusCode,
		Header:     header,
		Body:       rbody,
	}
	if values := header.Values("Content-Length"); len(values) == 1 {
		if n, err := strconv.ParseUint(values[0], 10, 63); err == nil {
			resp.ContentLength = int64(n)
		} else {
			resp.ContentLength = -1
		}
	} else {
		resp.ContentLength = -1
	}
	return resp
}

type pipedResponseWriter struct {
	body io.Writer

	headerWritten chan<- int
	header        http.Header
}

func (w *pipedResponseWriter) Header() http.Header {
	return w.header
}

func (w *pipedResponseWriter) WriteHeader(statusCode int) {
	if w.headerWritten != nil {
		// Prevent future changes to w.Header() from racing with reads.
		w.header = w.header.Clone()

		w.headerWritten <- statusCode
		close(w.headerWritten)
		w.headerWritten = nil
	}
}

func (w *pipedResponseWriter) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.body.Write(p)
}

type discardCloser struct{}

func (discardCloser) Write(p []byte) (int, error) {
	return len(p), nil
}

func (discardCloser) WriteString(s string) (int, error) {
	return len(s), nil
}

func (discardCloser) ReadFrom(r io.Reader) (n int64, err error) {
	return io.Copy(io.Discard, r)
}

func (discardCloser) Close() error {
	return nil
}

type customReadCloser struct {
	io.Reader
	close func() error
}

func (crc *customReadCloser) Close() error {
	return crc.close()
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

// headerFieldCombiner is the string recommended by [Section 5.3 of RFC 9110]
// to be used to join multiple values of the same HTTP header field.
//
// [Section 5.3 of RFC 9110]: https://www.rfc-editor.org/rfc/rfc9110.html#section-5.3
const headerFieldCombiner = ", "
