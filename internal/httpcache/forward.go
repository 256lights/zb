// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package httpcache

import (
	"fmt"
	"iter"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// forwardResult is the result of a call to [forward].
type forwardResult struct {
	requestedAt        time.Time
	requestHeader      http.Header
	response           *http.Response
	responseReceivedAt time.Time

	// freshenResponses is the set of responses to update response headers for.
	freshenResponses []*storedResponse

	// serveBodyFromCache is true if the response body should be read from the cache.
	serveBodyFromCache bool
	// storedResponseID is the row ID of the resource in the cache to be used for the response body.
	storedResponseID int64
}

// newStoredResponse returns a [*storedResponse] from a [forwardResult].
// It returns an error if the response cannot be stored in a cache.
func (result *forwardResult) newStoredResponse(id int64, responseBodySize int64) (*storedResponse, error) {
	resp := &storedResponse{
		id:                 id,
		statusCode:         result.response.StatusCode,
		responseHeader:     result.response.Header,
		requestedAt:        result.requestedAt,
		responseReceivedAt: result.responseReceivedAt,
		responseBodySize:   responseBodySize,
	}
	if vary := varyHeader(result.response.Header); !vary.hasWildcard() {
		for key := range vary.fieldNames() {
			values := result.requestHeader[key]
			if len(values) == 0 {
				continue
			}
			if !canStoreRequestHeader(key) {
				return nil, fmt.Errorf("cannot cache request header %s from Vary", key)
			}
			if resp.requestHeader == nil {
				resp.requestHeader = make(http.Header)
			}
			resp.requestHeader[key] = []string{strings.Join(values, headerFieldCombiner)}
		}
	}
	return resp, nil
}

func (result *forwardResult) canStore() bool {
	return result != nil && canStoreResponse(result.requestHeader, result.response.StatusCode, result.response.Header)
}

// forward forwards an incoming request to the [http.RoundTripper].
// If there is at least one stored response for the request URI that has validators,
// then the request will be transformed into a [validation request]
// before forwarding.
//
// [validation request]: https://www.rfc-editor.org/rfc/rfc9111.html#section-4.3
func forward(rt http.RoundTripper, req *http.Request, responses []*storedResponse) (*forwardResult, error) {
	do := func(req *http.Request) (*forwardResult, error) {
		result := &forwardResult{
			requestHeader: req.Header,
			requestedAt:   time.Now(),
		}
		var err error
		result.response, err = rt.RoundTrip(req)
		result.responseReceivedAt = time.Now()
		if result.response != nil {
			ensureDateHeader(result.response.Header, result.responseReceivedAt)
		}
		return result, err
	}

	newRequest := rewriteRequestForValidation(req, responses)
	result, err := do(newRequest)
	if err != nil {
		// TODO(soon): Use stale response on server error.
		return nil, err
	}
	if newRequest == req || result.response.StatusCode != http.StatusNotModified {
		return result, nil
	}

	newValidators := extractValidatorFields(result.response.Header)
	responseContentLength, _ := contentLength(result.response.Header)
	storedResponseToUse := selectResponseForNotModified(responses, newValidators, responseContentLength)
	if storedResponseToUse == nil {
		// If there is no matching response after a 304 to a validated request,
		// then the server has committed an HTTP protocol violation.
		// However, it is indicating that the resource has changed.
		// To recover, we can try the original request.
		// TODO(soon): Use stale response on server error.
		return do(req)
	}
	result.response.StatusCode = storedResponseToUse.statusCode
	result.response.Body.Close()
	result.response.Body = nil
	result.serveBodyFromCache = true
	result.storedResponseID = storedResponseToUse.id

	var matchingResponses iter.Seq[*storedResponse] = func(yield func(*storedResponse) bool) {
		for _, resp := range responses {
			if resp.matchesRequestHeader(req.Header) {
				if !yield(resp) {
					return
				}
			}
		}
	}
	maxResponsesToFreshen := 0
	for range matchingResponses {
		maxResponsesToFreshen++
	}
	if maxResponsesToFreshen > 0 {
		result.freshenResponses = make([]*storedResponse, 0, maxResponsesToFreshen)
		for id := range responseIDsToFreshen(matchingResponses, newValidators) {
			stored, err := result.newStoredResponse(id, storedResponseToUse.responseBodySize)
			if err == nil {
				result.freshenResponses = append(result.freshenResponses, stored)
			}
		}
	}

	return result, nil
}

func rewriteRequestForValidation(req *http.Request, responses []*storedResponse) *http.Request {
	ifNoneMatch := new(strings.Builder)
	var soleResponse *storedResponse
	multipleResponses := false
	for _, resp := range responses {
		if !resp.responseReceived() {
			continue
		}
		if etag, ok := resp.entityTag(); ok {
			if ifNoneMatch.Len() > 0 {
				const sep = ","
				ifNoneMatch.Grow(len(sep) + len(etag))
				ifNoneMatch.WriteString(sep)
			}
			ifNoneMatch.WriteString(string(etag))
		}
		if !multipleResponses {
			if soleResponse == nil {
				soleResponse = resp
			} else {
				soleResponse = nil
				multipleResponses = true
			}
		}
	}
	var lastModified []string
	if soleResponse != nil {
		lastModified = soleResponse.responseHeader["Last-Modified"]
	}
	if ifNoneMatch.Len() == 0 && len(lastModified) == 0 {
		return req
	}
	req = new(*req)
	if req.Header == nil {
		req.Header = make(http.Header)
	} else {
		req.Header = maps.Clone(req.Header)
	}
	if ifNoneMatch.Len() > 0 {
		// TODO(maybe): What if there are already headers?
		req.Header["If-None-Match"] = []string{ifNoneMatch.String()}
	}
	if len(lastModified) > 0 {
		req.Header["Last-Modified"] = lastModified[:1:1]
	}
	return req
}

// selectResponseForNotModified returns the response from responses
// that matches newValidators and the given Content-Length,
// or nil if no response matches.
func selectResponseForNotModified(responses []*storedResponse, newValidators validatorFields, contentLength int64) *storedResponse {
	switch {
	case newValidators.hasStrong():
		for _, resp := range responses {
			if (contentLength < 0 || resp.responseBodySize == contentLength) && extractValidatorFields(resp.responseHeader).hasAnyStrongFrom(newValidators) {
				return resp
			}
		}
	case newValidators.hasWeak():
		for _, resp := range responses {
			if (contentLength < 0 || resp.responseBodySize == contentLength) && extractValidatorFields(resp.responseHeader).hasAnyFrom(newValidators) {
				return resp
			}
		}
	case len(responses) > 0 && (contentLength < 0 || responses[0].responseBodySize == contentLength):
		return responses[0]
	}

	return nil
}

// responseIDsToFreshen returns an iterator over the IDs in responses that should be updated
// based on the validators of the response with a 304 Not Modified status code.
// responses must be in descending order of recency.
// See [Section 4.3.4 of RFC 9111] for a description.
//
// [Section 4.3.4 of RFC 9111]: https://www.rfc-editor.org/rfc/rfc9111.html#section-4.3.4
func responseIDsToFreshen(responses iter.Seq[*storedResponse], newValidators validatorFields) iter.Seq[int64] {
	switch {
	case newValidators.hasStrong():
		return func(yield func(int64) bool) {
			for resp := range responses {
				if extractValidatorFields(resp.responseHeader).hasAnyStrongFrom(newValidators) {
					if !yield(resp.id) {
						return
					}
				}
			}
		}
	case newValidators.hasWeak():
		return func(yield func(int64) bool) {
			for resp := range responses {
				if extractValidatorFields(resp.responseHeader).hasAnyFrom(newValidators) {
					// Only the most recent allowed.
					yield(resp.id)
					return
				}
			}
			for resp := range responses {
				if !yield(resp.id) {
					return
				}
			}
		}
	case !newValidators.IsZero():
		return func(yield func(int64) bool) {}
	}

	return func(yield func(int64) bool) {
		next, stop := iter.Pull(responses)
		defer stop()

		first, ok := next()
		if !ok {
			return
		}
		if _, hasMore := next(); hasMore {
			return
		}
		if extractValidatorFields(first.responseHeader).IsZero() {
			yield(first.id)
		}
	}
}

// contentLength parses the Content-Length header field.
// If the field cannot be parsed, contentLength returns (-1, false).
//
// [Section 8.6 of RFC 9110]: https://www.rfc-editor.org/rfc/rfc9110.html#section-8.6
func contentLength(h http.Header) (_ int64, ok bool) {
	v := headerValue(h, "Content-Length")
	if v == "" {
		return -1, false
	}
	n, err := strconv.ParseUint(v, 10, 63)
	if err != nil {
		return -1, false
	}
	return int64(n), true
}
