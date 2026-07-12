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
)

func fetch(ctx context.Context, client *http.Client, u *url.URL, accept string) ([]byte, error) {
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
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %v: %w", u.Redacted(), &httpError{
			statusCode: resp.StatusCode,
			status:     resp.Status,
		})
	}
	const mebibyte = 1 << 20
	const maxSize = 4 * mebibyte
	if resp.ContentLength > maxSize {
		return nil, fmt.Errorf("fetch %v: response too large (%.1f MiB)", u.Redacted(), float64(resp.ContentLength)/mebibyte)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		return nil, fmt.Errorf("fetch %v: %v", u.Redacted(), err)
	}
	if resp.ContentLength == -1 && len(data) == maxSize {
		if n, _ := resp.Body.Read(make([]byte, 1)); n > 0 {
			return nil, fmt.Errorf("fetch %v: response too large", u.Redacted())
		}
	}
	if e := resp.Header.Get("Content-Encoding"); e != "" {
		dec, err := httpencoding.Decode(bytes.NewReader(data), e)
		if err != nil {
			return nil, fmt.Errorf("fetch %v: %v", u.Redacted(), err)
		}
		defer dec.Close()
		data, err = io.ReadAll(dec)
		if err != nil {
			return nil, fmt.Errorf("fetch %v: %v", u.Redacted(), err)
		}
	}
	return data, nil
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
