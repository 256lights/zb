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
	"strconv"

	"zb.256lights.llc/pkg/internal/httpencoding"
	"zb.256lights.llc/pkg/internal/xhttp"
	"zb.256lights.llc/pkg/sets"
	"zombiezen.com/go/log"
)

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
	body       []byte
	allow      sets.Set[string]
	validators xhttp.ValidatorFields
}

func (res *fetchResponse) isMethodAllowed(method string) bool {
	return res == nil || len(res.allow) == 0 || res.allow.Has(method)
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

	result := &fetchResponse{
		validators: xhttp.ExtractValidatorFields(resp.Header),
	}
	if allow := resp.Header.Values("Allow"); len(allow) > 0 {
		result.allow = make(sets.Set[string])
		for _, value := range allow {
			for elem := range xhttp.SplitList(value) {
				result.allow.Add(elem)
			}
		}
	}
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("fetch %v: %w", req.url.Redacted(), &httpError{
			statusCode: resp.StatusCode,
			status:     resp.Status,
		})
	}

	const mebibyte = 1 << 20
	const maxSize = 4 * mebibyte
	if resp.ContentLength >= 0 && resp.ContentLength > maxSize {
		return result, fmt.Errorf("fetch %v: response too large (%.1f MiB)",
			req.url.Redacted(), float64(resp.ContentLength)/mebibyte)
	}
	result.body, err = io.ReadAll(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		return result, fmt.Errorf("fetch %v: %v", req.url.Redacted(), err)
	}
	if resp.ContentLength == -1 && len(result.body) == maxSize {
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

	content       io.Reader
	contentLength int64
	contentType   string

	noReplace    bool
	precondition xhttp.ValidatorFields
	cacheControl string
}

func put(ctx context.Context, client Client, req *putRequest) error {
	if req.noReplace && !req.precondition.IsZero() {
		return fmt.Errorf("put %s: precondition combined with If-None-Match:*", req.url.Redacted())
	}

	httpRequest := &http.Request{
		Method: http.MethodPut,
		URL:    req.url,
		Header: http.Header{
			"Content-Type": {req.contentType},
		},
		ContentLength: -1,
		Body:          io.NopCloser(req.content),
	}
	if req.contentLength >= 0 {
		httpRequest.Header.Set("Content-Length", strconv.FormatInt(req.contentLength, 10))
		httpRequest.ContentLength = req.contentLength
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
	log.Debugf(ctx, "PUT %s Content-Length:%d", req.url.Redacted(), req.contentLength)
	resp, err := client.Do(httpRequest.WithContext(ctx))
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil &&
		resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusCreated &&
		resp.StatusCode != http.StatusNoContent {
		err = &httpError{
			statusCode: resp.StatusCode,
			status:     resp.Status,
		}
	}
	if err != nil {
		return fmt.Errorf("put %s: %v", req.url.Redacted(), err)
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

type httpError struct {
	statusCode int
	status     string
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
