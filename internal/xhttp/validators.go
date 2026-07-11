// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package xhttp

import (
	"net/http"
	"strings"
	"time"
)

// ValidatorFields is a subset of HTTP headers
// that can be used in preconditions.
// See [RFC 9110 Section 8.8].
// The zero value is a set of absent headers.
//
// [RFC 9110 Section 8.8]: https://www.rfc-editor.org/rfc/rfc9110.html#section-8.8
type ValidatorFields struct {
	entityTag    EntityTag
	lastModified time.Time
}

// ExtractValidatorFields parses the validators from an [http.Header].
func ExtractValidatorFields(h http.Header) ValidatorFields {
	var vf ValidatorFields
	vf.entityTag, _ = EntityTagFromHeader(h)
	vf.lastModified, _ = dateHeader(h, "Last-Modified")
	return vf
}

// IsZero reports whether vf is the zero value.
func (vf ValidatorFields) IsZero() bool {
	return vf.entityTag == "" && vf.lastModified.IsZero()
}

// AddHeader adds the fields from vf to h.
func (vf ValidatorFields) AddHeader(h http.Header) {
	if vf.entityTag != "" {
		setHeader(h, "Etag", string(vf.entityTag))
	}
	if !vf.lastModified.IsZero() {
		setHeader(h, "Last-Modified", vf.lastModified.UTC().Format(http.TimeFormat))
	}
}

// HasStrong reports whether vf has at least one strong validator.
func (vf ValidatorFields) HasStrong() bool {
	// TODO(maybe): RFC 9110 8.8.2.2 states that Last-Modified can be strong
	// under certain circumstances I don't quite understand.
	return vf.entityTag.IsStrong()
}

// HasWeak reports whether vf has at least one weak validator.
func (vf ValidatorFields) HasWeak() bool {
	return !vf.lastModified.IsZero() || vf.entityTag.IsWeak()
}

// HasAnyStrongFrom reports whether vf contains any of the strong validators in want.
func (vf ValidatorFields) HasAnyStrongFrom(want ValidatorFields) bool {
	return vf.entityTag.EqualStrong(want.entityTag)
}

// HasAnyFrom reports whether vf contains any of the validators in want.
func (vf ValidatorFields) HasAnyFrom(want ValidatorFields) bool {
	return vf.entityTag.EqualWeak(want.entityTag) ||
		!vf.lastModified.IsZero() && !want.lastModified.IsZero()
}

func EvaluatePreconditions(method string, requestHeader http.Header, vf ValidatorFields, exists bool) int {
	if !exists {
		vf = ValidatorFields{}
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

func evaluateIfNoneMatch(values []string, vf ValidatorFields, exists bool) bool {
	if len(values) == 1 && values[0] == "*" {
		return !exists
	}
	if vf.entityTag == "" || len(values) == 0 || len(values) == 1 && strings.Trim(values[0], " \t") == "" {
		return true
	}
	for _, value := range values {
		for elem := range SplitList(value) {
			if etag := EntityTag(elem); etag.IsValid() && etag.EqualWeak(vf.entityTag) {
				return false
			}
		}
	}
	return true
}

func evaluateIfModifiedSince(values []string, vf ValidatorFields) bool {
	if len(values) == 0 || vf.lastModified.IsZero() {
		return true
	}
	t, err := http.ParseTime(values[0])
	if err != nil {
		return true
	}
	return vf.lastModified.After(t)
}

// weakEntityTagPrefix is the string that precedes a weak [EntityTag].
const weakEntityTagPrefix = "W/"

// An EntityTag is
// "an opaque validator for differentiating between multiple representations of the same resource"
// ([Section 8.8.3 of RFC 9110]).
// If an EntityTag is non-empty, then its methods (other than [EntityTag.IsValid]) assume it to be valid.
//
// [Section 8.8.3 of RFC 9110]: https://www.rfc-editor.org/rfc/rfc9110.html#section-8.8.3
type EntityTag string

// IsValid reports whether the entity tag is syntactically valid.
func (etag EntityTag) IsValid() bool {
	s := strings.TrimPrefix(string(etag), weakEntityTagPrefix)
	s = strings.TrimPrefix(s, `"`)
	return strings.IndexByte(s, '"') == len(s)-1
}

// IsWeak reports whether the entity tag is a weak validator.
func (etag EntityTag) IsWeak() bool {
	return etag != "" && strings.HasPrefix(string(etag), weakEntityTagPrefix)
}

// IsStrong reports whether the entity tag is a strong validator.
func (etag EntityTag) IsStrong() bool {
	return etag != "" && !etag.IsWeak()
}

// EqualStrong reports whether etag and etag2 are equal and both strong validators.
func (etag EntityTag) EqualStrong(etag2 EntityTag) bool {
	return etag.IsStrong() && etag == etag2
}

// EqualWeak reports whether etag and etag2 are equivalent values,
// regardless of whether either is a weak validator.
// If either [entityTag] is empty, then EqualWeak returns false.
func (etag EntityTag) EqualWeak(etag2 EntityTag) bool {
	if etag == "" {
		return false
	}
	s1 := strings.TrimPrefix(string(etag), weakEntityTagPrefix)
	s2 := strings.TrimPrefix(string(etag2), weakEntityTagPrefix)
	return s1 == s2
}

// EntityTagFromHeader parses an [entityTag] from an [ETag response header].
//
// [ETag response header]: https://www.rfc-editor.org/rfc/rfc9110.html#section-8.8.3
func EntityTagFromHeader(h http.Header) (_ EntityTag, ok bool) {
	value := headerValue(h, "Etag")
	rest := strings.TrimPrefix(value, "W/")
	rest = strings.TrimPrefix(rest, `"`)
	if strings.IndexByte(rest, '"') != len(rest)-1 {
		return "", false
	}
	return EntityTag(value), true
}
