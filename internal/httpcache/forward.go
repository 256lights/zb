// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package httpcache

import (
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
	requestHeaders     http.Header
	response           *http.Response
	responseReceivedAt time.Time

	// freshenResponses is the set of responses to update response headers for.
	freshenResponses []*storedResponse

	// serveBodyFromCache is true if the response body should be read from the cache.
	serveBodyFromCache bool
	// storedResponseID is the row ID of the resource in the cache to be used for the response body.
	storedResponseID int64
}

func (result *forwardResult) newStoredResponse(id int64, responseBodySize int64) *storedResponse {
	return &storedResponse{
		id:                 id,
		statusCode:         result.response.StatusCode,
		header:             result.response.Header,
		requestedAt:        result.requestedAt,
		responseReceivedAt: result.responseReceivedAt,
		responseBodySize:   responseBodySize,
	}
}

func (result *forwardResult) canStore() bool {
	return result != nil && canStoreResponse(result.response.StatusCode, result.response.Header)
}

// forward forwards an incoming request to the [http.RoundTripper].
// If there is at least one stored response that can be used to satisfy the request,
// then the request will be transformed into a [validation request]
// before forwarding.
//
// [validation request]: https://www.rfc-editor.org/rfc/rfc9111.html#section-4.3
func forward(rt http.RoundTripper, req *http.Request, responses []*storedResponse) (*forwardResult, error) {
	do := func(req *http.Request) (*forwardResult, error) {
		result := &forwardResult{
			requestHeaders: req.Header,
			requestedAt:    time.Now(),
		}
		var err error
		result.response, err = rt.RoundTrip(req)
		result.responseReceivedAt = time.Now()
		ensureDateHeader(result.response.Header, result.responseReceivedAt)
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

	result.freshenResponses = make([]*storedResponse, 0, len(responses))
	for id := range responseIDsToFreshen(responses, newValidators) {
		stored := result.newStoredResponse(id, storedResponseToUse.responseBodySize)
		result.freshenResponses = append(result.freshenResponses, stored)
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
		lastModified = soleResponse.header["Last-Modified"]
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
			if (contentLength < 0 || resp.responseBodySize == contentLength) && extractValidatorFields(resp.header).hasAnyStrongFrom(newValidators) {
				return resp
			}
		}
	case newValidators.hasWeak():
		for _, resp := range responses {
			if (contentLength < 0 || resp.responseBodySize == contentLength) && extractValidatorFields(resp.header).hasAnyFrom(newValidators) {
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
func responseIDsToFreshen(responses []*storedResponse, newValidators validatorFields) iter.Seq[int64] {
	switch {
	case newValidators.hasStrong():
		return func(yield func(int64) bool) {
			for _, resp := range responses {
				if extractValidatorFields(resp.header).hasAnyStrongFrom(newValidators) {
					if !yield(resp.id) {
						return
					}
				}
			}
		}
	case newValidators.hasWeak():
		return func(yield func(int64) bool) {
			for _, resp := range responses {
				if extractValidatorFields(resp.header).hasAnyFrom(newValidators) {
					// Only the most recent allowed.
					yield(resp.id)
					return
				}
			}
			for _, resp := range responses {
				if !yield(resp.id) {
					return
				}
			}
		}
	case newValidators.IsZero() && len(responses) == 1 && extractValidatorFields(responses[0].header).IsZero():
		return func(yield func(int64) bool) {
			yield(responses[0].id)
		}
	default:
		return func(yield func(int64) bool) {}
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
