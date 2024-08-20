// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

// Package zbstore provides the data types for the zb store API.
package zbstore

import "encoding/json"

// ExistsMethod is the name of the method that checks whether a store path exists.
// [ExistsRequest] is used for the request and the response is a boolean.
const ExistsMethod = "zb.exists"

// ExistsRequest is the set of parameters for [ExistsMethod].
type ExistsRequest struct {
	Path string `json:"path"`
}

// RealizeMethod is the name of the method that triggers a build of a store path.
// [RealizeRequest] is used for the request
// and [RealizeResponse] is used for the response.
const RealizeMethod = "zb.realize"

// RealizeRequest is the set of parameters for [RealizeMethod].
type RealizeRequest struct {
	DrvPath Path `json:"drvPath"`
}

// RealizeResponse is the result for [RealizeMethod].
type RealizeResponse struct {
	Outputs []*RealizeOutput `json:"outputs"`
}

// RealizeOutput is an output in [RealizeResponse].
type RealizeOutput struct {
	// OutputName is the name of the output that was built (e.g. "out" or "dev").
	Name string `json:"name"`
	// Path is the store path of the output if successfully built,
	// or null if the build failed.
	Path Nullable[Path] `json:"path"`
}

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

// MarshalJSON marshals n.X if n.Valid is true.
// Otherwise, MarshalJSON returns null.
func (n Nullable[T]) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.X)
}

// UnmarshalJSON unmarshals the given JSON data into n.X
// unless it receives a JSON null, in which case n is zeroed out.
func (n *Nullable[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" { // Compiler optimizes out allocation.
		*n = Nullable[T]{}
		return nil
	}
	err := json.Unmarshal(data, &n.X)
	n.Valid = err == nil
	return err
}
