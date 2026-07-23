// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package zbstorehttp

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"zb.256lights.llc/pkg/internal/httpencoding"
	"zb.256lights.llc/pkg/internal/xhttp"
	"zb.256lights.llc/pkg/internal/xio"
	"zb.256lights.llc/pkg/sets"
	"zombiezen.com/go/log"
)

const mebibyte = 1 << 20

// maxMemorySize is the maximum size of a resource to copy into memory.
const maxMemorySize = 4 * mebibyte

// A Client is an HTTP client that handles redirects, caching, and authentication.
// Clients are safe for concurrent use by multiple goroutines.
//
// Do sends an HTTP request and returns an HTTP response,
// following policy as configured on the client.
//
// Do returns an error if caused by client policy or failure to speak HTTP.
// On error, any [*http.Response] can be ignored.
// A non-2xx status code doesn't cause an error.
// If the returned error is nil, the [*http.Response] will contain a non-nil Body
// which the user is expected to close.
// Any returned error should be of type [*url.Error].
//
// The [*http.Request] Body, if non-nil, will be closed by the Client, even on errors.
// The Body may be closed asynchronously after Do returns.
type Client interface {
	Do(*http.Request) (*http.Response, error)
}

type fetchRequest struct {
	url    *url.URL
	origin *url.URL
	accept string
}

type fetchResponse struct {
	body               []byte
	validators         xhttp.ValidatorFields
	requestNegotiation requestNegotiation
}

func fetch(ctx context.Context, client Client, req *fetchRequest) (*fetchResponse, error) {
	httpRequest := (&http.Request{
		Method: http.MethodGet,
		URL:    req.url,
		Header: http.Header{
			"Accept":          {req.accept},
			"Accept-Encoding": {httpencoding.Accept},
		},
	}).WithContext(ctx)
	if origin, ok := computeOrigin(http.MethodGet, req.url, req.origin); ok {
		httpRequest.Header.Set("Origin", origin)
	}
	resp, err := client.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("fetch %v: %v", req.url.Redacted(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %v: %w", req.url.Redacted(), httpErrorFromResponse(resp))
	}

	result := &fetchResponse{
		validators:         xhttp.ExtractValidatorFields(resp.Header),
		requestNegotiation: *requestNegotiationFromResponseHeader(resp.Header),
	}
	if resp.ContentLength >= 0 && resp.ContentLength > maxMemorySize {
		return result, fmt.Errorf("fetch %v: response too large (%.1f MiB)",
			req.url.Redacted(), float64(resp.ContentLength)/mebibyte)
	}
	result.body, err = io.ReadAll(io.LimitReader(resp.Body, maxMemorySize))
	if err != nil {
		return result, fmt.Errorf("fetch %v: %v", req.url.Redacted(), err)
	}
	if resp.ContentLength == -1 && len(result.body) == maxMemorySize {
		if n, _ := resp.Body.Read(make([]byte, 1)); n > 0 {
			return result, fmt.Errorf("fetch %v: response too large", req.url.Redacted())
		}
	}
	if e := resp.Header.Get("Content-Encoding"); e != "" {
		dec, err := httpencoding.Decode(bytes.NewReader(result.body), e)
		if err != nil {
			return result, fmt.Errorf("fetch %v: %v", req.url.Redacted(), err)
		}
		defer dec.Close()
		result.body, err = io.ReadAll(dec)
		if err != nil {
			return result, fmt.Errorf("fetch %v: %v", req.url.Redacted(), err)
		}
	}
	return result, nil
}

type putRequest struct {
	url    *url.URL
	origin *url.URL

	getContent    func() (io.ReadCloser, error)
	contentLength int64
	contentType   string

	noReplace    bool
	precondition xhttp.ValidatorFields
	cacheControl string
	// acceptEncoding is a slice of Accept-Encoding field values
	// from a previous response from the same URL.
	acceptEncoding []string
}

func (req *putRequest) canContentFitInMemory() bool {
	return 0 <= req.contentLength && req.contentLength <= maxMemorySize
}

func (req *putRequest) readContent(ctx context.Context) ([]byte, error) {
	if req.contentLength < 0 {
		return nil, fmt.Errorf("put %s: request body: unknown size", req.url.Redacted())
	}
	if !req.canContentFitInMemory() {
		return nil, fmt.Errorf("put %s: request body: too large (%.1fMiB)", req.url.Redacted(), float64(req.contentLength)/mebibyte)
	}
	rc, err := req.getContent()
	if err != nil {
		return nil, fmt.Errorf("put %s: request body: %v", req.url.Redacted(), err)
	}
	defer func() {
		if err := rc.Close(); err != nil {
			log.Debugf(ctx, "While closing put %s request body: %v", req.url.Redacted(), err)
		}
	}()
	if req.contentLength >= 0 {
		rc = http.MaxBytesReader(nil, rc, req.contentLength)
	}
	content, err := io.ReadAll(rc)
	if err != nil {
		err = fmt.Errorf("put %s: request body: %v", req.url.Redacted(), err)
	} else if req.contentLength >= 0 && int64(len(content)) != req.contentLength {
		err = fmt.Errorf("put %s: request body: size (%d bytes) does not match Content-Length (%d bytes)",
			req.url.Redacted(), len(content), req.contentLength)
	}
	return content, err
}

// encodeContent returns the Content-Length (possibly -1 for unknown)
// and a function suitable for the GetBody field of [*http.Request]
// that encodes req.getContent according to the given Content-Encoding.
func (req *putRequest) encodeContent(ctx context.Context, contentEncoding string) (contentLength int64, getBody func() (io.ReadCloser, error), err error) {
	if contentEncoding == "" {
		return req.contentLength, req.getContent, nil
	}
	if !req.canContentFitInMemory() {
		f := req.getContent
		return -1, func() (io.ReadCloser, error) {
			uncompressed, err := f()
			if err != nil {
				return nil, err
			}
			rc, err := httpencoding.Encode(uncompressed, contentEncoding)
			if err != nil {
				uncompressed.Close()
				return nil, err
			}
			return &readMultiCloser{
				Reader: rc,
				closers: [len(readMultiCloser{}.closers)]io.Closer{
					rc,
					uncompressed,
				},
			}, nil
		}, nil
	}

	uncompressed, err := req.getContent()
	if err != nil {
		return -1, nil, err
	}
	defer func() {
		if err := uncompressed.Close(); err != nil {
			log.Debugf(ctx, "While closing put %s request body: %v", req.url.Redacted(), err)
		}
	}()
	wc := new(xio.WriteCounter)
	compressed, err := httpencoding.Encode(io.TeeReader(uncompressed, wc), contentEncoding)
	if err != nil {
		return -1, nil, fmt.Errorf("put %s: request body: %v", req.url.Redacted(), err)
	}
	defer func() {
		if err := compressed.Close(); err != nil {
			log.Debugf(ctx, "While closing compressed put %s request body: %v", req.url.Redacted(), err)
		}
	}()
	compressedData, err := io.ReadAll(compressed)
	if err != nil {
		return -1, nil, fmt.Errorf("put %s: request body: %v", req.url.Redacted(), err)
	}
	if int64(*wc) != req.contentLength {
		err := fmt.Errorf("put %s: request body: size (%d bytes) does not match Content-Length (%d bytes)",
			req.url.Redacted(), *wc, req.contentLength)
		return -1, nil, err
	}
	return int64(len(compressedData)), func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(compressedData)), nil
	}, nil
}

func put(ctx context.Context, client Client, req *putRequest) error {
	if req.noReplace && !req.precondition.IsZero() {
		return fmt.Errorf("put %s: precondition combined with If-None-Match:*", req.url.Redacted())
	}

	if req.canContentFitInMemory() {
		content, err := req.readContent(ctx)
		if err != nil {
			return err
		}
		req = new(*req)
		req.getContent = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(content)), nil
		}
	}

	acceptEncoding := req.acceptEncoding
	compareCodings := func(coding1, coding2 string) int {
		q1 := xhttp.EncodingQuality(acceptEncoding, coding1)
		q2 := xhttp.EncodingQuality(acceptEncoding, coding2)
		return cmp.Compare(q1, q2)
	}

	codings := []string{"gzip", ""} // Descending preference.
	nextCoding := slices.MaxFunc(codings, compareCodings)
	if xhttp.EncodingQuality(acceptEncoding, nextCoding) == 0 {
		// If server isn't advertising any of the codings we support,
		// try sending identity encoding anyway.
		return putEncoding(ctx, client, "", req)
	}
	for {
		codings = slices.DeleteFunc(codings, func(c string) bool { return c == nextCoding })

		err := putEncoding(ctx, client, nextCoding, req)
		var needsDifferentCoding bool
		acceptEncoding, needsDifferentCoding = isUnsupportedContentCoding(err)
		if !needsDifferentCoding || len(codings) == 0 {
			return err
		}
		nextCoding = slices.MaxFunc(codings, compareCodings)
		if xhttp.EncodingQuality(acceptEncoding, nextCoding) == 0 {
			// Nothing is acceptable after a fresh PUT.
			// Return the error from the request.
			return err
		}
	}
}

func putEncoding(ctx context.Context, client Client, contentEncoding string, req *putRequest) error {
	contentLength, getContent, err := req.encodeContent(ctx, contentEncoding)
	if err != nil {
		return err
	}

	body, err := getContent()
	if err != nil {
		return fmt.Errorf("put %s: %v", req.url.Redacted(), err)
	}
	httpRequest := &http.Request{
		Method: http.MethodPut,
		URL:    req.url,
		Header: http.Header{
			"Content-Type": {req.contentType},
		},
		ContentLength: -1,
		Body:          body,
		GetBody:       req.getContent,
	}
	if contentEncoding != "" {
		httpRequest.Header.Set("Content-Encoding", contentEncoding)
	}
	if contentLength >= 0 {
		httpRequest.Header.Set("Content-Length", strconv.FormatInt(contentLength, 10))
		httpRequest.ContentLength = contentLength
	}
	if origin, ok := computeOrigin(http.MethodPut, req.url, req.origin); ok {
		httpRequest.Header.Set("Origin", origin)
	}
	if req.noReplace {
		httpRequest.Header.Set("If-None-Match", "*")
	} else if req.precondition.ETag != "" {
		httpRequest.Header.Set("If-Match", string(req.precondition.ETag))
	} else if !req.precondition.LastModified.IsZero() {
		// As per https://datatracker.ietf.org/doc/html/rfc9110#section-13.1.4,
		// "[a] recipient MUST ignore If-Unmodified-Since if the request contains an If-Match header field [...]".
		// Thus, we avoid sending the header field if we already have an entity tag.
		v := req.precondition.LastModified.UTC().Format(http.TimeFormat)
		httpRequest.Header.Set("If-Unmodified-Since", v)
	}
	if req.cacheControl != "" {
		httpRequest.Header.Set("Cache-Control", req.cacheControl)
	}
	log.Debugf(ctx, "PUT %s Content-Encoding:%s", req.url.Redacted(), contentEncoding)
	resp, err := client.Do(httpRequest.WithContext(ctx))
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil &&
		resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusCreated &&
		resp.StatusCode != http.StatusNoContent {
		err = httpErrorFromResponse(resp)
	}
	if err != nil {
		return fmt.Errorf("put %s: %w", req.url.Redacted(), err)
	}
	return err
}

// computeOrigin marshals the [Origin header] for a request to URL u coming from the origin URL.
// include is true if u is not the same origin as origin
// or if method is not GET or HEAD.
//
// [Origin header]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Origin
func computeOrigin(method string, u, origin *url.URL) (_ string, include bool) {
	origin = cmp.Or(origin, u)
	include = (method != "" && method != http.MethodGet && method != http.MethodHead) ||
		u.Scheme != origin.Scheme || u.Host != origin.Host
	if origin.Scheme != "http" && origin.Scheme != "https" {
		return "null", true
	}
	return origin.Scheme + "://" + origin.Host, include
}

// requestNegotiation is the combination of the [content negotiation fields]
// and the [Allow header field].
// Nil or the zero value is equivalent to the headers being absent.
//
// [Allow header field]: https://datatracker.ietf.org/doc/html/rfc9110#section-10.2.1
// [content negotiation fields]: https://datatracker.ietf.org/doc/html/rfc9110#section-12.5
type requestNegotiation struct {
	// allow is the set of methods parsed from the Allow header.
	// If allow is nil, then no advertisement has been made
	// and all methods are implicitly allowed.
	allow sets.Set[string]
	// acceptEncoding is a list of Accept-Encoding field values.
	acceptEncoding []string
}

func requestNegotiationFromResponseHeader(h http.Header) *requestNegotiation {
	rneg := &requestNegotiation{
		acceptEncoding: slices.Clone(h.Values("Accept-Encoding")),
	}
	if allowValues := h.Values("Allow"); len(allowValues) > 0 {
		rneg.allow = make(sets.Set[string])
		for _, value := range allowValues {
			for elem := range xhttp.SplitList(value) {
				rneg.allow.Add(elem)
			}
		}
	}
	return rneg
}

func requestNegotiationFromFetchResponse(resp *fetchResponse, err error) *requestNegotiation {
	if resp != nil {
		return &resp.requestNegotiation
	}
	if h, ok := errors.AsType[*httpError](err); ok {
		return &h.requestNegotiation
	}
	return nil
}

func (rn *requestNegotiation) isMethodAllowed(method string) bool {
	return rn == nil || rn.allow == nil || rn.allow.Has(method)
}

type httpError struct {
	statusCode         int
	status             string
	requestNegotiation requestNegotiation
}

func httpErrorFromResponse(resp *http.Response) error {
	err := &httpError{
		statusCode:         resp.StatusCode,
		status:             cmp.Or(resp.Status, http.StatusText(resp.StatusCode)),
		requestNegotiation: *requestNegotiationFromResponseHeader(resp.Header),
	}
	if 100 <= resp.StatusCode && resp.StatusCode < 400 {
		return fmt.Errorf("unexpected %w", err)
	}
	return err
}

func (e *httpError) Error() string {
	status := e.status
	if status == "" {
		status = http.StatusText(e.statusCode)
		if status == "" {
			status = strconv.Itoa(e.statusCode)
		}
	}
	return "http " + status
}

func errorStatusCode(err error) (statusCode int, ok bool) {
	if err == nil {
		return http.StatusOK, false
	}
	var h *httpError
	if !errors.As(err, &h) {
		return http.StatusInternalServerError, false
	}
	return h.statusCode, true
}

func isNotFound(err error) bool {
	code, _ := errorStatusCode(err)
	return code == http.StatusNotFound || code == http.StatusGone
}

// isUnsupportedContentCoding reports whether err indicates that an HTTP request failed
// due to an unsupported Content-Encoding header,
// and if so, returns the values of the Accept-Encoding header field.
func isUnsupportedContentCoding(err error) (acceptEncoding []string, ok bool) {
	h, ok := errors.AsType[*httpError](err)
	if !ok {
		return nil, false
	}
	// As per [RFC 9110 Section 12.5.3]:
	// "servers that fail a request with a 415 status for reasons unrelated to content codings
	// MUST NOT include the Accept-Encoding header field."
	//
	// [RFC 9110 Section 12.5.3]: https://datatracker.ietf.org/doc/html/rfc9110#section-12.5.3
	if h.statusCode != http.StatusUnsupportedMediaType || len(h.requestNegotiation.acceptEncoding) == 0 {
		return nil, false
	}
	return h.requestNegotiation.acceptEncoding, true
}

type methodNotAllowedError struct {
	method string
}

func (e methodNotAllowedError) Error() string {
	return e.method + " not allowed"
}

func isMethodNotAllowed(err error) bool {
	if _, ok := errors.AsType[methodNotAllowedError](err); ok {
		return true
	}
	code, _ := errorStatusCode(err)
	return code == http.StatusMethodNotAllowed || code == http.StatusNotImplemented
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
