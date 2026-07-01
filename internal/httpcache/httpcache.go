// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

// Package httpcache provides a client-side HTTP cache backed by a SQLite database
// that conforms to [RFC 9111].
//
// [RFC 9111]: https://www.rfc-editor.org/rfc/rfc9111.html
package httpcache

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"zb.256lights.llc/pkg/internal/xslices"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitefile"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
)

// Options contains optional arguments to [Open].
// nil is treated the same as the zero value.
type Options struct {
	// MaxConcurrentRequests specifies a limit on the number of requests
	// that can concurrently access the cache.
	// If less than 1, then a reasonable default is used.
	MaxConcurrentRequests int

	// If MaxResponseSize is positive, then any responses with a body
	// whose size in bytes is greater than MaxResponseSize will not be cached.
	MaxResponseSize int64

	// RequestCoalescingCutoff is the amount of time to wait
	// for other concurrent requests to the same URL.
	// Zero or negative values disable request coalescing.
	RequestCoalescingCutoff time.Duration

	// ErrorReporter is the reporter to be used when a failure is encountered
	// that does not prevent a call to [*RoundTripper.RoundTrip] from succeeding.
	// Such failures are typically errors in reading or writing from the database.
	ErrorReporter ErrorReporter
}

// A RoundTripper is an HTTP cache middleware of a [http.RoundTripper].
// RoundTripper is safe for concurrent use by multiple goroutines.
type RoundTripper struct {
	db                      *sqlitemigration.Pool
	roundTripper            http.RoundTripper
	maxResponseSize         int64
	requestCoalescingCutoff time.Duration
	errorReporter           ErrorReporter

	backgroundContext       context.Context
	cancelBackgroundContext context.CancelFunc
	backgroundTasks         sync.WaitGroup
}

// Open returns a new [*RoundTripper] that persists cache data to the file at dbPath
// and forwards uncached requests to the given [http.RoundTripper].
// The caller is responsible for calling [*RoundTripper.Close]
// when the [*RoundTripper] is no longer used.
func Open(dbPath string, roundTripper http.RoundTripper, opts *Options) *RoundTripper {
	if roundTripper == nil {
		panic("nil http.RoundTripper")
	}

	rt := &RoundTripper{
		roundTripper:            roundTripper,
		maxResponseSize:         opts.MaxResponseSize,
		requestCoalescingCutoff: opts.RequestCoalescingCutoff,
		errorReporter:           opts.ErrorReporter,
	}
	rt.backgroundContext, rt.cancelBackgroundContext = context.WithCancel(context.Background())

	opts = cmp.Or(opts, new(Options))
	var onDBError sqlitemigration.ReportFunc
	if opts.ErrorReporter != nil {
		errorReporter := opts.ErrorReporter
		onDBError = func(err error) {
			errorReporter.ReportError(context.Background(), nil, err)
		}
	}
	poolSize := opts.MaxConcurrentRequests
	if poolSize < 1 {
		poolSize = 5
	}
	rt.db = sqlitemigration.NewPool(dbPath, schema(), sqlitemigration.Options{
		Flags:       sqlite.OpenReadWrite | sqlite.OpenCreate,
		PrepareConn: prepareConn,
		PoolSize:    poolSize,
		OnError:     onDBError,
		OnReady: func() {
			rt.backgroundTasks.Go(func() {
				rt.optimize(rt.backgroundContext)
			})
		},
	})

	return rt
}

// Close releases all resources associated with rt.
// Close waits until all response bodies being read from the cache are closed.
func (rt *RoundTripper) Close() error {
	rt.CloseIdleConnections()
	rt.cancelBackgroundContext()
	rt.backgroundTasks.Wait()
	return rt.db.Close()
}

// CloseIdleConnections calls CloseIdleConnections()
// on the underlying [http.RoundTripper], if present.
func (rt *RoundTripper) CloseIdleConnections() {
	cic, ok := rt.roundTripper.(interface {
		CloseIdleConnections()
	})
	if ok {
		cic.CloseIdleConnections()
	}
}

func (rt *RoundTripper) reportError(req *http.Request, err error) {
	if err == nil || rt == nil || rt.errorReporter == nil {
		return
	}
	rt.errorReporter.ReportError(req.Context(), newRequestInfo(req), err)
}

// optimize is a long-running goroutine that periodically runs "PRAGMA optimize;".
// See the [docs for ANALYZE] for more details.
//
// [docs for ANALYZE]: https://www.sqlite.org/lang_analyze.html#recommended_usage_patterns
func (rt *RoundTripper) optimize(ctx context.Context) {
	conn, err := rt.db.Get(ctx)
	if err != nil {
		return
	}
	err = optimizeDBFull(conn)
	rt.db.Put(conn)
	if err != nil {
		if rt.errorReporter != nil {
			rt.errorReporter.ReportError(ctx, nil, err)
		}
		return
	}

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}

		conn, err := rt.db.Get(ctx)
		if err != nil {
			return
		}
		err = optimizeDB(conn)
		rt.db.Put(conn)
		if err != nil {
			if rt.errorReporter != nil {
				rt.errorReporter.ReportError(ctx, nil, err)
			}
			return
		}
	}
}

// RoundTrip implements [http.RoundTripper].
func (rt *RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	// TODO(someday): Handle Range requests.
	if !isCacheableMethod(req) || len(req.Header["Range"]) > 0 {
		resp, err := rt.roundTripper.RoundTrip(req)
		if err == nil && !isSafeMethod(req) && 200 <= resp.StatusCode && resp.StatusCode < 400 {
			// Unsafe request succeeded: invalidate cache.
			err := func() (err error) {
				conn, err := rt.db.Get(ctx)
				if err != nil {
					return err
				}
				defer rt.db.Put(conn)
				endFn, err := sqlitex.ImmediateTransaction(conn)
				if err != nil {
					return err
				}
				defer endFn(&err)
				return clearURL(conn, req.URL.String())
			}()
			rt.reportError(req, err)
		}
		return resp, err
	}

	conn, err := rt.db.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("http cache: %v", err)
	}
	connInUse := false
	defer func() {
		if !connInUse {
			rt.db.Put(conn)
		}
	}()

	requestDirectives := newCacheControlRequestDirectives(cacheControlDirectives(req.Header))
	var responses []*storedResponse
	coalesceContext := ctx
	var cancelCoalesceContext context.CancelFunc = func() {}
	if rt.requestCoalescingCutoff > 0 {
		coalesceContext, cancelCoalesceContext = context.WithTimeout(coalesceContext, rt.requestCoalescingCutoff)
		defer cancelCoalesceContext()
	}
	for t := new(backoffTimer); ; {
		var err error
		responses, err = readCache(conn, req.URL)
		rt.reportError(req, err)

		if !requestDirectives.noCache {
			// Find a fresh response.
			cacheCheckTime := time.Now()
			for _, resp := range responses {
				if !resp.responseReceived() ||
					hasNoCacheDirective(cacheControlDirectives(resp.responseHeader)) ||
					!resp.matchesRequestHeader(varyHeader(resp.responseHeader), req.Header) {
					continue
				}
				if age, fresh := resp.isFresh(cacheCheckTime, requestDirectives); fresh {
					var body io.ReadCloser
					if req.Method == http.MethodHead {
						body = http.NoBody
					} else {
						connInUse = true // Database connection will stay open until body is closed.
						body = rt.newStoredResponseBody(conn, resp.id)
					}
					hr := resp.toResponse(body)
					hr.Header.Set("Age", formatDeltaSeconds(age))
					return hr, nil
				}
			}
		}

		if rt.requestCoalescingCutoff <= 0 || !hasUnreceivedResponses(slices.Values(responses)) {
			break
		}
		if err := t.wait(coalesceContext); err != nil {
			// Don't return the error here:
			// we'll use the last set of responses read from the cache.
			// The request's context (hopefully) has a longer deadline.
			break
		}
	}
	cancelCoalesceContext()

	// Only stale responses.
	if requestDirectives.onlyIfCached {
		const message = "Cached response not available"
		return &http.Response{
			Proto:         "HTTP/1.1",
			ProtoMajor:    1,
			ProtoMinor:    1,
			StatusCode:    http.StatusGatewayTimeout,
			Status:        http.StatusText(http.StatusGatewayTimeout),
			ContentLength: int64(len(message)),
			Header: http.Header{
				"Content-Type":           {"text/plain; charset=utf-8"},
				"Content-Length":         {strconv.Itoa(len(message))},
				"X-Content-Type-Options": {"nosniff"},
				"Date":                   {time.Now().UTC().Format(http.TimeFormat)},
			},
			Body:    io.NopCloser(strings.NewReader(message)),
			Request: req,
		}, nil
	}
	responses = xslices.Filter(responses, (*storedResponse).responseReceived)

	startedAt := time.Now()
	ch := make(chan allocateResourceResult)
	go func() {
		// Write a placeholder resource concurrently with the request.
		var id int64
		err := func() (err error) {
			endFn, err := sqlitex.ImmediateTransaction(conn)
			if err != nil {
				return err
			}
			defer endFn(&err)
			id, err = allocateResource(conn, req.URL, startedAt)
			return err
		}()
		ch <- allocateResourceResult{id, err}
	}()

	result, forwardError := forward(rt.roundTripper, req, responses)
	idResult := <-ch
	if forwardError != nil || idResult.error != nil || result.serveBodyFromCache || requestDirectives.noStore || req.Method == http.MethodHead || !rt.canStore(result) {
		if idResult.error == nil || (result != nil && len(result.freshenResponses)+len(result.staleResponseIDs) > 0) {
			err := func() (err error) {
				endFn, err := sqlitex.ImmediateTransaction(conn)
				if err != nil {
					return err
				}
				defer endFn(&err)
				if idResult.error == nil {
					rt.reportError(req, deleteResource(conn, idResult.id))
				}
				if result != nil {
					for _, stored := range result.freshenResponses {
						rt.reportError(req, updateCache(conn, stored))
					}
					for _, id := range result.staleResponseIDs {
						rt.reportError(req, markStale(conn, id))
					}
				}
				return nil
			}()
			rt.reportError(req, err)
		}
		if result != nil && result.serveBodyFromCache {
			// forward already cleared result.response.Body, so no need to close.
			connInUse = true
			result.response.Body = rt.newStoredResponseBody(conn, result.storedResponseID)
		}
		if result == nil {
			return nil, forwardError
		}
		return result.response, nil
	}

	bodyBuffer, err := sqlitefile.NewBuffer(conn)
	if err != nil {
		rt.reportError(req, deleteResource(conn, idResult.id))
		return result.response, nil
	}
	bodyBufferInUse := false
	defer func() {
		if !bodyBufferInUse {
			bodyBuffer.Close()
		}
	}()

	if _, err := limitedCopy(bodyBuffer, result.response.Body, rt.maxResponseSize); err != nil {
		// If we failed to copy the full body, then don't write to cache.
		// Return the response and read the bytes back from the temporary buffer,
		// followed by the rest of the body from the network.
		err := func() (err error) {
			endFn, err := sqlitex.ImmediateTransaction(conn)
			if err != nil {
				return err
			}
			defer endFn(&err)

			rt.reportError(req, deleteResource(conn, idResult.id))
			for _, stored := range result.freshenResponses {
				rt.reportError(req, updateCache(conn, stored))
			}
			for _, id := range result.staleResponseIDs {
				rt.reportError(req, markStale(conn, id))
			}
			return nil
		}()
		rt.reportError(req, err)

		result.response.Body = rt.newBufferedResponseBody(conn, bodyBuffer, result.response.Body)
		bodyBufferInUse = true
		connInUse = true
		return result.response, nil
	}
	err = func() (err error) {
		endFn, err := sqlitex.ImmediateTransaction(conn)
		if err != nil {
			return err
		}
		defer endFn(&err)

		toWrite, err := result.newStoredResponse(idResult.id, bodyBuffer.Len())
		if err != nil {
			return err
		}
		err = writeCache(conn, toWrite, bodyBuffer)
		if err != nil {
			rt.reportError(req, deleteResource(conn, idResult.id))
			return err
		}

		for _, id := range result.staleResponseIDs {
			rt.reportError(req, deleteResource(conn, id))
		}

		return nil
	}()
	if err != nil {
		result.response.Body.Close()
		return nil, err
	}

	result.response.Body.Close()
	connInUse = true
	result.response.Body = rt.newStoredResponseBody(conn, idResult.id)
	return result.response, nil
}

func (rt *RoundTripper) canStore(result *forwardResult) bool {
	if result == nil ||
		result.response == nil ||
		!canStoreResponse(result.requestHeader, result.response.StatusCode, result.response.Header) ||
		result.serveBodyFromCache {
		return false
	}
	if rt.maxResponseSize > 0 {
		if n, _ := contentLength(result.response.Header); n > rt.maxResponseSize {
			return false
		}
	}
	return true
}

// canStoreRequestHeader reports whether a header field with the given canonical key
// is allowed to be stored in the cache.
func canStoreRequestHeader(key string) bool {
	return key != "Authorization"
}

// canStoreResponse reports whether a private cache
// can store a response with the given status code and headers.
//
// [Section 3 of RFC 9111]: https://www.rfc-editor.org/rfc/rfc9111.html#section-3
func canStoreResponse(requestHeader http.Header, statusCode int, responseHeader http.Header) bool {
	if !isFinalStatusCode(statusCode) || statusCode == http.StatusPartialContent {
		return false
	}
	if vary := varyHeader(responseHeader); !vary.hasWildcard() {
		// Additional requirement beyond RFC: must be able store request header from Vary key.
		for key := range vary.fieldNames() {
			if len(requestHeader[key]) > 0 && !canStoreRequestHeader(key) {
				return false
			}
		}
	}
	canStore := isCacheableStatusCode(statusCode) || len(responseHeader["Expires"]) > 0
	noStore := false
	mustUnderstand := false
	for d := range cacheControlDirectives(responseHeader) {
		switch {
		case d.nameMatches("no-store") && d.rawArgument == "":
			noStore = true
		case !canStore && (d.nameMatches("private") ||
			d.nameMatches("public") && d.rawArgument == "" ||
			d.nameMatches("max-age") && d.rawArgument != ""):
			canStore = true
		case d.nameMatches("must-understand") && d.rawArgument == "":
			if !isStatusCodeUnderstood(statusCode) {
				return false
			}
			mustUnderstand = true
		}
	}
	return canStore && (!noStore || mustUnderstand)
}

// isStatusCodeUnderstood reports whether the HTTP status code
// is one of those that this cache implementation understands the semantics of.
func isStatusCodeUnderstood(code int) bool {
	return code == http.StatusOK ||
		code == http.StatusCreated ||
		code == http.StatusAccepted ||
		code == http.StatusNonAuthoritativeInfo ||
		code == http.StatusNoContent ||
		code == http.StatusMultipleChoices ||
		code == http.StatusMovedPermanently ||
		code == http.StatusFound ||
		code == http.StatusSeeOther ||
		code == http.StatusNotModified ||
		code == http.StatusTemporaryRedirect ||
		code == http.StatusPermanentRedirect ||
		code == http.StatusBadRequest ||
		code == http.StatusUnauthorized ||
		code == http.StatusForbidden ||
		code == http.StatusNotFound ||
		code == http.StatusMethodNotAllowed ||
		code == http.StatusNotAcceptable ||
		code == http.StatusProxyAuthRequired ||
		code == http.StatusRequestTimeout ||
		code == http.StatusConflict ||
		code == http.StatusGone ||
		code == http.StatusPreconditionFailed ||
		code == http.StatusRequestURITooLong ||
		code == http.StatusExpectationFailed ||
		code == http.StatusMisdirectedRequest ||
		code == http.StatusUnprocessableEntity ||
		code == http.StatusUpgradeRequired ||
		code == http.StatusInternalServerError ||
		code == http.StatusNotImplemented ||
		code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusGatewayTimeout
}

func readCache(conn *sqlite.Conn, u *url.URL) (result []*storedResponse, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("read cache for %s: %v", u.Redacted(), err)
		}
	}()

	defer sqlitex.Save(conn)(&err)

	stmt := prepareQuery(conn, "resources/find.sql")
	responseReceivedAtColumn := stmt.ColumnIndex("response_received_at")
	responseBodySizeColumn := stmt.ColumnIndex("response_body_size")
	stmt.SetText(":url", u.String())
	for {
		hasRow, err := stmt.Step()
		if err != nil {
			return nil, err
		}
		if !hasRow {
			return result, nil
		}
		resp := &storedResponse{
			id:               stmt.GetInt64("id"),
			stale:            stmt.GetBool("stale"),
			statusCode:       int(stmt.GetInt64("status_code")),
			requestedAt:      time.UnixMilli(stmt.GetInt64("requested_at")),
			responseBodySize: -1,
		}
		resp.requestHeader, err = fetchRequestHeaders(conn, resp.id)
		if err != nil {
			return nil, err
		}
		resp.responseHeader, err = fetchResponseHeaders(conn, resp.id)
		if err != nil {
			return nil, err
		}
		if stmt.ColumnType(responseReceivedAtColumn) != sqlite.TypeNull {
			resp.responseReceivedAt = time.UnixMilli(stmt.ColumnInt64(responseReceivedAtColumn))
		}
		if stmt.ColumnType(responseBodySizeColumn) != sqlite.TypeNull {
			resp.responseBodySize = stmt.ColumnInt64(responseBodySizeColumn)
		}
		result = append(result, resp)
	}
}

type allocateResourceResult struct {
	id    int64
	error error
}

func allocateResource(conn *sqlite.Conn, u *url.URL, requestedAt time.Time) (id int64, err error) {
	stmt := prepareQuery(conn, "resources/insert.sql")
	stmt.SetText(":url", u.String())
	stmt.SetInt64(":requested_at", requestedAt.UnixMilli())
	id, err = sqlitex.ResultInt64(stmt)
	if err != nil {
		return 0, fmt.Errorf("insert cache placeholder for %s %s: %w", http.MethodGet, u.Redacted(), err)
	}
	return id, nil
}

func deleteResource(conn *sqlite.Conn, id int64) error {
	stmt := prepareQuery(conn, "resources/delete.sql")
	stmt.SetInt64(":id", id)
	if err := runStatement(stmt); err != nil {
		return fmt.Errorf("delete cached response id=%d: %w", id, err)
	}
	return nil
}

func writeCache(conn *sqlite.Conn, resp *storedResponse, body io.Reader) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("write cache: %v", err)
		}
	}()
	defer sqlitex.Save(conn)(&err)

	if err := prepareCacheResponse(conn, resp); err != nil {
		return err
	}

	hu := prepareHeaderUpserter(conn)
	requestHeaderStmt := prepareQuery(conn, "resources/insert_request_header.sql")
	requestHeaderStmt.SetInt64(":id", resp.id)
	for key, values := range resp.requestHeader {
		if len(values) == 0 {
			continue
		}
		if !canStoreRequestHeader(key) {
			return fmt.Errorf("insert %s request header: cannot store", key)
		}
		if len(values) > 1 {
			return fmt.Errorf("insert %s request header: multiple values", key)
		}
		headerID, err := hu.upsert(key, values[0])
		if err != nil {
			return err
		}
		requestHeaderStmt.SetInt64(":header_id", headerID)
		if err := runStatement(requestHeaderStmt); err != nil {
			return fmt.Errorf("insert %s request header: %v", key, err)
		}
	}

	responseHeaderStmt := prepareQuery(conn, "resources/append_response_header.sql")
	responseHeaderStmt.SetInt64(":id", resp.id)
	for key, values := range responseHeadersToStore(resp.responseHeader) {
		for _, value := range values {
			headerID, err := hu.upsert(key, value)
			if err != nil {
				return err
			}
			responseHeaderStmt.SetInt64(":header_id", headerID)
			if err := runStatement(responseHeaderStmt); err != nil {
				return fmt.Errorf("insert %s response header: %v", key, err)
			}
		}
	}

	initBodyStmt := prepareQuery(conn, "resources/init_body.sql")
	initBodyStmt.SetInt64(":id", resp.id)
	initBodyStmt.SetInt64(":size", resp.responseBodySize)
	if err := runStatement(initBodyStmt); err != nil {
		return fmt.Errorf("init body: %v", err)
	}
	blob, err := conn.OpenBlob("", "resources", "response_body", resp.id, true)
	if err != nil {
		return err
	}
	_, writeError := io.CopyN(blob, body, resp.responseBodySize)
	closeError := blob.Close()
	if err := cmp.Or(writeError, closeError); err != nil {
		return fmt.Errorf("save body: %v", err)
	}

	return nil
}

func updateCache(conn *sqlite.Conn, resp *storedResponse) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("update cache: %v", err)
		}
	}()
	defer sqlitex.Save(conn)(&err)

	if err := prepareCacheResponse(conn, resp); err != nil {
		return err
	}

	// Delete headers first.
	// There's a high likelihood we'll see the same headers again,
	// so then we'll clear everything and reset the index counter.
	deleteHeadersStmt := prepareQuery(conn, "resources/delete_response_headers.sql")
	deleteHeadersStmt.SetInt64(":id", resp.id)
	headersToUpdate := responseHeadersToUpdate(resp.responseHeader)
	for key := range headersToUpdate {
		deleteHeadersStmt.SetText(":name", key)
		if err := runStatement(deleteHeadersStmt); err != nil {
			return fmt.Errorf("delete %s headers for resource id=%d: %v", key, resp.id, err)
		}
	}

	hu := prepareHeaderUpserter(conn)
	responseHeaderStmt := prepareQuery(conn, "resources/append_response_header.sql")
	responseHeaderStmt.SetInt64(":id", resp.id)
	for key, values := range headersToUpdate {
		for _, value := range values {
			headerID, err := hu.upsert(key, value)
			if err != nil {
				return err
			}
			responseHeaderStmt.SetInt64(":header_id", headerID)
			if err := runStatement(responseHeaderStmt); err != nil {
				return fmt.Errorf("insert %s response header: %v", key, err)
			}
		}
	}

	if err := gcHeaders(conn); err != nil {
		return err
	}
	return nil
}

func markStale(conn *sqlite.Conn, id int64) error {
	stmt := prepareQuery(conn, "resources/mark_stale.sql")
	stmt.SetInt64(":id", id)
	if err := runStatement(stmt); err != nil {
		return fmt.Errorf("mark resource id=%d stale: %w", id, err)
	}
	return nil
}

// prepareCacheResponse updates the metadata for the response.
func prepareCacheResponse(conn *sqlite.Conn, resp *storedResponse) error {
	stmt := prepareQuery(conn, "resources/prepare_response.sql")
	stmt.SetInt64(":id", resp.id)
	stmt.SetInt64(":requested_at", resp.requestedAt.UnixMilli())
	stmt.SetInt64(":received_at", resp.responseReceivedAt.UnixMilli())
	stmt.SetInt64(":status_code", int64(resp.statusCode))
	if err := runStatement(stmt); err != nil {
		return fmt.Errorf("response metadata: %v", err)
	}
	return nil
}

// responseHeadersToStore returns an iterator over the response headers
// that should be stored according to [Section 3.1 of RFC 9111].
// The returned iterator may be used multiple times,
// but the set of headers to omit is computed before responseHeadersToStore returns.
//
// [Section 3.1 of RFC 9111]: https://www.rfc-editor.org/rfc/rfc9111.html#section-3.1
func responseHeadersToStore(header http.Header) iter.Seq2[string, []string] {
	return headersExcept(header, headersToNotStore(header))
}

// responseHeadersToStore returns an iterator over the response headers
// that should be stored after a validation request according to [Section 3.2 of RFC 9111].
// The returned iterator may be used multiple times,
// but the set of headers to omit is computed before responseHeadersToStore returns.
//
// [Section 3.2 of RFC 9111]: https://www.rfc-editor.org/rfc/rfc9111.html#section-3.2
func responseHeadersToUpdate(header http.Header) iter.Seq2[string, []string] {
	skip := headersToNotStore(header)
	skip["Content-Length"] = struct{}{}
	return headersExcept(header, skip)
}

func headersToNotStore(header http.Header) map[string]struct{} {
	skip := map[string]struct{}{
		"Connection":                {},
		"Proxy-Connection":          {},
		"Keep-Alive":                {},
		"Te":                        {},
		"Transfer-Encoding":         {},
		"Upgrade":                   {},
		"Proxy-Authenticate":        {},
		"Proxy-Authentication-Info": {},
		"Proxy-Authorization":       {},
	}
	for d := range cacheControlDirectives(header) {
		if d.nameMatches("no-cache") {
			if arg, ok := d.argument(); ok {
				for name := range splitList(arg) {
					if name != "" && tokenEnd(name) == len(name) {
						skip[http.CanonicalHeaderKey(name)] = struct{}{}
					}
				}
			}
		}
	}
	for _, value := range header["Connection"] {
		for name := range splitList(value) {
			if name != "" && tokenEnd(name) == len(name) {
				skip[http.CanonicalHeaderKey(name)] = struct{}{}
			}
		}
	}
	return skip
}

func headersExcept(header http.Header, skip map[string]struct{}) iter.Seq2[string, []string] {
	return func(yield func(string, []string) bool) {
		for key, values := range header {
			if _, shouldSkip := skip[key]; !shouldSkip {
				if !yield(key, values) {
					return
				}
			}
		}
	}
}

func clearURL(conn *sqlite.Conn, urlstr string) (err error) {
	defer func() {
		if err != nil {
			redactedURL := urlstr
			if u, parseError := url.Parse(urlstr); parseError == nil {
				redactedURL = u.Redacted()
			}
			err = fmt.Errorf("remove %s from cache: %w", redactedURL, err)
		}
	}()
	defer sqlitex.Save(conn)(&err)

	stmt := prepareQuery(conn, "resources/clear_url.sql")
	stmt.SetText(":url", urlstr)
	if err := runStatement(stmt); err != nil {
		return err
	}
	if err := gcHeaders(conn); err != nil {
		return err
	}
	return nil
}

func gcHeaders(conn *sqlite.Conn) error {
	stmt := prepareQuery(conn, "headers/gc.sql")
	if err := runStatement(stmt); err != nil {
		return fmt.Errorf("expunge unused headers: %w", err)
	}
	return nil
}

type headerUpserter struct {
	conn       *sqlite.Conn
	findStmt   *sqlite.Stmt
	insertStmt *sqlite.Stmt
}

func prepareHeaderUpserter(conn *sqlite.Conn) headerUpserter {
	return headerUpserter{
		conn:       conn,
		findStmt:   prepareQuery(conn, "headers/find.sql"),
		insertStmt: prepareQuery(conn, "headers/insert.sql"),
	}
}

func (hu headerUpserter) upsert(key, value string) (id int64, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("write %s header to cache: %w", key, err)
		}
	}()
	defer sqlitex.Save(hu.conn)(&err)

	hu.findStmt.BindText(1, key)
	hu.findStmt.BindText(2, value)
	hasRow, err := hu.findStmt.Step()
	if err != nil {
		hu.findStmt.Reset()
		return 0, err
	}
	if hasRow {
		id = hu.findStmt.ColumnInt64(0)
		hu.findStmt.Reset()
		return id, nil
	}
	hu.findStmt.Reset()

	hu.insertStmt.BindText(1, key)
	hu.insertStmt.BindText(2, value)
	return sqlitex.ResultInt64(hu.insertStmt)
}

type bufferedResponseBody struct {
	db     *sqlitemigration.Pool
	conn   *sqlite.Conn
	buffer *sqlitefile.Buffer
	body   io.ReadCloser
}

func (rt *RoundTripper) newBufferedResponseBody(conn *sqlite.Conn, buffer *sqlitefile.Buffer, body io.ReadCloser) *bufferedResponseBody {
	return &bufferedResponseBody{
		db:     rt.db,
		conn:   conn,
		buffer: buffer,
		body:   body,
	}
}

func (body *bufferedResponseBody) Read(p []byte) (n int, err error) {
	if body.buffer != nil {
		n, err = body.buffer.Read(p)
		if n > 0 {
			if err == io.EOF {
				err = nil
			}
			return n, err
		}
		if err != nil && err != io.EOF {
			return 0, err
		}
		body.closeBuffer()
	}

	return body.body.Read(p)
}

func (body *bufferedResponseBody) Close() error {
	err1 := body.closeBuffer()
	err2 := body.body.Close()
	return errors.Join(err1, err2)
}

func (body *bufferedResponseBody) closeBuffer() error {
	if body.buffer == nil {
		return nil
	}
	err := body.buffer.Close()
	body.buffer = nil
	body.db.Put(body.conn)
	body.db = nil
	body.conn = nil
	return err
}

type storedResponseBody struct {
	db   *sqlitemigration.Pool
	conn *sqlite.Conn
	id   int64
	blob *sqlite.Blob
}

func (rt *RoundTripper) newStoredResponseBody(conn *sqlite.Conn, resourceID int64) *storedResponseBody {
	return &storedResponseBody{
		db:   rt.db,
		conn: conn,
		id:   resourceID,
	}
}

func (body *storedResponseBody) Read(p []byte) (int, error) {
	if body.blob == nil {
		if body.conn == nil {
			return 0, fmt.Errorf("read cached response body: closed")
		}
		var err error
		body.blob, err = body.conn.OpenBlob("", "resources", "response_body", body.id, false)
		if err != nil {
			return 0, fmt.Errorf("read cached response body: %v", err)
		}
	}

	n, err := body.blob.Read(p)
	if err != nil && err != io.EOF {
		err = fmt.Errorf("read cached response body: %v", err)
	}
	return n, err
}

func (body *storedResponseBody) Close() error {
	var err error
	if body.blob != nil {
		err = body.blob.Close()
		body.blob = nil
	}
	if body.conn != nil {
		body.db.Put(body.conn)
		body.conn = nil
	}
	body.db = nil
	return err
}

func fetchRequestHeaders(conn *sqlite.Conn, id int64) (http.Header, error) {
	stmt := prepareQuery(conn, "request_headers.sql")
	stmt.SetInt64(":id", id)
	nameColumn := stmt.ColumnIndex("name")
	valueColumn := stmt.ColumnIndex("value")
	var result http.Header
	for {
		hasRow, err := stmt.Step()
		if err != nil {
			return nil, fmt.Errorf("read request id=%d headers from cache: %w", id, err)
		}
		if !hasRow {
			return result, nil
		}
		if result == nil {
			result = make(http.Header)
		}
		name := http.CanonicalHeaderKey(stmt.ColumnText(nameColumn))
		value := stmt.ColumnText(valueColumn)
		if v := result[name]; len(v) == 0 {
			result[name] = []string{value}
		} else {
			// Generally, we don't serialize like this because it would lose order,
			// but handle just in case.
			v[0] += headerFieldCombiner + value
			result[name] = v
		}
	}
}

func fetchResponseHeaders(conn *sqlite.Conn, id int64) (http.Header, error) {
	stmt := prepareQuery(conn, "response_headers.sql")
	stmt.SetInt64(":id", id)
	nameColumn := stmt.ColumnIndex("name")
	valueColumn := stmt.ColumnIndex("value")
	var result http.Header
	for {
		hasRow, err := stmt.Step()
		if err != nil {
			return nil, fmt.Errorf("read response id=%d headers from cache: %w", id, err)
		}
		if !hasRow {
			return result, nil
		}
		if result == nil {
			result = make(http.Header)
		}
		result.Add(stmt.ColumnText(nameColumn), stmt.ColumnText(valueColumn))
	}
}

func isCacheableMethod(req *http.Request) bool {
	return req.Method == "" || req.Method == http.MethodGet || req.Method == http.MethodHead
}

// isSafeMethod reports whether the request method's semantics are read-only
// according to [Section 9.2.1 of RFC 9110].
//
// [Section 9.2.1 of RFC 9110]: https://www.rfc-editor.org/rfc/rfc9110.html#section-9.2.1
func isSafeMethod(req *http.Request) bool {
	return req.Method == "" ||
		req.Method == http.MethodGet ||
		req.Method == http.MethodHead ||
		req.Method == http.MethodOptions ||
		req.Method == http.MethodTrace
}
