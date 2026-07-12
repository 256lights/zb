// Copyright 2025 The zb Authors
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

	jsonv2 "github.com/go-json-experiment/json"
	"zb.256lights.llc/pkg/internal/fileurl"
	"zb.256lights.llc/pkg/internal/hal"
	"zb.256lights.llc/pkg/internal/httpencoding"
	"zb.256lights.llc/pkg/internal/multierror"
	"zb.256lights.llc/pkg/internal/useragent"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
)

var _ interface {
	zbstore.Store
	zbstore.RealizationFetcher
} = (*HTTPStore)(nil)

const (
	narInfoRelation     = "https://zb-build.dev/api/rel/narinfo"
	realizationRelation = "https://zb-build.dev/api/rel/realization"
)

// An HTTPStore implements [zbstore.Store] and [zbstore.RealizationFetcher]
// using the [Binary Cache Protocol].
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
	if err := jsonv2.Unmarshal(data, hr); err != nil {
		return nil, fmt.Errorf("get discovery document: %v", err)
	}
	return hr, nil
}

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
	var ec multierror.Collector
	for _, link := range infoLinks.Objects {
		if !link.Templated {
			return nil, fmt.Errorf("stat %s: %s: link relation %s: not all links are templated",
				path, s.URL.Redacted(), narInfoRelation)
		}
		u, err := link.Expand(params)
		if err != nil {
			ec.Add(fmt.Errorf("stat %s: %s: link relation %s: %v",
				path, s.URL.Redacted(), narInfoRelation, err))
			continue
		}
		u, err = resolveReference(s.URL, u)
		if err != nil {
			ec.Add(fmt.Errorf("stat %s: %s: link relation %s: %v",
				path, s.URL.Redacted(), narInfoRelation, err))
			continue
		}
		info, err := fetchNARInfo(ctx, c, u)
		if err == nil {
			return &httpObject{
				base:   u,
				client: c,
				info:   info,
			}, nil
		}
		if statusCode, _ := errorStatusCode(err); statusCode == http.StatusNotFound || statusCode == http.StatusGone {
			log.Debugf(ctx, "NAR info not found: %v", err)
		} else {
			ec.Add(fmt.Errorf("stat %s: %v", path, err))
		}
	}

	err = ec.Error()
	if err == nil {
		err = fmt.Errorf("stat %s: %w", path, zbstore.ErrNotFound)
	}
	return nil, err
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

// FetchRealizations implements [zbstore.RealizationFetcher]
// by fetching the realization document(s) for the given [derivation hash].
//
// [derivation hash]: https://zb.256lights.llc/binary-cache/realizations#derivation-hashes
func (s *HTTPStore) FetchRealizations(ctx context.Context, drvHash nix.Hash) (zbstore.RealizationMap, error) {
	result := zbstore.RealizationMap{DerivationHash: drvHash}

	hr, err := s.discover(ctx)
	if err != nil {
		return result, fmt.Errorf("fetch realizations for %v: %v", drvHash, err)
	}
	infoLinks := hr.Links[realizationRelation]
	if infoLinks.Single {
		return result, fmt.Errorf("fetch realizations for %v: %v: link relation %s is not an array", drvHash, s.URL.Redacted(), realizationRelation)
	}
	var ec multierror.Collector
	for _, link := range infoLinks.Objects {
		if !link.Templated {
			ec.Add(fmt.Errorf("fetch realizations for %v: %v: link relation %s: not all links are templated",
				drvHash, s.URL.Redacted(), realizationRelation))
			break
		}
	}

	params := struct {
		HashAlgorithm string
		HashDigest    string
	}{
		HashAlgorithm: drvHash.Type().String(),
		HashDigest:    drvHash.RawBase16(),
	}
	for _, link := range infoLinks.Objects {
		if !link.Templated {
			continue
		}
		u, err := link.Expand(params)
		if err != nil {
			ec.Add(fmt.Errorf("fetch realizations for %v: %v: link relation %s: %v",
				drvHash, s.URL.Redacted(), realizationRelation, err))
			continue
		}
		u, err = resolveReference(s.URL, u)
		if err != nil {
			ec.Add(fmt.Errorf("fetch realizations for %v: %v: link relation %s: %v",
				drvHash, s.URL.Redacted(), realizationRelation, err))
			continue
		}
		if err := s.addRealizations(ctx, &result, u); err != nil {
			ec.Add(err)
			continue
		}
	}

	return result, ec.Error()
}

func (s *HTTPStore) addRealizations(ctx context.Context, dst *zbstore.RealizationMap, u *url.URL) error {
	docData, err := fetch(ctx, s.client(), u, "application/json,text/*;q=0.9,*/*;q=0.8")
	if err != nil {
		if code, _ := errorStatusCode(err); code == http.StatusNotFound || code == http.StatusGone {
			log.Debugf(ctx, "Fetch realizations for %v: %v", dst.DerivationHash, err)
			return nil
		}
		return fmt.Errorf("fetch realizations for %v: %w", dst.DerivationHash, err)
	}
	doc := new(zbstore.RealizationMap)
	unmarshalers := jsonv2.UnmarshalFromFunc(zbstore.UnmarshalHashJSONFrom)
	if err := jsonv2.Unmarshal(docData, doc, jsonv2.WithUnmarshalers(unmarshalers)); err != nil {
		return fmt.Errorf("fetch realizations for %v: %v: %v", dst.DerivationHash, u.Redacted(), err)
	}
	if err := dst.Merge(*doc); err != nil {
		return fmt.Errorf("fetch realizations for %v: %v: %v", dst.DerivationHash, u.Redacted(), err)
	}
	return nil
}

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
	narFileURL, err := resolveReference(obj.base, ref)
	if err != nil {
		return fmt.Errorf("download %s: %v", obj.info.StorePath, err)
	}

	req := &http.Request{
		Method: http.MethodGet,
		URL:    narFileURL,
		Header: http.Header{
			"Accept":          {"*/*"},
			"Accept-Encoding": {httpencoding.Accept},
			"User-Agent":      {useragent.String},
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
	decodedBody, err := httpencoding.Decode(resp.Body, resp.Header.Get("Content-Encoding"))
	if err != nil {
		return fmt.Errorf("download %s: get %s: %v", obj.info.StorePath, narFileURL.Redacted(), err)
	}
	defer decodedBody.Close()
	if _, err := io.Copy(dst, decodedBody); err != nil {
		return fmt.Errorf("download %s: get %s: %v", obj.info.StorePath, narFileURL.Redacted(), err)
	}
	return nil
}

func resolveReference(baseURL, ref *url.URL) (*url.URL, error) {
	targetURL := baseURL.ResolveReference(ref)
	if (targetURL.Scheme == "" || targetURL.Scheme == fileurl.Scheme) && baseURL.Scheme != fileurl.Scheme {
		return nil, fmt.Errorf("link to %s not permitted from %s", ref.Redacted(), baseURL.Redacted())
	}
	return targetURL, nil
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
