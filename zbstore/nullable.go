// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package zbstore

import (
	"fmt"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// Nullable wraps a type to permit a null JSON serialization.
// The zero value is null.
type Nullable[T any] struct {
	X     T
	Valid bool
}

// NonNull returns a [Nullable] that wraps the given value.
func NonNull[T any](x T) Nullable[T] {
	return Nullable[T]{x, true}
}

// String converts n.X to a string or returns "null" if n is not valid.
func (n Nullable[T]) String() string {
	if !n.Valid {
		return "null"
	}
	return fmt.Sprint(n.X)
}

// MarshalJSON marshals n.X if n.Valid is true.
// Otherwise, MarshalJSON returns "null".
func (n Nullable[T]) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return jsonv2.Marshal(n.X)
}

// MarshalJSONTo encodes n.X if n.Valid is true.
// Otherwise, MarshalJSONTo writes a null token.
func (n Nullable[T]) MarshalJSONTo(enc *jsontext.Encoder) error {
	if !n.Valid {
		return enc.WriteToken(jsontext.Null)
	}
	return jsonv2.MarshalEncode(enc, n.X)
}

// UnmarshalJSON unmarshals the given JSON data into n.X
// unless it receives a JSON null, in which case n is zeroed out.
func (n *Nullable[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" { // Compiler optimizes out allocation.
		*n = Nullable[T]{}
		return nil
	}
	err := jsonv2.Unmarshal(data, &n.X)
	n.Valid = err == nil
	return err
}

// UnmarshalJSONFrom unmarshals the next value from the JSON decoder into n.X
// unless it receives a JSON null, in which case n is zeroed out.
func (n *Nullable[T]) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if dec.PeekKind() == 'n' {
		if _, err := dec.ReadToken(); err != nil {
			return err
		}
		*n = Nullable[T]{}
		return nil
	}
	err := jsonv2.UnmarshalDecode(dec, &n.X)
	n.Valid = err == nil
	return err
}
