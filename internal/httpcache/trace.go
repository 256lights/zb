// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package httpcache

import (
	"cmp"
	"context"
	"net/http"
	"net/url"
)

// RequestInfo stores information about an inbound request to the cache.
type RequestInfo struct {
	Method string
	URL    *url.URL
}

func newRequestInfo(req *http.Request) *RequestInfo {
	return &RequestInfo{
		Method: cmp.Or(req.Method, http.MethodGet),
		URL:    new(*req.URL),
	}
}

// An ErrorReporter is a type that receives errors from a [RoundTripper].
// It must be safe to call ReportError from multiple goroutines simultaneously.
// info may be nil if the error is part of some background work.
type ErrorReporter interface {
	ReportError(ctx context.Context, info *RequestInfo, err error)
}

// ErrorReporterFunc is a function that implements [ErrorReporter].
type ErrorReporterFunc func(ctx context.Context, info *RequestInfo, err error)

// ReportError implements [ErrorReporter] by calling f.
func (f ErrorReporterFunc) ReportError(ctx context.Context, info *RequestInfo, err error) {
	f(ctx, info, err)
}
