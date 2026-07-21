// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package althttp

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fsouza/fake-gcs-server/fakestorage"
	"zb.256lights.llc/pkg/internal/xhttp"
)

func TestGCSTransport(t *testing.T) {
	const bucketName = "foo"
	const preloadedObjectName = "bar"
	const contentType = "text/plain; charset=utf-8"
	const etag xhttp.EntityTag = `"xyzzy"`
	const cacheControl = `max-age=604800`
	const content = "Hello, World!\n"
	objectCreated := time.Date(2026, time.July, 1, 9, 0, 0, 0, time.UTC)

	gcsServer := fakestorage.NewServer([]fakestorage.Object{
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName:   bucketName,
				Name:         preloadedObjectName,
				ContentType:  contentType,
				Size:         int64(len(content)),
				Etag:         string(etag[1 : len(etag)-1]),
				Created:      objectCreated,
				Updated:      objectCreated,
				CacheControl: cacheControl,
			},
			Content: []byte(content),
		},
	})
	t.Cleanup(gcsServer.Stop)

	client := &http.Client{
		Transport: &GCSTransport{
			Client: gcsServer.Client(),
		},
	}

	t.Run("Get/NoPreconditions", func(t *testing.T) {
		resp, err := client.Do(&http.Request{
			URL: &url.URL{
				Scheme: GCSScheme,
				Host:   bucketName,
				Path:   "/" + preloadedObjectName,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error("close response body:", err)
			}
		}()

		if got, want := resp.StatusCode, http.StatusOK; got != want {
			t.Errorf("status code = %d (%s); want %d (%s)",
				got, http.StatusText(got), want, http.StatusText(want))
		}
		if got, want := resp.Header.Get("Content-Type"), contentType; got != want {
			t.Errorf("Content-Type = %s; want %s", got, want)
		}
		if got, want := resp.Header.Get("ETag"), string(etag); got != want {
			t.Errorf("ETag = %s; want %s", got, want)
		}
		if got, want := resp.Header.Get("Last-Modified"), "Wed, 01 Jul 2026 09:00:00 GMT"; got != want {
			t.Errorf("Last-Modified = %s; want %s", got, want)
		}
		if got, want := resp.Header.Get("Cache-Control"), cacheControl; got != want {
			t.Errorf("Cache-Control = %s; want %s", got, want)
		}

		gotBody, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Error("io.ReadAll(response.Body):", err)
		}
		if string(gotBody) != content {
			t.Errorf("body = %q; want %q", gotBody, content)
		}
	})

	t.Run("Get/NotFound", func(t *testing.T) {
		resp, err := client.Do(&http.Request{
			URL: &url.URL{
				Scheme: GCSScheme,
				Host:   bucketName,
				Path:   "/notfound",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error("close response body:", err)
			}
		}()

		if got, want := resp.StatusCode, http.StatusNotFound; got != want {
			t.Errorf("status code = %d (%s); want %d (%s)",
				got, http.StatusText(got), want, http.StatusText(want))
		}
	})

	t.Run("Get/IfModifiedSince", func(t *testing.T) {
		resp, err := client.Do(&http.Request{
			URL: &url.URL{
				Scheme: GCSScheme,
				Host:   bucketName,
				Path:   "/" + preloadedObjectName,
			},
			Header: http.Header{
				"If-Modified-Since": {"Wed, 01 Jul 2026 09:00:00 GMT"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error("close response body:", err)
			}
		}()

		if got, want := resp.StatusCode, http.StatusNotModified; got != want {
			t.Errorf("status code = %d (%s); want %d (%s)",
				got, http.StatusText(got), want, http.StatusText(want))
		}
		if got, want := resp.Header.Get("Content-Type"), contentType; got != want {
			t.Errorf("Content-Type = %s; want %s", got, want)
		}
		if got, want := resp.Header.Get("ETag"), string(etag); got != want {
			t.Errorf("ETag = %s; want %s", got, want)
		}
		if got, want := resp.Header.Get("Last-Modified"), "Wed, 01 Jul 2026 09:00:00 GMT"; got != want {
			t.Errorf("Last-Modified = %s; want %s", got, want)
		}
		if got, want := resp.Header.Get("Cache-Control"), cacheControl; got != want {
			t.Errorf("Cache-Control = %s; want %s", got, want)
		}

		gotBody, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Error("io.ReadAll(response.Body):", err)
		}
		if string(gotBody) != "" {
			t.Errorf("body = %q; want \"\"", gotBody)
		}
	})

	t.Run("Head/NoPreconditions", func(t *testing.T) {
		resp, err := client.Do(&http.Request{
			Method: http.MethodHead,
			URL: &url.URL{
				Scheme: GCSScheme,
				Host:   bucketName,
				Path:   "/" + preloadedObjectName,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error("close response body:", err)
			}
		}()

		if got, want := resp.StatusCode, http.StatusOK; got != want {
			t.Errorf("status code = %d (%s); want %d (%s)",
				got, http.StatusText(got), want, http.StatusText(want))
		}
		if got, want := resp.Header.Get("Content-Type"), contentType; got != want {
			t.Errorf("Content-Type = %s; want %s", got, want)
		}
		if got, want := resp.Header.Get("ETag"), string(etag); got != want {
			t.Errorf("ETag = %s; want %s", got, want)
		}
		if got, want := resp.Header.Get("Last-Modified"), "Wed, 01 Jul 2026 09:00:00 GMT"; got != want {
			t.Errorf("Last-Modified = %s; want %s", got, want)
		}
		if got, want := resp.Header.Get("Cache-Control"), cacheControl; got != want {
			t.Errorf("Cache-Control = %s; want %s", got, want)
		}

		gotBody, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Error("io.ReadAll(response.Body):", err)
		}
		if string(gotBody) != "" {
			t.Errorf("body = %q; want \"\"", gotBody)
		}
	})

	t.Run("Head/NotFound", func(t *testing.T) {
		resp, err := client.Do(&http.Request{
			Method: http.MethodHead,
			URL: &url.URL{
				Scheme: GCSScheme,
				Host:   bucketName,
				Path:   "/notfound",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error("close response body:", err)
			}
		}()

		if got, want := resp.StatusCode, http.StatusNotFound; got != want {
			t.Errorf("status code = %d (%s); want %d (%s)",
				got, http.StatusText(got), want, http.StatusText(want))
		}
	})

	t.Run("Put/EmptyContentType", func(t *testing.T) {
		const objectName = "put-emptycontenttype"
		resp, err := client.Do(&http.Request{
			Method: http.MethodPut,
			URL: &url.URL{
				Scheme: GCSScheme,
				Host:   bucketName,
				Path:   "/" + objectName,
			},
			Header: http.Header{
				"Content-Length": {strconv.Itoa(len(content))},
			},
			Body: io.NopCloser(strings.NewReader(content)),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error("close response body:", err)
			}
		}()

		if got, want := resp.StatusCode, http.StatusUnsupportedMediaType; got != want {
			t.Errorf("status code = %d (%s); want %d (%s)",
				got, http.StatusText(got), want, http.StatusText(want))
		}

		if _, err := gcsServer.GetObject(bucketName, objectName); err == nil {
			t.Errorf("Created %s/%s", bucketName, objectName)
		} else {
			t.Log(err, "(expected)")
		}
	})

	t.Run("Put/Created", func(t *testing.T) {
		const objectName = "put-created"
		resp, err := client.Do(&http.Request{
			Method: http.MethodPut,
			URL: &url.URL{
				Scheme: GCSScheme,
				Host:   bucketName,
				Path:   "/" + objectName,
			},
			Header: http.Header{
				"Content-Type":   {contentType},
				"Content-Length": {strconv.Itoa(len(content))},
			},
			Body: io.NopCloser(strings.NewReader(content)),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error("close response body:", err)
			}
		}()

		if got, want := resp.StatusCode, http.StatusCreated; got != want {
			t.Errorf("status code = %d (%s); want %d (%s)",
				got, http.StatusText(got), want, http.StatusText(want))
		}
		if got := resp.Header.Get("ETag"); got == "" {
			t.Error("empty ETag")
		}
		if got := resp.Header.Get("Last-Modified"); got == "" {
			t.Error("empty Last-Modified")
		}

		obj, err := gcsServer.GetObject(bucketName, objectName)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := string(obj.Content), content; got != want {
			t.Errorf("content = %q; want %q", got, want)
		}
		if got, want := obj.ContentType, contentType; got != want {
			t.Errorf("Content-Type = %q; want %q", got, want)
		}
	})

	t.Run("Put/Updated", func(t *testing.T) {
		const objectName = "put-updated"

		gcsServer.CreateObject(fakestorage.Object{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName:  bucketName,
				Name:        objectName,
				ContentType: contentType,
				Created:     objectCreated,
				Updated:     objectCreated,
			},
			Content: []byte("omg what is this content?!\n"),
		})

		resp, err := client.Do(&http.Request{
			Method: http.MethodPut,
			URL: &url.URL{
				Scheme: GCSScheme,
				Host:   bucketName,
				Path:   "/" + objectName,
			},
			Header: http.Header{
				"Content-Type":   {contentType},
				"Content-Length": {strconv.Itoa(len(content))},
			},
			Body: io.NopCloser(strings.NewReader(content)),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error("close response body:", err)
			}
		}()

		if got, want := resp.StatusCode, http.StatusNoContent; got != want {
			t.Errorf("status code = %d (%s); want %d (%s)",
				got, http.StatusText(got), want, http.StatusText(want))
		}
		if got := resp.Header.Get("ETag"); got == "" {
			t.Error("empty ETag")
		}
		if got := resp.Header.Get("Last-Modified"); got == "" {
			t.Error("empty Last-Modified")
		}

		obj, err := gcsServer.GetObject(bucketName, objectName)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := string(obj.Content), content; got != want {
			t.Errorf("content = %q; want %q", got, want)
		}
		if got, want := obj.ContentType, contentType; got != want {
			t.Errorf("Content-Type = %q; want %q", got, want)
		}
	})

	t.Run("Put/IfNoneMatchStar/NotExist", func(t *testing.T) {
		const objectName = "put-ifnonematchstar-notexist"
		resp, err := client.Do(&http.Request{
			Method: http.MethodPut,
			URL: &url.URL{
				Scheme: GCSScheme,
				Host:   bucketName,
				Path:   "/" + objectName,
			},
			Header: http.Header{
				"Content-Type":   {contentType},
				"Content-Length": {strconv.Itoa(len(content))},
				"If-None-Match":  {"*"},
			},
			Body: io.NopCloser(strings.NewReader(content)),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error("close response body:", err)
			}
		}()

		if got, want := resp.StatusCode, http.StatusCreated; got != want {
			t.Errorf("status code = %d (%s); want %d (%s)",
				got, http.StatusText(got), want, http.StatusText(want))
		}
		if got := resp.Header.Get("ETag"); got == "" {
			t.Error("empty ETag")
		}
		if got := resp.Header.Get("Last-Modified"); got == "" {
			t.Error("empty Last-Modified")
		}

		obj, err := gcsServer.GetObject(bucketName, objectName)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := string(obj.Content), content; got != want {
			t.Errorf("content = %q; want %q", got, want)
		}
		if got, want := obj.ContentType, contentType; got != want {
			t.Errorf("Content-Type = %q; want %q", got, want)
		}
	})

	t.Run("Put/IfNoneMatchStar/Exist", func(t *testing.T) {
		const objectName = "put-ifnonematchstar-exist"

		gcsServer.CreateObject(fakestorage.Object{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName:  bucketName,
				Name:        objectName,
				ContentType: contentType,
				Created:     objectCreated,
				Updated:     objectCreated,
			},
			Content: []byte(content),
		})

		const overwriteContent = "BORK BORK BORK\n"
		resp, err := client.Do(&http.Request{
			Method: http.MethodPut,
			URL: &url.URL{
				Scheme: GCSScheme,
				Host:   bucketName,
				Path:   "/" + objectName,
			},
			Header: http.Header{
				"Content-Type":   {contentType},
				"Content-Length": {strconv.Itoa(len(overwriteContent))},
				"If-None-Match":  {"*"},
			},
			Body: io.NopCloser(strings.NewReader(overwriteContent)),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error("close response body:", err)
			}
		}()

		if got, want := resp.StatusCode, http.StatusPreconditionFailed; got != want {
			t.Errorf("status code = %d (%s); want %d (%s)",
				got, http.StatusText(got), want, http.StatusText(want))
		}

		obj, err := gcsServer.GetObject(bucketName, objectName)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := string(obj.Content), content; got != want {
			t.Errorf("content = %q; want %q", got, want)
		}
		if got, want := obj.ContentType, contentType; got != want {
			t.Errorf("Content-Type = %q; want %q", got, want)
		}
	})

	t.Run("Put/IfMatch/Match", func(t *testing.T) {
		const objectName = "put-ifmatch-match"

		gcsServer.CreateObject(fakestorage.Object{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName:  bucketName,
				Name:        objectName,
				ContentType: contentType,
				Created:     objectCreated,
				Updated:     objectCreated,
			},
			Content: []byte("It was the most initial of times...\n"),
		})
		obj, err := gcsServer.GetObject(bucketName, objectName)
		if err != nil {
			t.Fatal(err)
		}
		etagOpaque := obj.Etag
		if etagOpaque == "" {
			t.Error("fake did not assign an entity tag")
		}
		etag, err := xhttp.StrongEntityTag(etagOpaque)
		if err != nil {
			t.Fatal(err)
		}

		resp, err := client.Do(&http.Request{
			Method: http.MethodPut,
			URL: &url.URL{
				Scheme: GCSScheme,
				Host:   bucketName,
				Path:   "/" + objectName,
			},
			Header: http.Header{
				"Content-Type":   {contentType},
				"Content-Length": {strconv.Itoa(len(content))},
				"If-Match":       {string(etag)},
			},
			Body: io.NopCloser(strings.NewReader(content)),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error("close response body:", err)
			}
		}()

		if got, want := resp.StatusCode, http.StatusNoContent; got != want {
			t.Errorf("status code = %d (%s); want %d (%s)",
				got, http.StatusText(got), want, http.StatusText(want))
		}

		obj, err = gcsServer.GetObject(bucketName, objectName)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := string(obj.Content), content; got != want {
			t.Errorf("content = %q; want %q", got, want)
		}
		if got, want := obj.ContentType, contentType; got != want {
			t.Errorf("Content-Type = %q; want %q", got, want)
		}
	})

	t.Run("Put/IfMatch/NotMatch", func(t *testing.T) {
		const objectName = "put-ifmatch-notmatch"

		gcsServer.CreateObject(fakestorage.Object{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName:  bucketName,
				Name:        objectName,
				ContentType: contentType,
				Created:     objectCreated,
				Updated:     objectCreated,
			},
			Content: []byte("It was the most initial of times...\n"),
		})
		obj, err := gcsServer.GetObject(bucketName, objectName)
		if err != nil {
			t.Fatal(err)
		}
		etagOpaque := obj.Etag
		if etagOpaque == "" {
			t.Error("fake did not assign an entity tag")
		}
		etag, err := xhttp.StrongEntityTag(etagOpaque)
		if err != nil {
			t.Fatal(err)
		}

		const modifiedContent = "OBJECTION!\n"
		gcsServer.CreateObject(fakestorage.Object{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName:  bucketName,
				Name:        objectName,
				ContentType: contentType,
				Created:     objectCreated,
				Updated:     objectCreated.Add(10 * time.Second),
			},
			Content: []byte(modifiedContent),
		})

		resp, err := client.Do(&http.Request{
			Method: http.MethodPut,
			URL: &url.URL{
				Scheme: GCSScheme,
				Host:   bucketName,
				Path:   "/" + objectName,
			},
			Header: http.Header{
				"Content-Type":   {contentType},
				"Content-Length": {strconv.Itoa(len(content))},
				"If-Match":       {string(etag)},
			},
			Body: io.NopCloser(strings.NewReader(content)),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error("close response body:", err)
			}
		}()

		if got, want := resp.StatusCode, http.StatusPreconditionFailed; got != want {
			t.Errorf("status code = %d (%s); want %d (%s)",
				got, http.StatusText(got), want, http.StatusText(want))
		}

		obj, err = gcsServer.GetObject(bucketName, objectName)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := string(obj.Content), modifiedContent; got != want {
			t.Errorf("content = %q; want %q", got, want)
		}
		if got, want := obj.ContentType, contentType; got != want {
			t.Errorf("Content-Type = %q; want %q", got, want)
		}
	})
}
