// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

// Package hal provides data types for the [JSON Hypertext Application Language] (HAL).
//
// [JSON Hypertext Application Language]: https://datatracker.ietf.org/doc/html/draft-kelly-json-hal-11
package hal

import (
	"encoding/json"
	"fmt"
	"net/url"

	"zombiezen.com/go/uritemplate"
)

// MediaType is the MIME media type of a HAL document.
const MediaType = "application/hal+json"

// Well-known link relation types.
const (
	SelfRelationType = "self"
)

const (
	linksPropertyName    = "_links"
	embeddedPropertyName = "_embedded"
)

// Resource represents a HAL [resource object].
// The zero value is an empty resource object.
//
// [resource object]: https://datatracker.ietf.org/doc/html/draft-kelly-json-hal-11#name-resource-objects
type Resource struct {
	// Links is a map of [link relation types] to links.
	//
	// [link relation types]: https://datatracker.ietf.org/doc/html/rfc5988#section-4
	Links map[string]ArrayOrObject[*Link]
	// Embedded is a map of link relation types to embedded resources.
	Embedded map[string]ArrayOrObject[*Resource]
	// Properties are top-level properties of the resource object.
	Properties map[string]json.RawMessage
}

// Link returns the link object for the link relation type
// or nil if there is no such link
// or the resource uses an array for the link relation type.
func (r *Resource) Link(rel string) *Link {
	if r == nil {
		return nil
	}
	l, _ := r.Links[rel].Get()
	return l
}

// MarshalJSON marshals the resource as a JSON object.
func (r *Resource) MarshalJSON() ([]byte, error) {
	if r == nil {
		return []byte("null"), nil
	}

	for _, name := range [...]string{linksPropertyName, embeddedPropertyName} {
		if _, ok := r.Properties[name]; ok {
			return nil, fmt.Errorf("marshal hal resource: %s property is reserved", name)
		}
	}

	var buf []byte
	buf = append(buf, '{')
	if len(r.Links) > 0 {
		buf = append(buf, `"`+linksPropertyName+`":`...)
		data, err := json.Marshal(r.Links)
		if err != nil {
			return nil, fmt.Errorf("marshal hal resource: %s: %v", linksPropertyName, err)
		}
		buf = append(buf, data...)
	}
	if len(r.Embedded) > 0 {
		if len(buf) > len("{") {
			buf = append(buf, ',')
		}
		buf = append(buf, `"`+embeddedPropertyName+`":`...)
		data, err := json.Marshal(r.Embedded)
		if err != nil {
			return nil, fmt.Errorf("marshal hal resource: %s: %v", embeddedPropertyName, err)
		}
		buf = append(buf, data...)
	}
	if len(r.Properties) > 0 {
		if len(buf) > len("{") {
			buf = append(buf, ',')
		}
		data, err := json.Marshal(r.Properties)
		if err != nil {
			return nil, fmt.Errorf("marshal hal resource: %v", err)
		}
		buf = append(buf, data[len("{"):]...)
		// Already contains the closing brace, so return early.
		return buf, nil
	}
	buf = append(buf, '}')
	return buf, nil
}

// UnmarshalJSON unmarshals a JSON object into the resource.
func (r *Resource) UnmarshalJSON(data []byte) error {
	var properties map[string]json.RawMessage
	if err := json.Unmarshal(data, &properties); err != nil {
		return fmt.Errorf("unmarshal hal resource: %w", err)
	}
	if links := properties[linksPropertyName]; len(links) > 0 {
		delete(properties, linksPropertyName)
		if err := json.Unmarshal(links, &r.Links); err != nil {
			return fmt.Errorf("unmarshal hal resource: %s: %v", linksPropertyName, err)
		}
		if len(r.Links) == 0 {
			r.Links = nil
		}
	}
	if embedded := properties[embeddedPropertyName]; len(embedded) > 0 {
		delete(properties, embeddedPropertyName)
		if err := json.Unmarshal(embedded, &r.Embedded); err != nil {
			return fmt.Errorf("unmarshal hal resource: %s: %v", embeddedPropertyName, err)
		}
		if len(r.Embedded) == 0 {
			r.Embedded = nil
		}
	}
	if len(properties) > 0 {
		r.Properties = properties
	} else {
		r.Properties = nil
	}
	return nil
}

// ArrayOrObject is either a T or an array of T.
// The zero value is an empty array.
type ArrayOrObject[T any] struct {
	Objects []T
	Single  bool
}

// Object returns a new [ArrayOrObject] that is a single object.
func Object[T any](x T) ArrayOrObject[T] {
	return ArrayOrObject[T]{
		Objects: []T{x},
		Single:  true,
	}
}

// Array returns a [ArrayOrObject] wrapping the given slice.
func Array[T any](x []T) ArrayOrObject[T] {
	return ArrayOrObject[T]{Objects: x}
}

// Get extracts the object from arr
// if and only if arr.Single and len(arr.Objects) == 1.
func (arr ArrayOrObject[T]) Get() (_ T, ok bool) {
	if len(arr.Objects) != 1 || !arr.Single {
		var zero T
		return zero, false
	}
	return arr.Objects[0], true
}

// MarshalJSON marshals arr as JSON.
func (arr ArrayOrObject[T]) MarshalJSON() ([]byte, error) {
	if obj, ok := arr.Get(); ok {
		return json.Marshal(obj)
	}
	return json.Marshal(arr.Objects)
}

// UnmarshalJSON unmarshals arrays into arr.
// Any other type of JSON value is treated as a single type T.
func (arr *ArrayOrObject[T]) UnmarshalJSON(data []byte) error {
	arr.Single = len(data) == 0 || data[0] != '['
	if arr.Single {
		arr.Objects = make([]T, 1)
		return json.Unmarshal(data, &arr.Objects[0])
	}
	return json.Unmarshal(data, &arr.Objects)
}

// Link represents a HAL [link object].
// The only required field is HRef.
//
// [link object]: https://datatracker.ietf.org/doc/html/draft-kelly-json-hal-11#name-link-objects
type Link struct {
	// HRef is either a URI or a [URI template]
	// based on the value of Templated.
	//
	// [URI template]: https://datatracker.ietf.org/doc/html/rfc6570
	HRef string `json:"href"`
	// If Templated is true, the value of HRef is a URI template.
	// Otherwise, HRef's value is a URI.
	Templated bool `json:"templated,omitzero"`

	// Title is an optional human-readable label for the link.
	Title string `json:"title,omitempty"`
	// Type is a hint to indicate the media type expected
	// when dereferencing the target resource.
	Type string `json:"type,omitempty"`
	// If Deprecation is not empty, the link is to be deprecated (i.e. removed) at a future date.
	// Its value is a URL that should provide further information about the deprecation.
	Deprecation string `json:"deprecation,omitempty"`
	// Name may be used as a secondary key
	// for selecting link objects which share the same relation type.
	Name string `json:"name,omitempty"`
	// Profile is a URI that hints about the profile of the target resource.
	Profile string `json:"profile,omitempty"`
	// ResourceLanguage indicates the language of the target resource.
	ResourceLanguage string `json:"hreflang,omitempty"`
}

// Expand expands the link's URI with the given template parameters.
// If the link is not templated, then the template parameters are ignored.
func (l *Link) Expand(data any) (*url.URL, error) {
	href := l.HRef
	if l.Templated {
		var err error
		href, err = uritemplate.Expand(href, data)
		if err != nil {
			return nil, err
		}
	}
	return url.Parse(href)
}
