// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package remotestore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"zb.256lights.llc/pkg/internal/httpencoding"
	"zb.256lights.llc/pkg/internal/useragent"
	"zb.256lights.llc/pkg/internal/xhttp"
	"zb.256lights.llc/pkg/sets"
	"zombiezen.com/go/log"
)

type resource struct {
	body  []byte
	allow sets.Set[string]
}

func (res *resource) isMethodAllowed(method string) bool {
	return res == nil || len(res.allow) == 0 || res.allow.Has(method)
}

func fetch(ctx context.Context, client *http.Client, u *url.URL, accept string) (*resource, error) {
	req := (&http.Request{
		Method: http.MethodGet,
		URL:    u,
		Header: http.Header{
			"Accept":          {accept},
			"Accept-Encoding": {httpencoding.Accept},
			"User-Agent":      {useragent.String},
		},
	}).WithContext(ctx)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %v: %v", u.Redacted(), err)
	}
	defer resp.Body.Close()

	result := &resource{}
	if allow := resp.Header.Values("Allow"); len(allow) > 0 {
		result.allow = make(sets.Set[string])
		for _, value := range allow {
			for elem := range xhttp.SplitList(value) {
				result.allow.Add(elem)
			}
		}
	}
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("fetch %v: %w", u.Redacted(), &httpError{
			statusCode: resp.StatusCode,
			status:     resp.Status,
		})
	}

	const mebibyte = 1 << 20
	const maxSize = 4 * mebibyte
	if resp.ContentLength > maxSize {
		return result, fmt.Errorf("fetch %v: response too large (%.1f MiB)", u.Redacted(), float64(resp.ContentLength)/mebibyte)
	}
	result.body, err = io.ReadAll(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		return result, fmt.Errorf("fetch %v: %v", u.Redacted(), err)
	}
	if resp.ContentLength == -1 && len(result.body) == maxSize {
		if n, _ := resp.Body.Read(make([]byte, 1)); n > 0 {
			return result, fmt.Errorf("fetch %v: response too large", u.Redacted())
		}
	}
	if e := resp.Header.Get("Content-Encoding"); e != "" {
		dec, err := httpencoding.Decode(bytes.NewReader(result.body), e)
		if err != nil {
			return result, fmt.Errorf("fetch %v: %v", u.Redacted(), err)
		}
		defer dec.Close()
		result.body, err = io.ReadAll(dec)
		if err != nil {
			return result, fmt.Errorf("fetch %v: %v", u.Redacted(), err)
		}
	}
	return result, nil
}

type putRequest struct {
	url *url.URL

	content       io.Reader
	contentLength int64
	contentType   string

	noReplace    bool
	cacheControl string
}

func put(ctx context.Context, client *http.Client, req *putRequest) error {
	httpRequest := &http.Request{
		Method: http.MethodPut,
		URL:    req.url,
		Header: http.Header{
			"Content-Type": {req.contentType},
			"User-Agent":   {useragent.String},
		},
		ContentLength: -1,
		Body:          io.NopCloser(req.content),
	}
	if req.contentLength >= 0 {
		httpRequest.Header.Set("Content-Length", strconv.FormatInt(req.contentLength, 10))
		httpRequest.ContentLength = req.contentLength
	}
	if req.noReplace {
		httpRequest.Header.Set("If-None-Match", "*")
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

func isMethodNotAllowed(err error) bool {
	code, _ := errorStatusCode(err)
	return code == http.StatusMethodNotAllowed || code == http.StatusNotImplemented
}
