// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package fileurl

import (
	"bytes"
	"compress/gzip"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"zb.256lights.llc/pkg/internal/xhttp"
	"zb.256lights.llc/pkg/sets"
)

func TestTransport(t *testing.T) {
	client := &http.Client{
		Transport: new(Transport),
	}
	defer client.CloseIdleConnections()

	t.Run("Get", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "foo.txt")
		const content = "Hello, World!\n"
		if err := os.WriteFile(path, []byte(content), 0o666); err != nil {
			t.Fatal(err)
		}

		resp, err := client.Do(&http.Request{URL: FromPath(path)})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error("response.Body.Close():", err)
			}
		}()

		if got, want := resp.StatusCode, http.StatusOK; got != want {
			t.Errorf("response.StatusCode = %d; want %d", got, want)
		}
		contentType := resp.Header.Get("Content-Type")
		if got, _, err := mime.ParseMediaType(contentType); err != nil {
			t.Errorf("Content-Type = %q; parse error: %v", contentType, err)
		} else if want := "text/plain"; got != want {
			t.Errorf("Content-Type = %s; want text/plain", contentType)
		}
		if got, want := resp.ContentLength, int64(len(content)); got != want {
			t.Errorf("Content-Length = %d; want %d", got, want)
		}
		if got, err := io.ReadAll(resp.Body); string(got) != content || err != nil {
			t.Errorf("io.ReadAll(response.Body) = %q, %v; want %q, <nil>", got, err, content)
		}
	})

	t.Run("GetDirectory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "foo")
		if err := os.Mkdir(path, 0o777); err != nil {
			t.Fatal(err)
		}

		resp, err := client.Do(&http.Request{URL: FromPath(path)})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error("response.Body.Close():", err)
			}
		}()

		if got, want := resp.StatusCode, http.StatusNoContent; got != want {
			t.Errorf("response.StatusCode = %d; want %d", got, want)
		}
		if got, want := resp.Header.Get("Content-Type"), ""; got != want {
			t.Errorf("Content-Type = %q; want %q", got, want)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		resp, err := client.Do(&http.Request{
			URL: FromPath(filepath.Join(t.TempDir(), "foo.txt")),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error("response.Body.Close():", err)
			}
		}()

		if got, want := resp.StatusCode, http.StatusNotFound; got != want {
			t.Errorf("response.StatusCode = %d; want %d", got, want)
		}
	})

	t.Run("Head", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "foo.txt")
		const content = "Hello, World!\n"
		if err := os.WriteFile(path, []byte(content), 0o666); err != nil {
			t.Fatal(err)
		}

		resp, err := client.Do(&http.Request{
			Method: http.MethodHead,
			URL:    FromPath(path),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error("response.Body.Close():", err)
			}
		}()

		if got, want := resp.StatusCode, http.StatusOK; got != want {
			t.Errorf("response.StatusCode = %d; want %d", got, want)
		}
		contentType := resp.Header.Get("Content-Type")
		if got, _, err := mime.ParseMediaType(contentType); err != nil {
			t.Errorf("Content-Type = %q; parse error: %v", contentType, err)
		} else if want := "text/plain"; got != want {
			t.Errorf("Content-Type = %s; want text/plain", contentType)
		}
		if got, want := resp.ContentLength, int64(len(content)); got != want {
			t.Errorf("Content-Length = %d; want %d", got, want)
		}
	})

	t.Run("Put", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "foo.txt")
		const content = "Hello, World!\n"

		resp, err := client.Do(&http.Request{
			Method: http.MethodPut,
			URL:    FromPath(path),
			Header: http.Header{
				"Content-Length": {strconv.Itoa(len(content))},
				"Content-Type":   {"text/plain; charset=utf-8"},
			},
			Body: io.NopCloser(strings.NewReader(content)),
			GetBody: func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(content)), nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error("response.Body.Close():", err)
			}
		}()

		if got, want := resp.StatusCode, http.StatusNoContent; got != want {
			t.Errorf("response.StatusCode = %d; want %d", got, want)
		}
		if got, err := os.ReadFile(path); string(got) != content || err != nil {
			t.Errorf("os.ReadFile(%q) = %q, %v; want %q, <nil>", path, got, err, content)
		}
	})

	t.Run("PutGzip", func(t *testing.T) {
		const content = "Hello, World!\n"
		bodyBuffer := new(bytes.Buffer)
		zw := gzip.NewWriter(bodyBuffer)
		if _, err := zw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}

		path := filepath.Join(t.TempDir(), "foo.txt")
		resp, err := client.Do(&http.Request{
			Method: http.MethodPut,
			URL:    FromPath(path),
			Header: http.Header{
				"Content-Length":   {strconv.Itoa(len(content))},
				"Content-Type":     {"text/plain; charset=utf-8"},
				"Content-Encoding": {"gzip"},
			},
			Body: io.NopCloser(bytes.NewReader(bodyBuffer.Bytes())),
			GetBody: func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(bodyBuffer.Bytes())), nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error("response.Body.Close():", err)
			}
		}()

		if got, want := resp.StatusCode, http.StatusNoContent; got != want {
			t.Errorf("response.StatusCode = %d; want %d", got, want)
		}
		if got, err := os.ReadFile(path); string(got) != content || err != nil {
			t.Errorf("os.ReadFile(%q) = %q, %v; want %q, <nil>", path, got, err, content)
		}
	})

	t.Run("PutDirectory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "foo")
		if err := os.Mkdir(path, 0o777); err != nil {
			t.Fatal(err)
		}

		const content = "Hello, World!\n"
		resp, err := client.Do(&http.Request{
			Method: http.MethodPut,
			URL:    FromPath(path),
			Header: http.Header{
				"Content-Length": {strconv.Itoa(len(content))},
				"Content-Type":   {"text/plain; charset=utf-8"},
			},
			Body: io.NopCloser(strings.NewReader(content)),
			GetBody: func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(content)), nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error("response.Body.Close():", err)
			}
		}()

		if got, want := resp.StatusCode, http.StatusMethodNotAllowed; got != want {
			t.Errorf("response.StatusCode = %d; want %d", got, want)
		}
		gotAllow := make(sets.Set[string])
		for _, value := range resp.Header.Values("Allow") {
			gotAllow.AddSeq(xhttp.SplitList(value))
		}
		wantAllow := sets.New(http.MethodGet, http.MethodHead)
		if diff := cmp.Diff(wantAllow, gotAllow); diff != "" {
			t.Errorf("Allow (-want +got):\n%s", diff)
		}
	})
}
