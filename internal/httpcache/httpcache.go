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
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"net/http"
	"net/url"
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
	// ErrorLogger is the logger to be used when a failure is encountered
	// that does not prevent a call to [*RoundTripper.RoundTrip] from succeeding.
	// Such failures are typically errors in reading or writing from the database.
	ErrorLogger ErrorReporter
}

type RoundTripper struct {
	db           *sqlitemigration.Pool
	roundTripper http.RoundTripper
	errorLogger  ErrorReporter
}

func Open(dbPath string, roundTripper http.RoundTripper, opts *Options) *RoundTripper {
	if roundTripper == nil {
		panic("nil http.RoundTripper")
	}

	opts = cmp.Or(opts, new(Options))
	var onDBError sqlitemigration.ReportFunc
	if opts.ErrorLogger != nil {
		errorLogger := opts.ErrorLogger
		onDBError = func(err error) {
			errorLogger.ReportError(context.Background(), nil, err)
		}
	}
	return &RoundTripper{
		roundTripper: roundTripper,
		db: sqlitemigration.NewPool(dbPath, schema(), sqlitemigration.Options{
			Flags:       sqlite.OpenReadWrite | sqlite.OpenCreate,
			PrepareConn: prepareConn,
			OnError:     onDBError,
		}),
		errorLogger: opts.ErrorLogger,
	}
}

func (rt *RoundTripper) Close() error {
	rt.CloseIdleConnections()
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
	if err == nil || rt == nil || rt.errorLogger == nil {
		return
	}
	rt.errorLogger.ReportError(req.Context(), newRequestInfo(req), err)
}

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
	// TODO(soon): defer rt.db.Put(conn)

	responses, err := readCache(conn, req.URL)
	if err != nil {
		// TODO(soon): Cache response.
		rt.db.Put(conn)
		return rt.roundTripper.RoundTrip(req)
	}

	// Find a fresh response.
	cacheCheckTime := time.Now()
	for _, resp := range responses {
		if !resp.responseReceived() || hasNoCacheDirective(cacheControlDirectives(resp.responseHeader)) || !resp.matchesRequestHeader(req.Header) {
			continue
		}
		if !cacheCheckTime.After(resp.date().Add(resp.freshnessLifetime())) {
			// No rt.db.Put: database connection will stay open until body is closed.
			hr := resp.toResponse(rt.newStoredResponseBody(conn, resp.id))
			hr.Header.Set("Age", formatDeltaSeconds(resp.ageAt(cacheCheckTime)))
			return hr, nil
		}
	}

	// TODO(soon): If there are placeholders, then block.

	// Only stale responses.
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
	if forwardError != nil || idResult.error != nil || result.serveBodyFromCache || !result.canStore() {
		if idResult.error == nil || result != nil && len(result.freshenResponses) > 0 {
			err := func() (err error) {
				endFn, err := sqlitex.ImmediateTransaction(conn)
				if err != nil {
					return err
				}
				defer endFn(&err)
				if idResult.error == nil {
					deleteResource(conn, idResult.id)
				}
				if result != nil {
					for _, stored := range result.freshenResponses {
						stored.responseHeader = result.response.Header
						if err := updateCache(conn, stored); err != nil {
							return err
						}
					}
				}
				return nil
			}()
			rt.reportError(req, err)
		}
		if result != nil && result.serveBodyFromCache {
			result.response.Body = rt.newStoredResponseBody(conn, result.storedResponseID)
		} else {
			rt.db.Put(conn)
		}
		if result == nil {
			return nil, forwardError
		}
		return result.response, nil
	}

	// TODO(soon): Size limit?
	bodyBuffer, err := sqlitefile.NewBuffer(conn)
	if err != nil {
		rt.reportError(req, deleteResource(conn, idResult.id))
		rt.db.Put(conn)
		return result.response, nil
	}
	if _, err := io.Copy(bodyBuffer, result.response.Body); err != nil {
		// TODO(soon): Return response with partial body from database.
		rt.reportError(req, deleteResource(conn, idResult.id))
		bodyBuffer.Close()
		rt.db.Put(conn)
		result.response.Body.Close()
		return nil, fmt.Errorf("cache response body: %v", err)
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
		return nil
	}()
	bodyBuffer.Close()
	if err != nil {
		rt.db.Put(conn)
		result.response.Body.Close()
		return nil, err
	}

	result.response.Body.Close()
	result.response.Body = rt.newStoredResponseBody(conn, idResult.id)
	return result.response, nil
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
	if !isFinalStatusCode(statusCode) {
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
	// TODO(soon): Understand the status code?
	canStore := isCacheableStatusCode(statusCode) || len(responseHeader["Expires"]) > 0
	for d := range cacheControlDirectives(responseHeader) {
		switch {
		case d.nameMatches("no-store") && d.rawArgument == "":
			return false
		case !canStore && (d.nameMatches("private") ||
			d.nameMatches("public") && d.rawArgument == "" ||
			d.nameMatches("max-age") && d.rawArgument != ""):
			canStore = true
		}
	}
	return canStore
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

//go:embed sql
var rawSQLFiles embed.FS

var sqlFiles = sync.OnceValue(func() fs.FS {
	fsys, err := fs.Sub(rawSQLFiles, "sql")
	if err != nil {
		panic(err)
	}
	return fsys
})

var schema = sync.OnceValue(func() sqlitemigration.Schema {
	var schema sqlitemigration.Schema
	for i := 1; ; i++ {
		migration, err := fs.ReadFile(sqlFiles(), fmt.Sprintf("schema/%02d.sql", i))
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			panic(fmt.Sprintf("read migrations: %v", err))
		}
		schema.Migrations = append(schema.Migrations, string(migration))
	}
	return schema
})

func prepareConn(conn *sqlite.Conn) error {
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA journal_mode=wal;", nil); err != nil {
		return fmt.Errorf("enable write-ahead logging: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA synchronous=normal;", nil); err != nil {
		return fmt.Errorf("enable write-ahead logging: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA foreign_keys=on;", nil); err != nil {
		return fmt.Errorf("enable foreign keys: %v", err)
	}

	err := conn.SetCollation("headerkey", func(a, b string) int {
		return cmp.Compare(http.CanonicalHeaderKey(a), http.CanonicalHeaderKey(b))
	})
	if err != nil {
		return err
	}

	err = conn.CreateFunction("httpdate", &sqlite.FunctionImpl{
		NArgs:         1,
		Deterministic: true,
		AllowIndirect: true,
		Scalar: func(ctx sqlite.Context, args []sqlite.Value) (sqlite.Value, error) {
			arg := args[0]
			if arg.Type() == sqlite.TypeNull {
				return sqlite.Value{}, nil
			}
			t, err := http.ParseTime(arg.Text())
			if err != nil {
				return sqlite.Value{}, nil
			}
			return sqlite.IntegerValue(t.Unix()), nil
		},
	})
	if err != nil {
		return err
	}

	return nil
}

var queryCache struct {
	mu    sync.RWMutex
	files map[string]string
}

func prepareQuery(conn *sqlite.Conn, name string) *sqlite.Stmt {
	queryCache.mu.RLock()
	query := queryCache.files[name]
	queryCache.mu.RUnlock()
	if query != "" {
		return conn.Prep(query)
	}

	query, err := readFileString(sqlFiles(), name)
	if err != nil {
		panic(err)
	}
	query = strings.TrimRight(query, "\n")

	queryCache.mu.Lock()
	if queryCache.files == nil {
		queryCache.files = make(map[string]string)
	}
	queryCache.files[name] = query
	queryCache.mu.Unlock()

	return conn.Prep(query)
}

func runStatement(stmt *sqlite.Stmt) error {
	hasRow, stepError := stmt.Step()
	resetError := stmt.Reset()
	if stepError != nil {
		return stepError
	}
	if hasRow {
		return errors.New("unexpected result row")
	}
	return resetError
}

func readFileString(fsys fs.FS, name string) (string, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sb := new(strings.Builder)
	_, err = io.Copy(sb, f)
	return sb.String(), err
}
