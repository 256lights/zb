// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package remotestore

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/dsnet/compress/brotli"
	"zb.256lights.llc/pkg/internal/hal"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
)

var _ zbstore.Store = (*HTTPStore)(nil)

// An HTTPStore implements [zbstore.Store] by using the [Binary Cache Protocol].
//
// [Binary Cache Protocol]: https://zb.256lights.llc/binary-cache/
type HTTPStore struct {
	// URL is the URL of the binary cache discovery document.
	// This must be non-nil or the store's methods will return errors.
	URL *url.URL
	// Store methods use HTTPClient to make HTTP requests.
	// It is recommended to use a client that performs caching.
	// If HTTPClient is nil, then [http.DefaultClient] is used.
	HTTPClient *http.Client
}

func (s *HTTPStore) client() *http.Client {
	if s.HTTPClient == nil {
		return http.DefaultClient
	}
	return s.HTTPClient
}

func (s *HTTPStore) discover(ctx context.Context) (*hal.Resource, error) {
	if s.URL == nil {
		return nil, fmt.Errorf("get discovery document: url missing")
	}

	data, err := fetch(ctx, s.client(), s.URL, "application/hal+json,application/json;q=0.9,text/*;q=0.8,*/*;q=0.7")
	if err != nil {
		return nil, fmt.Errorf("get discovery document: %v", err)
	}
	hr := new(hal.Resource)
	if err := json.Unmarshal(data, hr); err != nil {
		return nil, fmt.Errorf("get discovery document: %v", err)
	}
	return hr, nil
}

const narInfoRelation = "https://zb-build.dev/api/rel/narinfo"

// Object fetches the .narinfo resource for the store object at the given path.
func (s *HTTPStore) Object(ctx context.Context, path zbstore.Path) (zbstore.Object, error) {
	hr, err := s.discover(ctx)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %v", path, err)
	}
	infoLinks := hr.Links[narInfoRelation]
	if infoLinks.Single {
		return nil, fmt.Errorf("stat %s: link relation %s is not an array", path, narInfoRelation)
	}

	c := s.client()
	params := struct {
		Base   string
		Digest string
	}{
		Base:   path.Base(),
		Digest: path.Digest(),
	}
	var allErrors error
	for _, link := range infoLinks.Objects {
		if !link.Templated {
			return nil, fmt.Errorf("stat %s: link relation %s: not all links are templated", path, narInfoRelation)
		}
		u, err := link.Expand(params)
		if err != nil {
			return nil, err
		}
		u = s.URL.ResolveReference(u)
		info, err := fetchNARInfo(ctx, c, u)
		if err == nil {
			return &httpObject{
				base:   u,
				client: c,
				info:   info,
			}, nil
		}
		if statusCode, _ := errorStatusCode(err); statusCode == http.StatusNotFound {
			log.Debugf(ctx, "NAR info not found: %v", err)
		} else {
			allErrors = errors.Join(allErrors, err)
		}
	}

	if allErrors == nil {
		allErrors = fmt.Errorf("stat %s: %w", path, zbstore.ErrNotFound)
	}
	return nil, allErrors
}

func fetchNARInfo(ctx context.Context, client *http.Client, u *url.URL) (*NARInfo, error) {
	data, err := fetch(ctx, client, u, "text/x-nix-narinfo,text/*;q=0.9,*/*;q=0.8")
	if err != nil {
		return nil, err
	}
	result := new(NARInfo)
	if err := result.UnmarshalText(data); err != nil {
		return nil, fmt.Errorf("fetch %v: %v", u.Redacted(), err)
	}
	return result, nil
}

func fetch(ctx context.Context, client *http.Client, u *url.URL, accept string) ([]byte, error) {
	req := (&http.Request{
		Method: http.MethodGet,
		URL:    u,
		Header: http.Header{
			"Accept":          {accept},
			"Accept-Encoding": {acceptEncoding},
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
		dec, err := decodeBody(bytes.NewReader(data), e)
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

// acceptEncoding is the value of an [Accept-Encoding header]
// that advertises the algorithms that [decodeBody] supports.
//
// [Accept-Encoding header]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Accept-Encoding
const acceptEncoding = "br,gzip,deflate"

func decodeBody(r io.Reader, contentEncoding string) (io.ReadCloser, error) {
	switch contentEncoding {
	case "":
		return io.NopCloser(r), nil
	case "br":
		return brotli.NewReader(r, nil)
	case "gzip", "x-gzip":
		return gzip.NewReader(r)
	case "deflate":
		return flate.NewReader(r), nil
	default:
		return nil, fmt.Errorf("unsupported Content-Encoding %s", contentEncoding)
	}
}

// httpObject is the implementation of [zbstore.Object] for [HTTPStore].
type httpObject struct {
	client *http.Client
	base   *url.URL
	info   *NARInfo
}

func (obj *httpObject) Trailer() *zbstore.ExportTrailer {
	return &zbstore.ExportTrailer{
		StorePath:      obj.info.StorePath,
		References:     obj.info.References,
		Deriver:        obj.info.Deriver,
		ContentAddress: obj.info.CA,
	}
}

func (obj *httpObject) WriteNAR(ctx context.Context, dst io.Writer) error {
	ref, err := url.Parse(obj.info.URL)
	if err != nil {
		return fmt.Errorf("download %s: invalid nar url: %v", obj.info.StorePath, err)
	}
	narFileURL := obj.base.ResolveReference(ref)

	req := &http.Request{
		Method: http.MethodGet,
		URL:    narFileURL,
		Header: http.Header{
			"Accept":          {"*/*"},
			"Accept-Encoding": {acceptEncoding},
		},
	}
	resp, err := obj.client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: get %s: %v", obj.info.StorePath, narFileURL.Redacted(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: get %s: %v", obj.info.StorePath, narFileURL.Redacted(), &httpError{
			statusCode: resp.StatusCode,
			status:     resp.Status,
		})
	}
	decodedBody, err := decodeBody(resp.Body, resp.Header.Get("Content-Encoding"))
	if err != nil {
		return fmt.Errorf("download %s: get %s: %v", obj.info.StorePath, narFileURL.Redacted(), err)
	}
	defer decodedBody.Close()
	if _, err := io.Copy(dst, decodedBody); err != nil {
		return fmt.Errorf("download %s: get %s: %v", obj.info.StorePath, narFileURL.Redacted(), err)
	}
	return nil
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
