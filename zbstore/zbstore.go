// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package zbstore provides the data types for the zb store API.
package zbstore

import (
	"encoding/base64"
	"encoding/json"
	"iter"
	"unicode/utf8"

	"zombiezen.com/go/nix"
)

// ExistsMethod is the name of the method that checks whether a store path exists.
// [ExistsRequest] is used for the request and the response is a boolean.
const ExistsMethod = "zb.exists"

// ExistsRequest is the set of parameters for [ExistsMethod].
type ExistsRequest struct {
	Path string `json:"path"`
}

// InfoMethod is the name of the method that returns information about a store object.
// [InfoRequest] is used for the request
// and [InfoResponse] is used for the response.
const InfoMethod = "zb.info"

// InfoRequest is the set of parameters for [InfoMethod].
type InfoRequest struct {
	Path Path `json:"path"`
}

// InfoResponse is the result for [InfoMethod].
type InfoResponse struct {
	// Info is the information for the requested path,
	// or null if the path does not exist.
	Info *ObjectInfo `json:"info"`
}

// ObjectInfo is a condensed version of [NARInfo] used in [InfoResponse].
type ObjectInfo struct {
	// NARHash is the hash of the decompressed .nar file.
	// Nix requires this field to be set.
	NARHash nix.Hash `json:"narHash"`
	// NARSize is the size of the decompressed .nar file in bytes.
	// Nix requires this field to be set.
	NARSize int64 `json:"narSize"`
	// References is the set of other store objects that this store object references.
	References []Path `json:"references"`
	// CA is a content-addressability assertion.
	CA ContentAddress `json:"ca"`
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

// OutputsByName returns an iterator over the outputs with the given name.
func (resp *RealizeResponse) OutputsByName(name string) iter.Seq[*RealizeOutput] {
	if resp == nil {
		return func(yield func(*RealizeOutput) bool) {}
	}
	return func(yield func(*RealizeOutput) bool) {
		for _, out := range resp.Outputs {
			if out.Name == name {
				if !yield(out) {
					return
				}
			}
		}
	}
}

// RealizeOutput is an output in [RealizeResponse].
type RealizeOutput struct {
	// OutputName is the name of the output that was built (e.g. "out" or "dev").
	Name string `json:"name"`
	// Path is the store path of the output if successfully built,
	// or null if the build failed.
	Path Nullable[Path] `json:"path"`
}

// ExpandMethod is the name of the method that performs placeholder expansion
// on a derivation.
// This will cause its dependencies to be realized,
// but the derivation itself will not.
// [ExpandRequest] is used for the request
// and [ExpandResponse] is used for the response.
const ExpandMethod = "zb.expand"

// ExpandRequest is the set of parameters for [ExpandMethod].
type ExpandRequest struct {
	DrvPath            Path   `json:"drvPath"`
	TemporaryDirectory string `json:"tempDir"`
}

// ExpandResponse is the result for [ExpandMethod].
type ExpandResponse struct {
	Builder string            `json:"builder"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// LogMethod is the name of the method invoked on the client
// to record a log message from a running invocation.
// [LogNotification] is used for the request
// and the response is ignored.
const LogMethod = "zb.log"

// LogNotification is the set of parameters for [LogMethod].
// One of Text or Base64 should be set to a non-empty string.
type LogNotification struct {
	DrvPath Path   `json:"drvPath"`
	Text    string `json:"text,omitempty"`
	Base64  string `json:"base64,omitempty"`
}

// Payload returns the log's byte content.
func (notif *LogNotification) Payload() []byte {
	switch {
	case notif.Base64 != "":
		b, _ := base64.StdEncoding.DecodeString(notif.Base64)
		return b
	case notif.Text != "":
		return []byte(notif.Text)
	default:
		return nil
	}
}

// SetPayload sets notif.Text and notif.Base64 to reflect the given payload.
func (notif *LogNotification) SetPayload(src []byte) {
	if utf8.Valid(src) {
		notif.Text = string(src)
		notif.Base64 = ""
	} else {
		notif.Text = ""
		notif.Base64 = base64.StdEncoding.EncodeToString(src)
	}
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
