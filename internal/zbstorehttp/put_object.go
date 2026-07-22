// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package zbstorehttp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"zb.256lights.llc/pkg/internal/hal"
	"zb.256lights.llc/pkg/internal/multierror"
	"zb.256lights.llc/pkg/internal/xio"
	"zb.256lights.llc/pkg/internal/xurl"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
)

// PutObjectRequest is holds the arguments to [*Store.PutObject].
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
func (s *Store) PutObject(ctx context.Context, req *PutObjectRequest) error {
	if req.StorePath == "" {
		return permanentError{fmt.Errorf("upload: path not set")}
	}
	if req.ContentAddress.IsZero() {
		return permanentError{fmt.Errorf("upload %s: content address not set", req.StorePath)}
	}

	hr, err := s.discover(ctx)
	if err != nil {
		return fmt.Errorf("upload %s: %w", req.StorePath, err)
	}
	putInfoURLs, err := s.findExistingInfoForPut(ctx, hr, req)
	if !errors.Is(err, zbstore.ErrNotFound) {
		var ec2 multierror.Collector
		for err := range multierror.All(err) {
			ec2.Add(fmt.Errorf("upload %s: %w", req.StorePath, err))
		}
		return ec2.Error()
	}

	narLink, hasNARLink := hr.Links[narRelation].Get()
	if !hasNARLink {
		return permanentError{fmt.Errorf("upload %s: %s: missing %s link", req.StorePath, s.URL.Redacted(), narRelation)}
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
		return permanentError{fmt.Errorf("upload %s: %s: %v", req.StorePath, s.URL.Redacted(), err)}
	}
	narURL, err = resolveReference(s.URL, narURL)
	if err != nil {
		return permanentError{fmt.Errorf("upload %s: %s: %s: link: %v",
			req.StorePath, s.URL.Redacted(), narRelation, err)}
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
	uploadNARError := put(ctx, s.client(), &putRequest{
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
		err := fmt.Errorf("upload %s: %v", req.StorePath, uploadNARError)
		if isMethodNotAllowed(uploadNARError) {
			err = permanentError{err}
		}
		return err
	}
	if verifyError == nil && narSize >= 0 && int64(*wc) != narSize {
		verifyError = fmt.Errorf("nar size = %d bytes (advertised %d bytes)", *wc, narSize)
	}
	if verifyError != nil {
		return fmt.Errorf("upload %s: %v", req.StorePath, verifyError)
	}
	narSize = int64(*wc)

	var ec multierror.Collector
	for _, u := range putInfoURLs {
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
		uploadError := put(ctx, s.client(), uploadInfoRequest)
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

// findExistingInfoForPut checks whether there is an existing .narinfo file
// that is compatible with the [*PutObjectRequest].
// findExistingInfoForPut returns [zbstore.ErrNotFound] if the store does not have such a file.
// On any error, findExistingInfoForPut will return a list of .narinfo URLs that seem to accept PUTs.
func (s *Store) findExistingInfoForPut(ctx context.Context, discoveryDocument *hal.Resource, req *PutObjectRequest) (putURLs []*url.URL, err error) {
	var ec multierror.Collector
	hasInfoURLs := false
	for u := range s.narInfoURLs(&ec, discoveryDocument, req.StorePath) {
		hasInfoURLs = true
		remoteInfo, putAllowed, fetchError := s.fetchNARInfo(ctx, u)
		if fetchError == nil {
			if !ensureInfoMatches(&ec, req, u, remoteInfo) {
				if len(putURLs) == 0 {
					break
				}
				log.Warnf(ctx, "Found conflicting %s at %s, but have %d other URL(s) that are higher priority. While searching: %v",
					req.StorePath, u.Redacted(), len(putURLs), ec.Error())
				return putURLs, zbstore.ErrNotFound
			}
			if err := ec.Error(); err != nil {
				log.Warnf(ctx, "Found existing %s at %s. Skipping upload. While searching: %v",
					req.StorePath, u.Redacted(), err)
			} else {
				log.Debugf(ctx, "Found existing %s at %s. Skipping upload.", req.StorePath, u.Redacted())
			}
			return nil, nil
		}
		if putAllowed {
			putURLs = append(putURLs, u)
		}
		if isNotFound(fetchError) {
			log.Debugf(ctx, "While uploading %s, as expected: %v", req.StorePath, fetchError)
		} else {
			ec.Add(fetchError)
		}
	}
	if !hasInfoURLs {
		ec.Add(permanentError{fmt.Errorf("%s: missing valid %s link", s.URL.Redacted(), narInfoRelation)})
	} else if len(putURLs) == 0 {
		ec.Add(permanentError{fmt.Errorf("%s: %s links do not permit %s", s.URL.Redacted(), narInfoRelation, http.MethodPut)})
	}
	if err := ec.Error(); err != nil {
		return putURLs, err
	}
	return putURLs, zbstore.ErrNotFound
}

// ensureInfoMatches reports whether the remote [NARInfo]
// matches an object we're about to upload.
// If it does not, then errors will be added to the [multierror.Collector].
func ensureInfoMatches(ec *multierror.Collector, req *PutObjectRequest, u *url.URL, remoteInfo *NARInfo) bool {
	matches := true
	if remoteInfo.StorePath != req.StorePath {
		ec.Add(permanentError{fmt.Errorf("%s: mismatched store path %s", u.Redacted(), remoteInfo.StorePath)})
		matches = false
	}
	if remoteInfo.CA.IsZero() {
		ec.Add(permanentError{fmt.Errorf("%s: missing content address", u.Redacted())})
		matches = false
	} else if !remoteInfo.CA.Equal(req.ContentAddress) {
		ec.Add(permanentError{fmt.Errorf("%s: content address = %v; expecting %v",
			u.Redacted(), remoteInfo.CA, req.ContentAddress)})
		matches = false
	}
	if req.NARSize > 0 && remoteInfo.NARSize != req.NARSize {
		ec.Add(permanentError{fmt.Errorf("%s: nar size = %d bytes; expecting %d bytes",
			u.Redacted(), remoteInfo.NARSize, req.NARSize)})
		matches = false
	}
	return matches
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
