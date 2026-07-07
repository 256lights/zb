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

	"zb.256lights.llc/pkg/internal/xhttp"
)

// forwardResult is the result of a call to [forward].
type forwardResult struct {
	requestedAt        time.Time
	requestHeader      http.Header
	response           *http.Response
	responseReceivedAt time.Time

	// freshenResponses is the set of responses to update response headers for.
	freshenResponses []*storedResponse
	// staleResponseIDs is the set of response IDs to mark as stale.
	staleResponseIDs []int64

	// serveBodyFromCache is true if the response body should be read from the cache
	// and the response.Body will be cleared.
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
	if vary := xhttp.VaryHeader(result.response.Header); !vary.HasWildcard() {
		for key := range vary.FieldNames() {
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
			resp.requestHeader[key] = []string{strings.Join(values, xhttp.HeaderFieldCombiner)}
		}
	}
	return resp, nil
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
		// TODO(someday): Use stale response on server error.
		return nil, err
	}
	newValidators := xhttp.ExtractValidatorFields(result.response.Header)
	responseContentLength, _ := contentLength(result.response.Header)
	fresh, stale := computeFreshen(newValidators, responseContentLength, func(yield func(*storedResponse) bool) {
		vary := xhttp.VaryHeader(result.response.Header)
		for _, resp := range responses {
			if vary.HasWildcard() || resp.matchesRequestHeader(vary, req.Header) {
				if !yield(resp) {
					return
				}
			}
		}
	})

	if newRequest != req && result.response.StatusCode == http.StatusNotModified {
		if len(fresh) == 0 {
			// If there is no matching response after a 304 to a validated request,
			// then the server has committed an HTTP protocol violation.
			// However, it is indicating that the resource has changed.
			// To recover, we can try the original request.
			result, err = do(req)
			if err != nil {
				// TODO(someday): Use stale response on server error.
				return nil, err
			}
		} else {
			result.response.StatusCode = fresh[0].statusCode
			result.response.Body.Close()
			result.response.Body = nil
			result.serveBodyFromCache = true
			result.storedResponseID = fresh[0].id
		}
	}

	// RFC 9111 Section 4.3.5:
	// When a cache makes an inbound HEAD request for a target URI and receives a 200 (OK) response,
	// the cache SHOULD update or invalidate each of its stored GET responses
	// that could have been chosen for that request [...]
	if result.serveBodyFromCache || req.Method == http.MethodHead && result.response.StatusCode == http.StatusOK {
		freshCount := 0
		for _, original := range fresh {
			freshened, err := result.newStoredResponse(original.id, original.responseBodySize)
			if err == nil {
				fresh[freshCount] = freshened
				freshCount++
			}
		}
		clear(fresh[freshCount:])
		result.freshenResponses = fresh[:freshCount]
	}
	result.staleResponseIDs = stale

	return result, nil
}

func rewriteRequestForValidation(req *http.Request, responses []*storedResponse) *http.Request {
	if req.Method == http.MethodHead {
		// Don't rewrite HEAD.
		// As per RFC 9110 Section 13.2.1,
		// "Although conditional request header fields are defined as being usable with the HEAD method
		// (to keep HEAD's semantics consistent with those of GET),
		// there is no point in sending a conditional HEAD
		// because a successful response is around the same size as a 304 (Not Modified) response [...]"
		return req
	}

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

// computeFreshen returns the stored responses to freshen and mark as stale
// based on the validators and Content-Length of the response.
// responses must be in descending order of recency.
// See [Section 4.3.4 of RFC 9111] for a description.
//
// [Section 4.3.4 of RFC 9111]: https://www.rfc-editor.org/rfc/rfc9111.html#section-4.3.4
func computeFreshen(newValidators xhttp.ValidatorFields, contentLength int64, responses iter.Seq[*storedResponse]) (freshenResponses []*storedResponse, staleIDs []int64) {
	switch {
	case newValidators.HasStrong():
		n := 0
		for range responses {
			n++
		}
		staleIDs = make([]int64, 0, n)
		freshenResponses = make([]*storedResponse, 0, n)

		for resp := range responses {
			if xhttp.ExtractValidatorFields(resp.responseHeader).HasAnyStrongFrom(newValidators) && resp.matchesContentLength(contentLength) {
				freshenResponses = append(freshenResponses, resp)
			} else {
				staleIDs = append(staleIDs, resp.id)
			}
		}
		return freshenResponses, staleIDs
	case newValidators.HasWeak():
		n := 0
		for range responses {
			n++
		}
		staleIDs = make([]int64, 0, n)

		for resp := range responses {
			if len(freshenResponses) == 0 && xhttp.ExtractValidatorFields(resp.responseHeader).HasAnyFrom(newValidators) && resp.matchesContentLength(contentLength) {
				// Only the most recent allowed.
				freshenResponses = []*storedResponse{resp}
			} else {
				staleIDs = append(staleIDs, resp.id)
			}
		}
		return freshenResponses, staleIDs
	case !newValidators.IsZero():
		n := 0
		for range responses {
			n++
		}
		staleIDs = make([]int64, 0, n)
		for resp := range responses {
			staleIDs = append(staleIDs, resp.id)
		}
		return nil, staleIDs
	default:
		next, stop := iter.Pull(responses)
		defer stop()

		first, ok := next()
		if !ok {
			return
		}
		if _, hasMore := next(); hasMore {
			return
		}
		stop()
		if xhttp.ExtractValidatorFields(first.responseHeader).IsZero() && first.matchesContentLength(contentLength) {
			return []*storedResponse{first}, nil
		} else {
			return nil, []int64{first.id}
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
