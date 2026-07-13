// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package remotestore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"

	jsonv2 "github.com/go-json-experiment/json"
	"zb.256lights.llc/pkg/bytebuffer"
	"zb.256lights.llc/pkg/internal/fileurl"
	"zb.256lights.llc/pkg/internal/hal"
	"zb.256lights.llc/pkg/internal/httpencoding"
	"zb.256lights.llc/pkg/internal/multierror"
	"zb.256lights.llc/pkg/internal/useragent"
	"zb.256lights.llc/pkg/internal/xio"
	"zb.256lights.llc/pkg/internal/xurl"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
)

var _ interface {
	zbstore.Store
	zbstore.RealizationFetcher
} = (*HTTPStore)(nil)

const (
	narRelation         = "https://zb-build.dev/api/rel/nar"
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
	// CreateTemp is called to create temporary storage for uploading.
	// If CreateTemp is nil, uploads will store NAR files in memory.
	// This is generally not recommended, as the files can be large.
	CreateTemp bytebuffer.Creator
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

	res, err := fetch(ctx, s.client(), s.URL, "application/hal+json,application/json;q=0.9,text/*;q=0.8,*/*;q=0.7")
	if err != nil {
		return nil, fmt.Errorf("get discovery document: %v", err)
	}
	hr := new(hal.Resource)
	if err := jsonv2.Unmarshal(res.body, hr); err != nil {
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
	var ec multierror.Collector
	c := s.client()
	for u := range s.narInfoURLs(&ec, hr, path) {
		info, _, err := fetchNARInfo(ctx, c, u)
		if err == nil {
			return &httpObject{
				base:   u,
				client: c,
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

func (s *HTTPStore) narInfoURLs(ec *multierror.Collector, discoveryDocument *hal.Resource, path zbstore.Path) iter.Seq[*url.URL] {
	infoLinks := discoveryDocument.Links[narInfoRelation]
	if infoLinks.Single {
		return func(yield func(*url.URL) bool) {
			ec.Add(fmt.Errorf("%s: link relation %s is not an array", s.URL.Redacted(), narInfoRelation))
		}
	}
	params := struct {
		Base   string
		Digest string
	}{
		Base:   path.Base(),
		Digest: path.Digest(),
	}
	return func(yield func(*url.URL) bool) {
		addedNotTemplatedError := false
		for _, link := range infoLinks.Objects {
			if !link.Templated {
				if !addedNotTemplatedError {
					ec.Add(fmt.Errorf("%s: link relation %s: not all links are templated", s.URL.Redacted(), narInfoRelation))
					addedNotTemplatedError = true
				}
				continue
			}
			u, err := link.Expand(params)
			if err != nil {
				ec.Add(fmt.Errorf("%s: link relation %s: %v", s.URL.Redacted(), narInfoRelation, err))
				continue
			}
			u, err = resolveReference(s.URL, u)
			if err != nil {
				ec.Add(fmt.Errorf("%s: link relation %s: %v", s.URL.Redacted(), narInfoRelation, err))
				continue
			}
			if !yield(u) {
				return
			}
		}
	}
}

func fetchNARInfo(ctx context.Context, client *http.Client, u *url.URL) (info *NARInfo, putAllowed bool, err error) {
	res, err := fetch(ctx, client, u, "text/x-nix-narinfo,text/*;q=0.9,*/*;q=0.8")
	putAllowed = res.isMethodAllowed(http.MethodPut)
	if err != nil {
		return nil, putAllowed, err
	}
	result := new(NARInfo)
	if err := result.UnmarshalText(res.body); err != nil {
		return nil, putAllowed, fmt.Errorf("fetch %v: %v", u.Redacted(), err)
	}
	return result, putAllowed, nil
}

// PutObjectRequest is holds the arguments to [*HTTPStore.PutObject].
type PutObjectRequest struct {
	// StorePath is the path of the store object.
	// It must not be empty.
	StorePath zbstore.Path
	// Reference is the set of references the store object has to other store objects.
	References sets.Sorted[zbstore.Path]
	// ContentAddress is the store object's content-addressability assertion.
	// It must not be zero.
	ContentAddress zbstore.ContentAddress
	// NAR is a stream of the store object in NAR format.
	// It must not be nil.
	NAR io.Reader
	// NARSize is the number of bytes in NAR.
	// If NARSize is non-positive,
	// then it will be computed from the number of bytes read from NAR.
	// Passing this will enable additional checks.
	NARSize int64
}

// PutObject uploads a store object to the store
// or does nothing if the object already exists in the store.
// PutObject first searches for an existing .narinfo file for the store path.
// If none is found, then two PUT requests are made:
// the first to upload the NAR file
// and the second to upload the .narinfo file.
// The object is verified during transit,
// so if after writing the NAR file the content does not match the trailer,
// then the .narinfo file is never uploaded.
func (s *HTTPStore) PutObject(ctx context.Context, req *PutObjectRequest) error {
	if req.StorePath == "" {
		return fmt.Errorf("upload: path not set")
	}
	if req.ContentAddress.IsZero() {
		return fmt.Errorf("upload %s: content address not set", req.StorePath)
	}

	hr, err := s.discover(ctx)
	if err != nil {
		return fmt.Errorf("upload %s: %v", req.StorePath, err)
	}

	// Look at existing .narinfo files for a few reasons:
	//
	// 1. See if this object already exists in the store.
	// 2. If the object already exists, does it match our information?
	// 3. Do any of the .narinfo URLs specifically advertise not supporting PUT?
	var ec multierror.Collector
	var putURLs []*url.URL
	c := s.client()
	hasInfoURLs := false
	hasConflicts := false
	for u := range s.narInfoURLs(&ec, hr, req.StorePath) {
		hasInfoURLs = true
		remoteInfo, putAllowed, err := fetchNARInfo(ctx, c, u)
		if putAllowed {
			putURLs = append(putURLs, u)
		}
		if err != nil {
			if isNotFound(err) {
				log.Debugf(ctx, "While uploading %s, as expected: %v", req.StorePath, err)
			} else {
				ec.Add(err)
			}
			continue
		}
		if !ensureInfoMatches(&ec, req, u, remoteInfo) {
			hasConflicts = true
		} else if !hasConflicts {
			// If the first fetched .narinfo is congruent, then no-op.
			if err := ec.Error(); err != nil {
				log.Warnf(ctx, "Found existing %s at %s. Skipping upload. While searching: %v",
					req.StorePath, u.Redacted(), err)
			} else {
				log.Debugf(ctx, "Found existing %s at %s. Skipping upload.", req.StorePath, u.Redacted())
			}
			return nil
		}
	}
	if !hasInfoURLs {
		ec.Add(fmt.Errorf("%s: missing valid %s link", s.URL.Redacted(), narInfoRelation))
	} else if len(putURLs) == 0 {
		ec.Add(fmt.Errorf("%s: %s links do not permit %s", s.URL.Redacted(), narInfoRelation, http.MethodPut))
	}
	if err := ec.Error(); err != nil {
		var ec2 multierror.Collector
		for err := range multierror.All(err) {
			ec2.Add(fmt.Errorf("upload %s: %w", req.StorePath, err))
		}
		return ec2.Error()
	}

	narLink, hasNARLink := hr.Links[narRelation].Get()
	if !hasNARLink {
		return fmt.Errorf("upload %s: %s: missing %s link", req.StorePath, s.URL.Redacted(), narRelation)
	}
	params := struct {
		Base   string
		Digest string
	}{
		Base:   req.StorePath.Base(),
		Digest: req.StorePath.Digest(),
	}
	narURL, err := narLink.Expand(params)
	if err != nil {
		return fmt.Errorf("upload %s: %s: %v", req.StorePath, s.URL.Redacted(), err)
	}
	narURL, err = resolveReference(s.URL, narURL)
	if err != nil {
		return fmt.Errorf("upload %s: %s: %s: link: %v",
			req.StorePath, s.URL.Redacted(), narRelation, err)
	}

	verifyWriter := make(chan io.Writer)
	verifyWriteDone := make(chan error)
	verifyDone := make(chan error)
	go func() {
		obj := &fakeObject{
			trailer: zbstore.ExportTrailer{
				StorePath:      req.StorePath,
				References:     req.References,
				ContentAddress: req.ContentAddress,
			},
			writer:    verifyWriter,
			writeDone: verifyWriteDone,
		}
		verifyDone <- zbstore.VerifyObject(ctx, obj, &zbstore.ContentAddressOptions{
			CreateTemp: s.CreateTemp,
		})
	}()

	hasher := nix.NewHasher(nix.SHA256)
	narContent := req.NAR
	narSize := int64(-1)
	if req.NARSize > 0 {
		narContent = http.MaxBytesReader(nil, io.NopCloser(req.NAR), req.NARSize)
		narSize = req.NARSize
	}
	wc := new(xio.WriteCounter)
	narContent = io.TeeReader(narContent, io.MultiWriter(wc, hasher, <-verifyWriter))
	const cacheControl = "max-age=2592000" // 1 week
	uploadNARError := put(ctx, c, &putRequest{
		url:           narURL,
		contentLength: narSize,
		contentType:   nar.MIMEType,
		cacheControl:  cacheControl,
		content:       narContent,
		// Replacement is fine, even if the contents differ.
		// We want PutObject to be idempotent, especially if a previous operation failed.
		// If there is a URL collision and multiple distinct .narinfo files referencing it,
		// then the other ones will detect it.
		noReplace: false,
	})
	verifyWriteDone <- uploadNARError
	verifyError := <-verifyDone
	if uploadNARError != nil {
		return fmt.Errorf("upload %s: %v", req.StorePath, uploadNARError)
	}
	if verifyError == nil && narSize >= 0 && int64(*wc) != narSize {
		verifyError = fmt.Errorf("nar size = %d bytes (hint was %d bytes)", *wc, narSize)
	}
	if verifyError != nil {
		return fmt.Errorf("upload %s: %v", req.StorePath, verifyError)
	}
	narSize = int64(*wc)

	for _, u := range putURLs {
		relNARURL, err := xurl.Rel(u, narURL)
		if err != nil {
			ec.Add(err)
			continue
		}
		narinfoData, err := (&NARInfo{
			StorePath:   req.StorePath,
			References:  req.References,
			URL:         relNARURL.String(),
			Compression: NoCompression,
			CA:          req.ContentAddress,
			NARHash:     hasher.SumHash(),
			NARSize:     narSize,
		}).MarshalText()
		if err != nil {
			ec.Add(err)
			continue
		}
		uploadInfoRequest := &putRequest{
			url:           u,
			content:       bytes.NewReader(narinfoData),
			contentLength: int64(len(narinfoData)),
			contentType:   NARInfoMIMEType,
			cacheControl:  cacheControl,
			noReplace:     true,
		}
		uploadError := put(ctx, c, uploadInfoRequest)
		if uploadError == nil {
			if err := ec.Error(); err != nil {
				log.Warnf(ctx, "While uploading %s: %v", req.StorePath, err)
			}
			return nil
		}
		ec.Add(uploadError)
		if !isMethodNotAllowed(uploadError) {
			break
		}
	}

	var ec2 multierror.Collector
	for err := range multierror.All(ec.Error()) {
		ec2.Add(fmt.Errorf("upload %s: %w", req.StorePath, err))
	}
	return ec2.Error()
}

// ensureInfoMatches reports whether the remote [NARInfo]
// matches an object we're about to upload.
// If it does not, then errors will be added to the [multierror.Collector].
func ensureInfoMatches(ec *multierror.Collector, req *PutObjectRequest, u *url.URL, remoteInfo *NARInfo) bool {
	matches := true
	if remoteInfo.StorePath != req.StorePath {
		ec.Add(fmt.Errorf("%s: mismatched store path %s", u.Redacted(), remoteInfo.StorePath))
		matches = false
	}
	if remoteInfo.CA.IsZero() {
		ec.Add(fmt.Errorf("%s: missing content address", u.Redacted()))
		matches = false
	} else if !remoteInfo.CA.Equal(req.ContentAddress) {
		ec.Add(fmt.Errorf("%s: content address = %v; expecting %v",
			u.Redacted(), remoteInfo.CA, req.ContentAddress))
		matches = false
	}
	if req.NARSize > 0 && remoteInfo.NARSize != req.NARSize {
		ec.Add(fmt.Errorf("%s: nar size = %d bytes; expecting %d bytes",
			u.Redacted(), remoteInfo.NARSize, req.NARSize))
		matches = false
	}
	return matches
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
		HashAlgorithm    string
		HashDigest       string
		HashDigestHex    string
		HashDigestBase64 string
	}{
		HashAlgorithm:    drvHash.Type().String(),
		HashDigest:       drvHash.RawBase32(),
		HashDigestHex:    drvHash.RawBase16(),
		HashDigestBase64: drvHash.RawBase64(),
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
	res, err := fetch(ctx, s.client(), u, "application/json,text/*;q=0.9,*/*;q=0.8")
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

type fakeObject struct {
	trailer   zbstore.ExportTrailer
	writer    chan<- io.Writer
	writeDone <-chan error
}

func (obj *fakeObject) Trailer() *zbstore.ExportTrailer {
	return &obj.trailer
}

func (obj *fakeObject) WriteNAR(ctx context.Context, dst io.Writer) error {
	if obj.writer == nil {
		return fmt.Errorf("already written")
	}
	obj.writer <- dst
	obj.writer = nil
	return <-obj.writeDone
}

func resolveReference(baseURL, ref *url.URL) (*url.URL, error) {
	targetURL := baseURL.ResolveReference(ref)
	if (targetURL.Scheme == "" || targetURL.Scheme == fileurl.Scheme) && baseURL.Scheme != fileurl.Scheme {
		return nil, fmt.Errorf("link to %s not permitted from %s", ref.Redacted(), baseURL.Redacted())
	}
	return targetURL, nil
}
