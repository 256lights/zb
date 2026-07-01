// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package httpcache

import (
	"net/http"
	"strings"
	"time"
)

// validatorFields is a subset of HTTP headers
// that can be used in preconditions.
// See [Section 8.8 of RFC 9110].
//
// [Section 8.8 of RFC 9110]: https://www.rfc-editor.org/rfc/rfc9110.html#section-8.8
type validatorFields struct {
	entityTag    entityTag
	lastModified time.Time
}

// extractValidatorFields parses the validators from an [http.Header].
func extractValidatorFields(h http.Header) validatorFields {
	var vf validatorFields
	vf.entityTag, _ = entityTagFromHeader(h)
	vf.lastModified, _ = dateHeader(h, "Last-Modified")
	return vf
}

// IsZero reports whether vf is the zero value.
func (vf validatorFields) IsZero() bool {
	return vf.entityTag == "" && vf.lastModified.IsZero()
}

// hasStrong reports whether vf has at least one strong validator.
func (vf validatorFields) hasStrong() bool {
	// TODO(maybe): RFC 9110 8.8.2.2 states that Last-Modified can be strong
	// under certain circumstances I don't quite understand.
	return vf.entityTag.isStrong()
}

// hasWeak reports whether vf has at least one weak validator.
func (vf validatorFields) hasWeak() bool {
	return !vf.lastModified.IsZero() || vf.entityTag.isWeak()
}

// hasAnyStrongFrom reports whether vf contains any of the strong validators in want.
func (vf validatorFields) hasAnyStrongFrom(want validatorFields) bool {
	return vf.entityTag.equalStrong(want.entityTag)
}

// hasAnyFrom reports whether vf contains any of the validators in want.
func (vf validatorFields) hasAnyFrom(want validatorFields) bool {
	return vf.entityTag.equalWeak(want.entityTag) ||
		!vf.lastModified.IsZero() && !want.lastModified.IsZero()
}

func evaluatePreconditions(method string, requestHeader http.Header, vf validatorFields, exists bool) int {
	if !exists {
		vf = validatorFields{}
	}

	if values := requestHeader["If-None-Match"]; len(values) > 0 {
		if !evaluateIfNoneMatch(values, vf, exists) {
			if method == "" || method == http.MethodGet || method == http.MethodHead {
				return http.StatusNotModified
			} else {
				return http.StatusPreconditionFailed
			}
		}
	} else if values := requestHeader["If-Modified-Since"]; len(values) > 0 {
		if !evaluateIfModifiedSince(values, vf) {
			return http.StatusNotModified
		}
	}

	// TODO(someday): Range request.

	return http.StatusOK
}

func evaluateIfNoneMatch(values []string, vf validatorFields, exists bool) bool {
	if len(values) == 1 && values[0] == "*" {
		return !exists
	}
	if vf.entityTag == "" || len(values) == 0 || len(values) == 1 && strings.Trim(values[0], " \t") == "" {
		return true
	}
	for _, value := range values {
		for elem := range splitList(value) {
			if etag := entityTag(elem); etag.isValid() && etag.equalWeak(vf.entityTag) {
				return false
			}
		}
	}
	return true
}

func evaluateIfModifiedSince(values []string, vf validatorFields) bool {
	if len(values) == 0 || vf.lastModified.IsZero() {
		return true
	}
	t, err := http.ParseTime(values[0])
	if err != nil {
		return true
	}
	return vf.lastModified.After(t)
}

// weakEntityTagPrefix is the string that precedes a weak [entityTag].
const weakEntityTagPrefix = "W/"

// An entityTag is
// "an opaque validator for differentiating between multiple representations of the same resource"
// ([Section 8.8.3 of RFC 9110]).
// If an entityTag is non-empty, then its methods (other than [entityTag.isValid]) assume it to be valid.
//
// [Section 8.8.3 of RFC 9110]: https://www.rfc-editor.org/rfc/rfc9110.html#section-8.8.3
type entityTag string

// isValid reports whether the entity tag is syntactically valid.
func (etag entityTag) isValid() bool {
	s := strings.TrimPrefix(string(etag), weakEntityTagPrefix)
	s = strings.TrimPrefix(s, `"`)
	return strings.IndexByte(s, '"') == len(s)-1
}

// isWeak reports whether the entity tag is a weak validator.
func (etag entityTag) isWeak() bool {
	return etag != "" && strings.HasPrefix(string(etag), weakEntityTagPrefix)
}

// isStrong reports whether the entity tag is a strong validator.
func (etag entityTag) isStrong() bool {
	return etag != "" && !etag.isWeak()
}

// equalStrong reports whether etag and etag2 are equal and both strong validators.
func (etag entityTag) equalStrong(etag2 entityTag) bool {
	return etag.isStrong() && etag == etag2
}

// equalWeak reports whether etag and etag2 are equivalent values,
// regardless of whether either is a weak validator.
// If either [entityTag] is empty, then equalWeak returns false.
func (etag entityTag) equalWeak(etag2 entityTag) bool {
	if etag == "" {
		return false
	}
	s1 := strings.TrimPrefix(string(etag), weakEntityTagPrefix)
	s2 := strings.TrimPrefix(string(etag2), weakEntityTagPrefix)
	return s1 == s2
}

// entityTagFromHeader parses an [entityTag] from an [ETag response header].
//
// [ETag response header]: https://www.rfc-editor.org/rfc/rfc9110.html#section-8.8.3
func entityTagFromHeader(h http.Header) (_ entityTag, ok bool) {
	value := headerValue(h, "Etag")
	rest := strings.TrimPrefix(value, "W/")
	rest = strings.TrimPrefix(rest, `"`)
	if strings.IndexByte(rest, '"') != len(rest)-1 {
		return "", false
	}
	return entityTag(value), true
}
