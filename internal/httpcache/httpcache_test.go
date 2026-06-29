// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package httpcache

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	googlecmp "github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"golang.org/x/tools/txtar"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

func TestRoundTripper(t *testing.T) {
	listing, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range listing {
		fileName := entry.Name()
		if strings.HasPrefix(fileName, ".") {
			continue
		}
		testName, isTXTAR := strings.CutSuffix(fileName, ".txtar")
		if !isTXTAR {
			continue
		}

		t.Run(testName, func(t *testing.T) {
			ar, err := txtar.ParseFile(filepath.Join("testdata", fileName))
			if err != nil {
				t.Fatal(err)
			}
			cacheFileData, ok := txtarFileData(ar, "cache.txt")
			if !ok {
				t.Fatalf("Missing cache.txt in testdata/%s", fileName)
			}
			cacheRequests, err := readRequestResponses(bufio.NewReader(bytes.NewReader(cacheFileData)))
			if err != nil {
				t.Fatal("cache.txt:", err)
			}
			serverFileData, _ := txtarFileData(ar, "server.txt")
			serverRequests, err := readRequestResponses(bufio.NewReader(bytes.NewReader(serverFileData)))
			if err != nil {
				t.Fatal("server.txt:", err)
			}

			synctest.Test(t, func(t *testing.T) {
				mockServer := &mockRoundTripper{
					tb:        t,
					responses: serverRequests,
				}
				cache := Open(filepath.Join(t.TempDir(), "http-cache.sqlite"), mockServer, &Options{
					ErrorLogger: ErrorReporterFunc(func(ctx context.Context, info *RequestInfo, err error) {
						if info != nil {
							t.Errorf("Cache error on %s %v: %v", info.Method, info.URL, err)
						} else {
							t.Error("Cache error:", err)
						}
					}),
				})
				t.Cleanup(func() {
					if err := cache.Close(); err != nil {
						t.Error("cache.Close():", err)
					}
				})

				for _, req := range cacheRequests {
					requestDate, ok := dateHeader(req.requestHeaders, "Date")
					if ok {
						if d := time.Until(requestDate); d > 0 {
							time.Sleep(d)
						}
					} else {
						requestDate = time.Now()
					}

					method := cmp.Or(req.method, http.MethodGet)
					got, err := cache.RoundTrip(req.makeRequest(t))
					if err != nil {
						t.Errorf("RoundTrip(%s %s @ %s): %v", method, req.url, requestDate.UTC().Format(http.TimeFormat), err)
					}

					if got == nil {
						t.Errorf("%s %s @ %s: response == <nil>", method, req.url, requestDate.UTC().Format(http.TimeFormat))
					} else {
						if want := cmp.Or(req.statusCode, http.StatusOK); got.StatusCode != want {
							t.Errorf("%s %s @ %s: status code = %d; want %d", method, req.url, requestDate.UTC().Format(http.TimeFormat), got.StatusCode, want)
						}
						if diff := googlecmp.Diff(req.responseHeaders, got.Header, cmpopts.EquateEmpty()); diff != "" {
							t.Errorf("%s %s @ %s: headers (-want +got):\n%s", method, req.url, requestDate.UTC().Format(http.TimeFormat), diff)
						}
						if got.Body == nil {
							t.Errorf("%s %s @ %s: response.Body == <nil>", method, req.url, requestDate.UTC().Format(http.TimeFormat))
						} else {
							gotBody, readError := io.ReadAll(got.Body)
							closeError := got.Body.Close()
							if readError != nil {
								t.Errorf("%s %s @ %s: reading body: %v", method, req.url, requestDate.UTC().Format(http.TimeFormat), readError)
							}
							if diff := googlecmp.Diff(req.responseBody, string(gotBody)); diff != "" {
								t.Errorf("%s %s @ %s: body (-want +got):\n%s", method, req.url, requestDate.UTC().Format(http.TimeFormat), diff)
							}
							if closeError != nil {
								t.Errorf("%s %s @ %s: closing body: %v", method, req.url, requestDate.UTC().Format(http.TimeFormat), closeError)
							}
						}
					}
				}

				mockServer.mu.Lock()
				remainingResponses := slices.Clone(mockServer.responses)
				mockServer.mu.Unlock()
				for _, resp := range remainingResponses {
					t.Errorf("Server did not receive request for %s %s %v",
						cmp.Or(resp.method, http.MethodGet), resp.url, resp.requestHeaders)
				}
			})
		})
	}
}

// TestRoundTripperVaryAuthorization verifies that even if a server responds with "Vary: Authorization"
// that a [RoundTripper] doesn't store the request header.
func TestRoundTripperVaryAuthorization(t *testing.T) {
	testTime := time.Now().UTC()
	mockServer := &mockRoundTripper{
		tb: t,
		responses: []*testRequestResponse{
			{
				url: "http://www.example.com/foo",
				requestHeaders: http.Header{
					"Host":          {"www.example.com"},
					"Authorization": {"Bearer xyzzy"},
				},
				responseHeaders: http.Header{
					"Content-Length": {"13"},
					"Content-Type":   {"text/plain; charset=utf-8"},
					"Vary":           {"Authorization"},
					"Date":           {testTime.Format(http.TimeFormat)},
					"Last-Modified":  {testTime.Format(http.TimeFormat)},
				},
				responseBody: "Hello, User!\n",
			},
		},
	}

	dbPath := filepath.Join(t.TempDir(), "http-cache.sqlite")
	cache := Open(dbPath, mockServer, &Options{
		ErrorLogger: ErrorReporterFunc(func(ctx context.Context, info *RequestInfo, err error) {
			if info != nil {
				t.Errorf("Cache error on %s %v: %v", info.Method, info.URL, err)
			} else {
				t.Error("Cache error:", err)
			}
		}),
	})
	resp, err := cache.RoundTrip(&http.Request{
		URL: &url.URL{
			Scheme: "http",
			Host:   "www.example.com",
			Path:   "/foo",
		},
		Header: http.Header{
			"Authorization": {"Bearer xyzzy"},
		},
	})
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Error("RoundTrip:", err)
	}
	if err := cache.Close(); err != nil {
		t.Error("cache.Close():", err)
	}

	conn, err := sqlite.OpenConn(dbPath, sqlite.OpenReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Error("conn.Close:", err)
		}
	}()
	if err := prepareConn(conn); err != nil {
		t.Error(err)
	}
	const query = `select "name", "value" from "headers" where "value" like '%xyzzy%';`
	err = sqlitex.ExecuteTransient(conn, query, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			name := stmt.ColumnText(0)
			value := stmt.ColumnText(1)
			t.Errorf("%s: %s found in headers table", name, value)
			return nil
		},
	})
	if err != nil {
		t.Error(err)
	}
}

type testRequestResponse struct {
	method         string
	url            string
	requestHeaders http.Header

	statusCode      int
	responseHeaders http.Header
	responseBody    string
}

func readRequestResponses(r *bufio.Reader) ([]*testRequestResponse, error) {
	var result []*testRequestResponse
	reqReader := textproto.NewReader(r)
	for {
		req, err := readRequest(reqReader)
		if err == io.EOF {
			return result, nil
		}
		if err != nil {
			return nil, err
		}
		req.Body.Close()
		resp, err := http.ReadResponse(r, req)
		if err != nil {
			return nil, err
		}
		body := new(strings.Builder)
		_, err = io.Copy(body, resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		result = append(result, &testRequestResponse{
			method:          req.Method,
			url:             req.URL.String(),
			requestHeaders:  req.Header,
			statusCode:      resp.StatusCode,
			responseHeaders: resp.Header,
			responseBody:    body.String(),
		})
	}
}

func (trr *testRequestResponse) makeRequest(tb testing.TB) *http.Request {
	tb.Helper()
	u, err := url.Parse(trr.url)
	if err != nil {
		tb.Fatal(err)
	}
	return (&http.Request{
		Method: cmp.Or(trr.method, http.MethodGet),
		URL:    u,
		Header: trr.requestHeaders.Clone(),
	}).WithContext(tb.Context())
}

func readRequest(r *textproto.Reader) (*http.Request, error) {
	if buf, err := r.R.Peek(256); err == io.EOF && len(bytes.TrimLeft(buf, "\n\r")) == 0 {
		r.R.Discard(len(buf))
		return nil, io.EOF
	}

	first, err := r.ReadLine()
	if err != nil {
		if err != io.EOF {
			err = fmt.Errorf("read http request: %w", err)
		}
		return nil, err
	}
	req := new(http.Request)
	var ok1, ok2 bool
	var rest string
	req.Method, rest, ok1 = strings.Cut(first, " ")
	req.RequestURI, req.Proto, ok2 = strings.Cut(rest, " ")
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("read http request: malformed HTTP request")
	}
	var ok bool
	if req.ProtoMajor, req.ProtoMinor, ok = http.ParseHTTPVersion(req.Proto); !ok {
		return nil, fmt.Errorf("read http request: malformed HTTP version %q", req.Proto)
	}
	if req.URL, err = url.ParseRequestURI(req.RequestURI); err != nil {
		return nil, fmt.Errorf("read http request: %v", err)
	}
	mimeHeader, err := r.ReadMIMEHeader()
	if err != nil {
		return nil, fmt.Errorf("read http request: %v", err)
	}
	req.Header = http.Header(mimeHeader)
	req.Host = cmp.Or(req.URL.Host, req.Header.Get("Host"))
	if contentLength := req.Header.Get("Content-Length"); contentLength == "" {
		req.ContentLength = -1
	} else if n, err := strconv.ParseUint(contentLength, 10, 63); err != nil {
		return nil, fmt.Errorf("read http request: Content-Length: %v", err)
	} else {
		req.ContentLength = int64(n)
	}
	switch {
	case req.Method == http.MethodHead:
		req.Body = http.NoBody
	case req.ContentLength <= 0:
		req.Body = http.NoBody
		req.ContentLength = 0
	default:
		req.Body = &limitedReader{R: r.R, N: req.ContentLength}
	}
	return req, nil
}

type limitedReader struct {
	R io.Reader
	N int64
}

func (lr *limitedReader) Read(p []byte) (n int, err error) {
	if lr.N <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > lr.N {
		p = p[0:lr.N]
	}
	n, err = lr.R.Read(p)
	lr.N -= int64(n)
	return
}

func (lr *limitedReader) Close() error {
	var err error
	if lr.N > 0 {
		_, err = io.Copy(io.Discard, lr)
	}
	return err
}

type mockRoundTripper struct {
	tb testing.TB

	mu        sync.Mutex
	responses []*testRequestResponse
}

func (rt *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	requestDate, ok := dateHeader(req.Header, "Date")
	if !ok {
		requestDate = time.Now()
	}

	gotMethod := cmp.Or(req.Method, http.MethodGet)
	want, err := rt.match(req, requestDate)
	if err != nil {
		rt.tb.Error(err)
		return nil, err
	}

	if gotHeaders, err := effectiveRequestHeaders(req); err != nil {
		rt.tb.Errorf("Comparing request headers for %s %v @ %s: %v", gotMethod, req.URL, requestDate.UTC().Format(http.TimeFormat), err)
	} else {
		diff := googlecmp.Diff(
			want.requestHeaders, gotHeaders,
			ignoreHeaders("User-Agent"),
			cmpopts.EquateEmpty(),
		)
		if diff != "" {
			rt.tb.Errorf("%s %v @ %s request headers (-want +got):\n%s", gotMethod, req.URL, requestDate.UTC().Format(http.TimeFormat), diff)
		}
	}

	if date, ok := dateHeader(want.responseHeaders, "Date"); ok {
		if dt := time.Until(date); dt > 0 {
			t := time.NewTimer(dt)
			select {
			case <-t.C:
			case <-req.Context().Done():
				t.Stop()
				return nil, fmt.Errorf("%s %v @ %s: %w", gotMethod, req.URL, requestDate.UTC().Format(http.TimeFormat), err)
			}
		}
	}

	resp := &http.Response{
		StatusCode: cmp.Or(want.statusCode, http.StatusOK),
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     want.responseHeaders.Clone(),
		Body:       io.NopCloser(strings.NewReader(want.responseBody)),
	}
	resp.Status = http.StatusText(resp.StatusCode)
	if gotMethod == http.MethodHead || resp.StatusCode != http.StatusNotModified {
		resp.ContentLength, _ = contentLength(want.responseHeaders)
	}
	return resp, nil
}

func (rt *mockRoundTripper) match(req *http.Request, requestDate time.Time) (*testRequestResponse, error) {
	gotMethod := cmp.Or(req.Method, http.MethodGet)

	rt.mu.Lock()
	defer rt.mu.Unlock()

	if len(rt.responses) == 0 {
		return nil, fmt.Errorf("mock round tripper received unexpected request for %s %v @ %s", gotMethod, req.URL, requestDate.UTC().Format(http.TimeFormat))
	}
	next := rt.responses[0]
	wantMethod := cmp.Or(next.method, http.MethodGet)
	if wantMethod != gotMethod || req.URL.String() != next.url {
		return nil, fmt.Errorf("mock round tripper received request for %s %v @ %s (want %s %v)",
			gotMethod, req.URL, requestDate.UTC().Format(http.TimeFormat), wantMethod, next.url)
	}
	rt.responses[0] = nil
	rt.responses = rt.responses[1:]
	return next, nil
}

func ignoreHeaders(names ...string) googlecmp.Option {
	nameSet := make(map[string]struct{})
	for _, name := range names {
		nameSet[http.CanonicalHeaderKey(name)] = struct{}{}
	}

	return googlecmp.FilterPath(func(p googlecmp.Path) bool {
		if p.Index(-2).Type() != reflect.TypeFor[http.Header]() {
			return false
		}
		key := p.Last().(googlecmp.MapIndex).Key().String()
		_, found := nameSet[key]
		return found
	}, googlecmp.Ignore())
}

func effectiveRequestHeaders(req *http.Request) (http.Header, error) {
	req2 := new(*req)
	req2.Body = nil
	buf := new(bytes.Buffer)
	if err := req2.Write(buf); err != nil {
		return nil, err
	}
	r := textproto.NewReader(bufio.NewReader(buf))
	if _, err := r.ReadLineBytes(); err != nil {
		return nil, err
	}
	header, err := r.ReadMIMEHeader()
	if err != nil {
		return nil, err
	}
	return http.Header(header), nil
}

func BenchmarkRoundTripper(b *testing.B) {
	const content = "Hello, World!\n"
	const entityTag = `"hello"`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("ETag", entityTag)

		for _, value := range r.Header.Values("If-None-Match") {
			for elem := range splitList(value) {
				if elem == entityTag {
					w.WriteHeader(http.StatusNotModified)
					return
				}
			}
		}

		io.WriteString(w, content)
	}))
	b.Cleanup(srv.Close)

	cache := Open(filepath.Join(b.TempDir(), "http-cache.sqlite"), srv.Client().Transport, nil)
	defer func() {
		if err := cache.Close(); err != nil {
			b.Error("cache.Close:", err)
		}
	}()
	client := &http.Client{Transport: cache}

	// Warm cache.
	resp, err := client.Get(srv.URL)
	if err != nil {
		b.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		b.Errorf("status code = %d; want %d", resp.StatusCode, http.StatusOK)
	}
	if gotBody, err := io.ReadAll(resp.Body); err != nil {
		b.Error("Read body:", err)
	} else if string(gotBody) != content {
		b.Errorf("body = %q; want %q", gotBody, content)
	}
	resp.Body.Close()

	for b.Loop() {
		resp, err := client.Get(srv.URL)
		if err != nil {
			b.Error(err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			b.Errorf("status code = %d; want %d", resp.StatusCode, http.StatusOK)
		}
		if resp.Header.Get("Age") == "" {
			b.Error("missing Age header")
		}
		if gotBody, err := io.ReadAll(resp.Body); err != nil {
			b.Error("Read body:", err)
		} else if string(gotBody) != content {
			b.Errorf("body = %q; want %q", gotBody, content)
		}
		resp.Body.Close()
	}
}

func txtarFileData(ar *txtar.Archive, name string) (data []byte, ok bool) {
	for _, file := range ar.Files {
		if file.Name == name {
			return file.Data, true
		}
	}
	return nil, false
}
