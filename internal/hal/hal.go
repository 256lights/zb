// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

// Package hal provides data types for the [JSON Hypertext Application Language] (HAL).
//
// [JSON Hypertext Application Language]: https://datatracker.ietf.org/doc/html/draft-kelly-json-hal-11
package hal

import (
	"fmt"
	"maps"
	"net/url"
	"slices"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"zombiezen.com/go/uritemplate"
)

// MediaType is the MIME media type of a HAL document.
const MediaType = "application/hal+json"

// Well-known link relation types.
// See https://www.iana.org/assignments/link-relations/link-relations.xhtml
// for a complete list of registered link relations.
const (
	SelfRelationType     = "self"
	PreviousRelationType = "prev"
	NextRelationType     = "next"
	FirstRelationType    = "first"
	LastRelationType     = "last"
	UpRelationType       = "up"
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
	Properties map[string]jsontext.Value
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
	return jsonv2.Marshal(r, jsonv2.Deterministic(true))
}

// MarshalJSONTo writes the resource to the given JSON encoder.
func (r *Resource) MarshalJSONTo(enc *jsontext.Encoder) error {
	if r == nil {
		if err := enc.WriteToken(jsontext.Null); err != nil {
			return fmt.Errorf("marshal hal resource: %w", err)
		}
	}

	for _, name := range [...]string{linksPropertyName, embeddedPropertyName} {
		if _, ok := r.Properties[name]; ok {
			return fmt.Errorf("marshal hal resource: %s property is reserved", name)
		}
	}

	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return fmt.Errorf("marshal hal resource: %w", err)
	}
	if len(r.Links) > 0 {
		if err := enc.WriteToken(jsontext.String(linksPropertyName)); err != nil {
			return fmt.Errorf("marshal hal resource: %s: %w", linksPropertyName, err)
		}
		if err := jsonv2.MarshalEncode(enc, r.Links); err != nil {
			return fmt.Errorf("marshal hal resource: %s: %w", linksPropertyName, err)
		}
	}
	if len(r.Embedded) > 0 {
		if err := enc.WriteToken(jsontext.String(embeddedPropertyName)); err != nil {
			return fmt.Errorf("marshal hal resource: %w", err)
		}
		if err := jsonv2.MarshalEncode(enc, r.Embedded); err != nil {
			return fmt.Errorf("marshal hal resource: %s: %w", embeddedPropertyName, err)
		}
	}
	if len(r.Properties) > 0 {
		keys := maps.Keys(r.Properties)
		if deterministic, _ := jsonv2.GetOption(enc.Options(), jsonv2.Deterministic); deterministic {
			keys = slices.Values(slices.Sorted(keys))
		}
		for k := range keys {
			if err := enc.WriteToken(jsontext.String(k)); err != nil {
				return fmt.Errorf("marshal hal resource: %s: %w", k, err)
			}
			if err := enc.WriteValue(jsontext.Value(r.Properties[k])); err != nil {
				return fmt.Errorf("marshal hal resource: %s: %w", k, err)
			}
		}
	}
	if err := enc.WriteToken(jsontext.EndObject); err != nil {
		return fmt.Errorf("marshal hal resource: %w", err)
	}
	return nil
}

// UnmarshalJSON unmarshals a JSON object into the resource.
func (r *Resource) UnmarshalJSON(data []byte) error {
	return jsonv2.Unmarshal(data, r)
}

// UnmarshalJSON unmarshals a JSON object into the resource.
func (r *Resource) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if tok, err := dec.ReadToken(); err != nil {
		return fmt.Errorf("unmarshal hal resource: %w", err)
	} else if got := tok.Kind(); got != '{' {
		return fmt.Errorf("unmarshal hal resource: unexpected %v token (want object)", got)
	}

	for {
		keyToken, err := dec.ReadToken()
		if err != nil {
			return fmt.Errorf("unmarshal hal resource: %w", err)
		}
		if keyToken.Kind() == '}' {
			break
		}
		key := keyToken.String()

		switch key {
		case linksPropertyName:
			if err := jsonv2.UnmarshalDecode(dec, &r.Links); err != nil {
				return fmt.Errorf("unmarshal hal resource: %s: %w", key, err)
			}
		case embeddedPropertyName:
			if err := jsonv2.UnmarshalDecode(dec, &r.Embedded); err != nil {
				return fmt.Errorf("unmarshal hal resource: %s: %w", key, err)
			}
		default:
			if r.Properties == nil {
				r.Properties = make(map[string]jsontext.Value)
			}
			var err error
			r.Properties[key], err = dec.ReadValue()
			if err != nil {
				return fmt.Errorf("unmarshal hal resource: %s: %w", key, err)
			}
		}
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
	return jsonv2.Marshal(arr, jsonv2.Deterministic(true))
}

// MarshalJSONTo writes arr to a JSON encoder.
func (arr ArrayOrObject[T]) MarshalJSONTo(enc *jsontext.Encoder) error {
	if obj, ok := arr.Get(); ok {
		return jsonv2.MarshalEncode(enc, obj)
	}
	return jsonv2.MarshalEncode(enc, arr.Objects)
}

// UnmarshalJSON unmarshals arrays into arr.
// Any other type of JSON value is treated as a single type T.
func (arr *ArrayOrObject[T]) UnmarshalJSON(data []byte) error {
	return jsonv2.Unmarshal(data, arr)
}

// UnmarshalJSONFrom unmarshals the next value from a JSON decoder into arr.
// Arrays are parsed as an array of T.
// Any other type of JSON value is treated as a single type T.
func (arr *ArrayOrObject[T]) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	arr.Single = dec.PeekKind() != '['
	if arr.Single {
		arr.Objects = make([]T, 1)
		return jsonv2.UnmarshalDecode(dec, &arr.Objects[0])
	}
	return jsonv2.UnmarshalDecode(dec, &arr.Objects)
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
