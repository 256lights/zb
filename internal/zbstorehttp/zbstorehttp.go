// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

// Package zbstorehttp provides a client of the [zb binary cache protocol].
//
// [zb binary cache protocol]: https://zb.256lights.llc/binary-cache/
package zbstorehttp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"time"

	jsonv2 "github.com/go-json-experiment/json"
	"zb.256lights.llc/pkg/bytebuffer"
	"zb.256lights.llc/pkg/internal/fileurl"
	"zb.256lights.llc/pkg/internal/hal"
	"zb.256lights.llc/pkg/internal/httpencoding"
	"zb.256lights.llc/pkg/internal/multierror"
	"zb.256lights.llc/pkg/internal/xhttp"
	"zb.256lights.llc/pkg/internal/xtime"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
)

var _ interface {
	zbstore.Store
	zbstore.RealizationFetcher
} = (*Store)(nil)

const (
	narRelation         = "https://zb-build.dev/api/rel/nar"
	narInfoRelation     = "https://zb-build.dev/api/rel/narinfo"
	realizationRelation = "https://zb-build.dev/api/rel/realization"
)

// A Store implements [zbstore.Store] and [zbstore.RealizationFetcher]
// using the [Binary Cache Protocol].
//
// [Binary Cache Protocol]: https://zb.256lights.llc/binary-cache/
type Store struct {
	// URL is the URL of the binary cache discovery document.
	// This must be non-nil or the store's methods will return errors.
	URL *url.URL
	// Store methods use HTTPClient to make HTTP requests.
	// It is recommended to use a client that performs caching.
	// If HTTPClient is nil, then [http.DefaultClient] is used.
	HTTPClient Client
	// CreateTemp is called to create temporary storage for uploading.
	// If CreateTemp is nil, uploads will store NAR files in memory.
	// This is generally not recommended, as the files can be large.
	CreateTemp bytebuffer.Creator
	// RealizationsCacheControl is the Cache-Control header value to use
	// when uploading a realizations document.
	RealizationsCacheControl string
}

func (s *Store) client() Client {
	if s.HTTPClient == nil {
		return http.DefaultClient
	}
	return s.HTTPClient
}

func (s *Store) discover(ctx context.Context) (*hal.Resource, error) {
	if s.URL == nil {
		return nil, permanentError{fmt.Errorf("get discovery document: url missing")}
	}

	res, err := fetch(ctx, s.client(), &fetchRequest{
		url:    s.URL,
		accept: "application/hal+json,application/json;q=0.9,text/*;q=0.8,*/*;q=0.7",
	})
	if err != nil {
		code, _ := errorStatusCode(err)
		err := fmt.Errorf("get discovery document: %v", err)
		if code == http.StatusGone {
			// Gone indicates that retries to obtain this resource will continue to fail.
			err = permanentError{err}
		}
		return nil, err
	}
	hr := new(hal.Resource)
	if err := jsonv2.Unmarshal(res.body, hr); err != nil {
		return nil, permanentError{fmt.Errorf("get discovery document: %v", err)}
	}
	return hr, nil
}

// Object fetches the .narinfo resource for the store object at the given path.
func (s *Store) Object(ctx context.Context, path zbstore.Path) (zbstore.Object, error) {
	hr, err := s.discover(ctx)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	var ec multierror.Collector
	for u := range s.narInfoURLs(&ec, hr, path) {
		info, _, err := s.fetchNARInfo(ctx, u)
		if err == nil {
			return &httpObject{
				base:   u,
				client: s.client(),
				info:   info,
			}, nil
		}
		if isNotFound(err) {
			log.Debugf(ctx, "NAR info not found: %v", err)
		} else {
			ec.Add(err)
		}
	}

	err = ec.Error()
	if err == nil {
		err = fmt.Errorf("stat %s: %w", path, zbstore.ErrNotFound)
	} else {
		var ec2 multierror.Collector
		for err := range multierror.All(err) {
			ec2.Add(fmt.Errorf("stat %s: %w", path, err))
		}
		err = ec2.Error()
	}
	return nil, err
}

func (s *Store) narInfoURLs(ec *multierror.Collector, discoveryDocument *hal.Resource, path zbstore.Path) iter.Seq[*url.URL] {
	return s.expandLinks(ec, discoveryDocument, narInfoRelation, struct {
		Base   string
		Digest string
	}{
		Base:   path.Base(),
		Digest: path.Digest(),
	})
}

func (s *Store) fetchNARInfo(ctx context.Context, u *url.URL) (info *NARInfo, putAllowed bool, err error) {
	res, err := fetch(ctx, s.client(), &fetchRequest{
		url:    u,
		accept: "text/x-nix-narinfo,text/*;q=0.9,*/*;q=0.8",
		origin: s.URL,
	})
	putAllowed = requestNegotiationFromFetchResponse(res, err).isMethodAllowed(http.MethodPut)
	if err != nil {
		return nil, putAllowed, err
	}
	result := new(NARInfo)
	if err := result.UnmarshalText(res.body); err != nil {
		return nil, putAllowed, fmt.Errorf("fetch %v: %v", u.Redacted(), err)
	}
	return result, putAllowed, nil
}

// FetchRealizations implements [zbstore.RealizationFetcher]
// by fetching the realization document(s) for the given [derivation hash].
//
// [derivation hash]: https://zb.256lights.llc/binary-cache/realizations#derivation-hashes
func (s *Store) FetchRealizations(ctx context.Context, drvHash nix.Hash) (zbstore.RealizationMap, error) {
	result := zbstore.RealizationMap{DerivationHash: drvHash}
	hr, err := s.discover(ctx)
	if err != nil {
		return result, fmt.Errorf("fetch realizations for %v: %w", drvHash, err)
	}

	var ec multierror.Collector
	for u := range s.realizationURLs(&ec, hr, drvHash) {
		if err := s.addRealizations(ctx, &result, u); err != nil {
			ec.Add(err)
			continue
		}
	}

	err = ec.Error()
	ec = multierror.Collector{}
	for err := range multierror.All(err) {
		ec.Add(fmt.Errorf("fetch realizations for %v: %v", drvHash, err))
	}
	return result, ec.Error()
}

func (s *Store) addRealizations(ctx context.Context, dst *zbstore.RealizationMap, u *url.URL) error {
	res, err := fetch(ctx, s.client(), &fetchRequest{
		url:    u,
		accept: "application/json,text/*;q=0.9,*/*;q=0.8",
		origin: s.URL,
	})
	if err != nil {
		if isNotFound(err) {
			log.Debugf(ctx, "Fetch realizations for %v: %v", dst.DerivationHash, err)
			return nil
		}
		return fmt.Errorf("fetch realizations for %v: %w", dst.DerivationHash, err)
	}
	doc := new(zbstore.RealizationMap)
	unmarshalers := jsonv2.UnmarshalFromFunc(zbstore.UnmarshalHashJSONFrom)
	if err := jsonv2.Unmarshal(res.body, doc, jsonv2.WithUnmarshalers(unmarshalers)); err != nil {
		return fmt.Errorf("fetch realizations for %v: %v: %v", dst.DerivationHash, u.Redacted(), err)
	}
	if err := dst.Merge(*doc); err != nil {
		return fmt.Errorf("fetch realizations for %v: %v: %v", dst.DerivationHash, u.Redacted(), err)
	}
	return nil
}

// PutRealizations uploads the realizations to the store.
// PutRealizations attempts to send a PUT request to each "https://zb-build.dev/api/rel/realization" link
// from the discovery document in sequence until one succeeds.
// Conditional requests are used to prevent lost concurrent updates,
// as best as the server supports.
func (s *Store) PutRealizations(ctx context.Context, realizations zbstore.RealizationMap) error {
	if realizations.IsEmpty() {
		return nil
	}

	hr, err := s.discover(ctx)
	if err != nil {
		return fmt.Errorf("update realizations for %v: %w", realizations.DerivationHash, err)
	}

	var ec multierror.Collector
	hasPutAllowed := false
	for u := range s.realizationURLs(&ec, hr, realizations.DerivationHash) {
		var retries multierror.Collector
		for attempt := 1; attempt < 50; attempt++ {
			err := s.putRealizations(ctx, u, realizations)
			if err == nil {
				if err := ec.Error(); err != nil {
					log.Warnf(ctx, "While updating realizations for %v: %v", realizations.DerivationHash, err)
				}
				return nil
			}
			retries.Add(errors.New(err.Error()))
			code, hasResponse := errorStatusCode(err)
			if hasResponse && !isMethodNotAllowed(err) {
				hasPutAllowed = true
			}
			if code != http.StatusPreconditionFailed {
				ec.Add(retries.Error())
				break
			}

			duration := putBackoffTable[min(attempt, len(putBackoffTable)-1)]
			log.Debugf(ctx, "%dth retry to update realizations for %v after %v...",
				attempt, realizations.DerivationHash, duration)
			if err := xtime.Sleep(ctx, duration); err != nil {
				ec.Add(retries.Error())
				ec.Add(err)
				break
			}
		}
	}
	if ec.Error() == nil {
		ec.Add(permanentError{fmt.Errorf("%s: no %s relation", s.URL.Redacted(), realizationRelation)})
	} else if !hasPutAllowed {
		ec.Add(permanentError{fmt.Errorf("%s: %s not supported for %s relation",
			s.URL.Redacted(), http.MethodPut, realizationRelation)})
	}

	var ec2 multierror.Collector
	for err := range multierror.All(ec.Error()) {
		ec2.Add(fmt.Errorf("update realizations for %v: %w", realizations.DerivationHash, err))
	}
	return ec2.Error()
}

func (s *Store) putRealizations(ctx context.Context, u *url.URL, realizations zbstore.RealizationMap) error {
	var existing zbstore.RealizationMap
	oldResource, fetchError := fetch(ctx, s.client(), &fetchRequest{
		url:    u,
		accept: "application/json,text/*;q=0.9,*/*;q=0.8",
		origin: s.URL,
	})
	if !requestNegotiationFromFetchResponse(oldResource, fetchError).isMethodAllowed(http.MethodPut) {
		log.Debugf(ctx, "Skipping %s because %s not in Allow header", u.Redacted(), http.MethodPut)
		return fmt.Errorf("%s: %w", u.Redacted(), methodNotAllowedError{http.MethodPut})
	}
	noReplace := false
	var validators xhttp.ValidatorFields
	switch {
	case fetchError == nil:
		unmarshalers := jsonv2.UnmarshalFromFunc(zbstore.UnmarshalHashJSONFrom)
		if err := jsonv2.Unmarshal(oldResource.body, &existing, jsonv2.WithUnmarshalers(unmarshalers)); err != nil {
			return fmt.Errorf("%s: %v", u.Redacted(), err)
		}
		existing.Compact()
		validators = oldResource.validators
	case isNotFound(fetchError):
		existing = zbstore.RealizationMap{DerivationHash: realizations.DerivationHash}
		noReplace = true
	default:
		// Make error opaque.
		return errors.New(fetchError.Error())
	}
	if err := existing.Merge(realizations); err != nil {
		return fmt.Errorf("%s: %v", u.Redacted(), err)
	}

	marshalers := jsonv2.MarshalToFunc(zbstore.MarshalHashJSONTo)
	newData, err := jsonv2.Marshal(existing, jsonv2.WithMarshalers(marshalers))
	if err != nil {
		return fmt.Errorf("%s: %v", u.Redacted(), err)
	}

	err = put(ctx, s.client(), &putRequest{
		url:           u,
		contentLength: int64(len(newData)),
		content:       bytes.NewReader(newData),
		contentType:   "application/json",
		noReplace:     noReplace,
		precondition:  validators,
		cacheControl:  s.RealizationsCacheControl,
	})
	if err != nil {
		if isMethodNotAllowed(err) {
			log.Debugf(ctx, "Skipping %s: %v", u.Redacted(), err)
			err = methodNotAllowedError{http.MethodPut}
		}
		return fmt.Errorf("%s: %w", u.Redacted(), err)
	}
	return nil
}

func (s *Store) realizationURLs(ec *multierror.Collector, discoveryDocument *hal.Resource, drvHash nix.Hash) iter.Seq[*url.URL] {
	return s.expandLinks(ec, discoveryDocument, realizationRelation, struct {
		HashAlgorithm    string
		HashDigest       string
		HashDigestHex    string
		HashDigestBase64 string
	}{
		HashAlgorithm:    drvHash.Type().String(),
		HashDigest:       drvHash.RawBase32(),
		HashDigestHex:    drvHash.RawBase16(),
		HashDigestBase64: drvHash.RawBase64(),
	})
}

func (s *Store) expandLinks(ec *multierror.Collector, discoveryDocument *hal.Resource, rel string, params any) iter.Seq[*url.URL] {
	realizationLinks := discoveryDocument.Links[rel]
	if realizationLinks.Single {
		return func(yield func(*url.URL) bool) {
			ec.Add(fmt.Errorf("%s: link relation %s is not an array", s.URL.Redacted(), rel))
		}
	}
	return func(yield func(*url.URL) bool) {
		addedNotTemplatedError := false
		for _, link := range realizationLinks.Objects {
			if !link.Templated {
				if !addedNotTemplatedError {
					ec.Add(fmt.Errorf("%s: link relation %s: not all links are templated", s.URL.Redacted(), rel))
					addedNotTemplatedError = true
				}
				continue
			}
			u, err := link.Expand(params)
			if err != nil {
				ec.Add(fmt.Errorf("%s: link relation %s: %v", s.URL.Redacted(), rel, err))
				continue
			}
			u, err = resolveReference(s.URL, u)
			if err != nil {
				ec.Add(fmt.Errorf("%s: link relation %s: %v", s.URL.Redacted(), rel, err))
				continue
			}
			if !yield(u) {
				return
			}
		}
	}
}

// httpObject is the implementation of [zbstore.Object] for [Store].
type httpObject struct {
	client Client
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
		},
	}
	resp, err := obj.client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: get %s: %v", obj.info.StorePath, narFileURL.Redacted(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err := httpErrorFromResponse(resp)
		return fmt.Errorf("download %s: get %s: %v", obj.info.StorePath, narFileURL.Redacted(), err)
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

type permanentError struct {
	err error
}

func (te permanentError) Error() string {
	return te.err.Error()
}

func (te permanentError) Unwrap() error {
	return te.err
}

// IsPermanentError reports whether err indicates a failure
// that likely cannot be resolved by retrying.
func IsPermanentError(err error) bool {
	_, ok := errors.AsType[permanentError](err)
	return ok
}

var putBackoffTable = [...]time.Duration{
	0 * time.Millisecond,
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	1000 * time.Millisecond,
	5000 * time.Millisecond,
}
