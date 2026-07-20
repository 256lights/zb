// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package xhttp

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ValidatorFields is a subset of HTTP headers
// that can be used in preconditions.
// See [RFC 9110 Section 8.8].
// The zero value is a set of absent headers.
//
// [RFC 9110 Section 8.8]: https://datatracker.ietf.org/doc/html/rfc9110#section-8.8
type ValidatorFields struct {
	// ETag is the [entity tag].
	// It is the empty string if not present.
	//
	// [entity tag]: https://datatracker.ietf.org/doc/html/rfc9110#section-8.8.3
	ETag EntityTag
	// LastModified is the [Last-Modified field value].
	// It is the zero time if not present.
	//
	// [Last-Modified field value]: https://datatracker.ietf.org/doc/html/rfc9110#section-8.8.2
	LastModified time.Time
}

// ExtractValidatorFields parses the validators from an [http.Header].
func ExtractValidatorFields(h http.Header) ValidatorFields {
	var vf ValidatorFields
	vf.ETag, _ = EntityTagFromHeader(h)
	vf.LastModified, _ = dateHeader(h, "Last-Modified")
	return vf
}

// String formats the fields in a Go-struct-like syntax.
func (vf ValidatorFields) String() string {
	sb := new(strings.Builder)
	sb.WriteString("{")
	if vf.ETag != "" {
		sb.WriteString("ETag:")
		sb.WriteString(string(vf.ETag))
		if !vf.LastModified.IsZero() {
			sb.WriteString(" ")
		}
	}
	if !vf.LastModified.IsZero() {
		sb.WriteString("Last-Modified:")
		sb.WriteString(vf.LastModified.UTC().Format(http.TimeFormat))
	}
	sb.WriteString("}")
	return sb.String()
}

// IsZero reports whether vf is the zero value.
func (vf ValidatorFields) IsZero() bool {
	return vf.ETag == "" && vf.LastModified.IsZero()
}

// HasStrong reports whether vf has at least one strong validator.
func (vf ValidatorFields) HasStrong() bool {
	// TODO(maybe): RFC 9110 8.8.2.2 states that Last-Modified can be strong
	// under certain circumstances I don't quite understand.
	return vf.ETag.IsStrong()
}

// HasWeak reports whether vf has at least one weak validator.
func (vf ValidatorFields) HasWeak() bool {
	return !vf.LastModified.IsZero() || vf.ETag.IsWeak()
}

// HasAnyStrongFrom reports whether vf contains any of the strong validators in want.
func (vf ValidatorFields) HasAnyStrongFrom(want ValidatorFields) bool {
	return vf.ETag.EqualStrong(want.ETag)
}

// HasAnyFrom reports whether vf contains any of the validators in want.
func (vf ValidatorFields) HasAnyFrom(want ValidatorFields) bool {
	return vf.ETag.EqualWeak(want.ETag) ||
		!vf.LastModified.IsZero() && !want.LastModified.IsZero()
}

// EvaluatePreconditions evaluates the preconditions specified in the request header
// for the request method and the origin server resource's validator fields (if any)
// and whether the server resource exists.
// EvaluatePreconditions returns the status code that complies with [RFC 9110 Section 13.2].
//
// [RFC 9110 Section 13.2]: https://datatracker.ietf.org/doc/html/rfc9110#section-13.2
func EvaluatePreconditions(method string, requestHeader http.Header, vf ValidatorFields, exists bool) int {
	if !exists {
		vf = ValidatorFields{}
	}

	if values := requestHeader["If-Match"]; len(values) > 0 {
		if !evaluateIfMatch(values, vf, exists) {
			return http.StatusPreconditionFailed
		}
	} else if values := requestHeader["If-Unmodified-Since"]; len(values) > 0 {
		if !evaluateIfUnmodifiedSince(values, vf) {
			return http.StatusPreconditionFailed
		}
	}
	isGetOrHead := method == "" || method == http.MethodGet || method == http.MethodHead
	if values := requestHeader["If-None-Match"]; len(values) > 0 {
		if !evaluateIfNoneMatch(values, vf, exists) {
			if isGetOrHead {
				return http.StatusNotModified
			} else {
				return http.StatusPreconditionFailed
			}
		}
	} else if values := requestHeader["If-Modified-Since"]; isGetOrHead && len(values) > 0 {
		if !evaluateIfModifiedSince(values, vf) {
			return http.StatusNotModified
		}
	}

	// TODO(someday): Range request.

	switch {
	case isGetOrHead && !exists:
		return http.StatusNotFound
	case method == http.MethodPut && !exists:
		return http.StatusCreated
	default:
		return http.StatusOK
	}
}

func evaluateIfMatch(values []string, vf ValidatorFields, exists bool) bool {
	if len(values) == 1 && values[0] == "*" {
		return exists
	}
	if len(values) == 0 || len(values) == 1 && strings.Trim(values[0], " \t") == "" {
		return true
	}
	if !vf.ETag.IsValid() || !vf.ETag.IsStrong() {
		return false
	}
	for _, value := range values {
		for elem := range SplitList(value) {
			if etag := EntityTag(elem); etag.IsValid() && etag.EqualStrong(vf.ETag) {
				return true
			}
		}
	}
	return false
}

func evaluateIfNoneMatch(values []string, vf ValidatorFields, exists bool) bool {
	if len(values) == 1 && values[0] == "*" {
		return !exists
	}
	if vf.ETag == "" || len(values) == 0 || len(values) == 1 && strings.Trim(values[0], " \t") == "" {
		return true
	}
	for _, value := range values {
		for elem := range SplitList(value) {
			if etag := EntityTag(elem); etag.IsValid() && etag.EqualWeak(vf.ETag) {
				return false
			}
		}
	}
	return true
}

func evaluateIfModifiedSince(values []string, vf ValidatorFields) bool {
	if len(values) == 0 || vf.LastModified.IsZero() {
		return true
	}
	t, err := http.ParseTime(values[0])
	if err != nil {
		return true
	}
	return vf.LastModified.Truncate(time.Second).After(t.Truncate(time.Second))
}

func evaluateIfUnmodifiedSince(values []string, vf ValidatorFields) bool {
	if len(values) == 0 || vf.LastModified.IsZero() {
		return true
	}
	t, err := http.ParseTime(values[0])
	if err != nil {
		return true
	}
	return !vf.LastModified.Truncate(time.Second).After(t.Truncate(time.Second))
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

// StrongEntityTag returns a new, strong [EntityTag] with the given opaque string.
func StrongEntityTag(s string) (EntityTag, error) {
	for _, b := range []byte(s) {
		if !isEntityTagCharacter(b) {
			return "", fmt.Errorf("new entity tag: invalid character %q", b)
		}
	}
	return EntityTag(`"` + s + `"`), nil
}

// WeakEntityTag returns a new, weak [EntityTag] with the given opaque string.
func WeakEntityTag(s string) (EntityTag, error) {
	etag, err := StrongEntityTag(s)
	if err != nil {
		return "", err
	}
	return weakEntityTagPrefix + etag, nil
}

// IsValid reports whether the entity tag is syntactically valid.
func (etag EntityTag) IsValid() bool {
	s := strings.TrimPrefix(string(etag), weakEntityTagPrefix)
	s, quoted := strings.CutPrefix(s, `"`)
	end := len(s) - 1
	if !quoted || end < 0 || s[end] != '"' {
		return false
	}
	for _, b := range []byte(s[:end]) {
		if !isEntityTagCharacter(b) {
			return false
		}
	}
	return true
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

// EntityTagFromHeader parses an [EntityTag] from an [ETag response header].
//
// [ETag response header]: https://www.rfc-r.org/rfc/rfc9110.html#section-8.8.3
func EntityTagFromHeader(h http.Header) (_ EntityTag, ok bool) {
	etag := EntityTag(headerValue(h, "Etag"))
	return etag, etag.IsValid()
}

func isEntityTagCharacter(b byte) bool {
	return b > ' ' && b != '"' && b != 0x7f
}
