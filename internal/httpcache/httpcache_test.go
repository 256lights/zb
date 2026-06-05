// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package httpcache

import (
	"bufio"
	"bytes"
	"cmp"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	googlecmp "github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestRoundTripper(t *testing.T) {
	initialTime := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	const plainMediaType = "text/plain; charset=utf-8"

	type cacheInteraction struct {
		testRequestResponse
		sleep time.Duration
	}

	tests := []struct {
		name           string
		cacheRequests  []*cacheInteraction
		serverRequests []*testRequestResponse
	}{
		{
			name: "SingleRequest",
			cacheRequests: []*cacheInteraction{
				{
					testRequestResponse: testRequestResponse{
						url: "http://www.example.com/foo",
						responseHeaders: http.Header{
							"Content-Type": {plainMediaType},
							"Date":         {initialTime.Format(http.TimeFormat)},
						},
						responseBody: "Hello, World!\n",
					},
				},
			},
			serverRequests: []*testRequestResponse{
				{
					url: "http://www.example.com/foo",
					requestHeaders: http.Header{
						"Host": {"www.example.com"},
					},
					responseHeaders: http.Header{
						"Content-Type": {plainMediaType},
					},
					responseBody: "Hello, World!\n",
				},
			},
		},
		{
			name: "HeuristicCache",
			cacheRequests: []*cacheInteraction{
				{
					testRequestResponse: testRequestResponse{
						url: "http://www.example.com/foo",
						responseHeaders: http.Header{
							"Content-Type": {plainMediaType},
							"Date":         {initialTime.Format(http.TimeFormat)},
						},
						responseBody: "Hello, World!\n",
					},
					sleep: 5 * time.Second,
				},
				{
					testRequestResponse: testRequestResponse{
						url: "http://www.example.com/foo",
						responseHeaders: http.Header{
							"Content-Type": {plainMediaType},
							"Date":         {initialTime.Format(http.TimeFormat)},
							"Age":          {"5"},
						},
						responseBody: "Hello, World!\n",
					},
				},
			},
			serverRequests: []*testRequestResponse{
				{
					url: "http://www.example.com/foo",
					requestHeaders: http.Header{
						"Host": {"www.example.com"},
					},
					responseHeaders: http.Header{
						"Content-Type": {plainMediaType},
						"Date":         {initialTime.Format(http.TimeFormat)},
					},
					responseBody: "Hello, World!\n",
				},
			},
		},
		{
			name: "ExplicitlyFresh",
			cacheRequests: []*cacheInteraction{
				{
					testRequestResponse: testRequestResponse{
						url: "http://www.example.com/foo",
						responseHeaders: http.Header{
							"Content-Type":  {plainMediaType},
							"Date":          {initialTime.Format(http.TimeFormat)},
							"Cache-Control": {"max-age=604800"},
						},
						responseBody: "Hello, World!\n",
					},
					sleep: 5 * time.Second,
				},
				{
					testRequestResponse: testRequestResponse{
						url: "http://www.example.com/foo",
						responseHeaders: http.Header{
							"Content-Type":  {plainMediaType},
							"Date":          {initialTime.Format(http.TimeFormat)},
							"Cache-Control": {"max-age=604800"},
							"Age":           {"5"},
						},
						responseBody: "Hello, World!\n",
					},
				},
			},
			serverRequests: []*testRequestResponse{
				{
					url: "http://www.example.com/foo",
					requestHeaders: http.Header{
						"Host": {"www.example.com"},
					},
					responseHeaders: http.Header{
						"Content-Type":  {plainMediaType},
						"Cache-Control": {"max-age=604800"},
						"Date":          {initialTime.Format(http.TimeFormat)},
					},
					responseBody: "Hello, World!\n",
				},
			},
		},
		{
			name: "ExplicitlyStale",
			cacheRequests: []*cacheInteraction{
				{
					testRequestResponse: testRequestResponse{
						url: "http://www.example.com/foo",
						responseHeaders: http.Header{
							"Content-Type":  {plainMediaType},
							"Date":          {initialTime.Format(http.TimeFormat)},
							"Cache-Control": {"max-age=0"},
						},
						responseBody: "Hello, World!\n",
					},
					sleep: 5 * time.Second,
				},
				{
					testRequestResponse: testRequestResponse{
						url: "http://www.example.com/foo",
						responseHeaders: http.Header{
							"Content-Type":  {plainMediaType},
							"Cache-Control": {"max-age=0"},
							"Date":          {initialTime.Add(5 * time.Second).Format(http.TimeFormat)},
						},
						responseBody: "Hello, World!\n",
					},
				},
			},
			serverRequests: []*testRequestResponse{
				{
					url: "http://www.example.com/foo",
					requestHeaders: http.Header{
						"Host": {"www.example.com"},
					},
					responseHeaders: http.Header{
						"Content-Type":  {plainMediaType},
						"Cache-Control": {"max-age=0"},
						"Date":          {initialTime.Format(http.TimeFormat)},
					},
					responseBody: "Hello, World!\n",
				},
				{
					url: "http://www.example.com/foo",
					requestHeaders: http.Header{
						"Host": {"www.example.com"},
					},
					responseHeaders: http.Header{
						"Content-Type":  {plainMediaType},
						"Cache-Control": {"max-age=0"},
						"Date":          {initialTime.Add(5 * time.Second).Format(http.TimeFormat)},
					},
					responseBody: "Hello, World!\n",
				},
			},
		},
		{
			name: "StripUpgrade",
			cacheRequests: []*cacheInteraction{
				{
					testRequestResponse: testRequestResponse{
						url: "http://www.example.com/foo",
						responseHeaders: http.Header{
							"Content-Type": {plainMediaType},
							"Date":         {initialTime.Format(http.TimeFormat)},
							"Upgrade":      {"foo"},
						},
						responseBody: "Hello, World!\n",
					},
					sleep: 5 * time.Second,
				},
				{
					testRequestResponse: testRequestResponse{
						url: "http://www.example.com/foo",
						responseHeaders: http.Header{
							"Content-Type": {plainMediaType},
							"Date":         {initialTime.Format(http.TimeFormat)},
							"Age":          {"5"},
						},
						responseBody: "Hello, World!\n",
					},
				},
			},
			serverRequests: []*testRequestResponse{
				{
					url: "http://www.example.com/foo",
					requestHeaders: http.Header{
						"Host": {"www.example.com"},
					},
					responseHeaders: http.Header{
						"Content-Type": {plainMediaType},
						"Date":         {initialTime.Format(http.TimeFormat)},
						"Upgrade":      {"foo"},
					},
					responseBody: "Hello, World!\n",
				},
			},
		},
		{
			name: "NoCacheHeaders",
			cacheRequests: []*cacheInteraction{
				{
					testRequestResponse: testRequestResponse{
						url: "http://www.example.com/foo",
						responseHeaders: http.Header{
							"Content-Type":  {plainMediaType},
							"Date":          {initialTime.Format(http.TimeFormat)},
							"Cache-Control": {`no-cache="X-Foo"`},
							"X-Foo":         {"42"},
							"X-Bar":         {"pi"},
						},
						responseBody: "Hello, World!\n",
					},
					sleep: 5 * time.Second,
				},
				{
					testRequestResponse: testRequestResponse{
						url: "http://www.example.com/foo",
						responseHeaders: http.Header{
							"Content-Type":  {plainMediaType},
							"Date":          {initialTime.Format(http.TimeFormat)},
							"Cache-Control": {`no-cache="X-Foo"`},
							"X-Bar":         {"pi"},
							"Age":           {"5"},
						},
						responseBody: "Hello, World!\n",
					},
				},
			},
			serverRequests: []*testRequestResponse{
				{
					url: "http://www.example.com/foo",
					requestHeaders: http.Header{
						"Host": {"www.example.com"},
					},
					responseHeaders: http.Header{
						"Content-Type":  {plainMediaType},
						"Date":          {initialTime.Format(http.TimeFormat)},
						"Cache-Control": {`no-cache="X-Foo"`},
						"X-Foo":         {"42"},
						"X-Bar":         {"pi"},
					},
					responseBody: "Hello, World!\n",
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				mockServer := &mockRoundTripper{
					tb:        t,
					responses: test.serverRequests,
				}
				cache := Open(filepath.Join(t.TempDir(), "http-cache.sqlite"), mockServer)
				defer func() {
					if err := cache.Close(); err != nil {
						t.Error("cache.Close():", err)
					}
				}()

				for _, req := range test.cacheRequests {
					method := cmp.Or(req.method, http.MethodGet)
					got, err := cache.RoundTrip(req.makeRequest(t))
					if err != nil {
						t.Errorf("RoundTrip(%s %s): %v", method, req.url, err)
					}

					if got == nil {
						t.Errorf("%s %s: response == <nil>", method, req.url)
					} else {
						if want := cmp.Or(req.statusCode, http.StatusOK); got.StatusCode != want {
							t.Errorf("%s %s: status code = %d; want %d", method, req.url, got.StatusCode, want)
						}
						if diff := googlecmp.Diff(req.responseHeaders, got.Header, cmpopts.EquateEmpty()); diff != "" {
							t.Errorf("%s %s: headers (-want +got):\n%s", method, req.url, diff)
						}
						if got.Body == nil {
							t.Errorf("%s %s: response.Body == <nil>", method, req.url)
						} else {
							gotBody, readError := io.ReadAll(got.Body)
							closeError := got.Body.Close()
							if readError != nil {
								t.Errorf("%s %s: reading body: %v", method, req.url, readError)
							}
							if diff := googlecmp.Diff(req.responseBody, string(gotBody)); diff != "" {
								t.Errorf("%s %s: body (-want +got):\n%s", method, req.url, diff)
							}
							if closeError != nil {
								t.Errorf("%s %s: closing body: %v", method, req.url, closeError)
							}
						}
					}

					if req.sleep > 0 {
						time.Sleep(req.sleep)
					}
				}
			})
		})
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

func (trr *testRequestResponse) makeRequest(tb testing.TB) *http.Request {
	tb.Helper()
	u, err := url.Parse(trr.url)
	if err != nil {
		tb.Fatal(err)
	}
	return &http.Request{
		Method: cmp.Or(trr.method, http.MethodGet),
		URL:    u,
		Header: trr.requestHeaders.Clone(),
	}
}

type mockRoundTripper struct {
	tb testing.TB

	mu        sync.Mutex
	responses []*testRequestResponse
}

func (rt *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	gotMethod := cmp.Or(req.Method, http.MethodGet)
	want, err := rt.match(req)
	if err != nil {
		rt.tb.Error(err)
		return nil, err
	}

	if gotHeaders, err := effectiveRequestHeaders(req); err != nil {
		rt.tb.Errorf("Comparing request headers for %s %v: %v", gotMethod, req.URL, err)
	} else {
		diff := googlecmp.Diff(
			want.requestHeaders, gotHeaders,
			ignoreHeaders("User-Agent"),
			cmpopts.EquateEmpty(),
		)
		if diff != "" {
			rt.tb.Errorf("%s %v request headers (-want +got):\n%s", gotMethod, req.URL, diff)
		}
	}

	if date, ok := dateHeader(want.responseHeaders, "Date"); ok {
		if dt := time.Until(date); dt > 0 {
			t := time.NewTimer(dt)
			select {
			case <-t.C:
			case <-req.Context().Done():
				t.Stop()
				return nil, fmt.Errorf("%s %v: %w", gotMethod, req.URL, err)
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
	return resp, nil
}

func (rt *mockRoundTripper) match(req *http.Request) (*testRequestResponse, error) {
	gotMethod := cmp.Or(req.Method, http.MethodGet)

	rt.mu.Lock()
	defer rt.mu.Unlock()

	if len(rt.responses) == 0 {
		return nil, fmt.Errorf("mock round tripper received unexpected request for %s %v", gotMethod, req.URL)
	}
	next := rt.responses[0]
	wantMethod := cmp.Or(next.method, http.MethodGet)
	if wantMethod != gotMethod || req.URL.String() != next.url {
		return nil, fmt.Errorf("mock round tripper received request for %s %v (want %s %v)",
			gotMethod, req.URL, wantMethod, next.url)
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
