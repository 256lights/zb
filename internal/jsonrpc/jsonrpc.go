// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package jsonrpc provides a stream-based implementation of the JSON-RPC 2.0 specification,
// inspired by the Language Server Protocol (LSP) framing format.
package jsonrpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// Request represents a parsed [JSON-RPC request].
//
// [JSON-RPC request]: https://www.jsonrpc.org/specification#request_object
type Request struct {
	// Method is the name of the method to be invoked.
	Method string
	// Params is the raw JSON of the parameters.
	// If len(Params) == 0, then the parameters are omitted on the wire.
	// Otherwise, Params must hold a valid JSON array or a valid JSON object.
	Params jsontext.Value
	// Notification is true if the client does not care about a response.
	Notification bool
	// Extra holds a map of additional top-level fields on the request object.
	Extra map[string]jsontext.Value
}

// Response represents a parsed [JSON-RPC response].
//
// [JSON-RPC response]: https://www.jsonrpc.org/specification#response_object
type Response struct {
	// Result is the result of invoking the method.
	// This may be any JSON.
	Result jsontext.Value
	// Extra holds a map of additional top-level fields on the response object.
	Extra map[string]jsontext.Value
}

// Do makes a single JSON-RPC to the given [Handler].
// request should be any Go value that can be passed to [jsonv2.Marshal].
// If request is nil, then the parameters are omitted.
// response should be any Go value (usually a pointer) that can be passed to [jsonv2.Unmarshal].
// If response is the nil interface, then any result data is ignored
// (but Do will still wait for the call to complete).
func Do(ctx context.Context, h Handler, method string, response, request any) error {
	var params jsontext.Value
	if request != nil {
		var err error
		params, err = jsonv2.Marshal(request)
		if err != nil {
			return fmt.Errorf("call json rpc %s: %v", method, err)
		}
	}
	fullResponse, err := h.JSONRPC(ctx, &Request{
		Method: method,
		Params: params,
	})
	if err != nil {
		return err
	}
	if len(fullResponse.Result) == 0 || response == nil {
		return nil
	}
	if err := jsonv2.Unmarshal(fullResponse.Result, response); err != nil {
		return fmt.Errorf("call json rpc %s: %v", method, err)
	}
	return nil
}

// Notify makes a single JSON-RPC to the given [Handler].
// params should be any Go value that can be passed to [jsonv2.Marshal].
func Notify(ctx context.Context, h Handler, method string, params any) error {
	rawParams, err := jsonv2.Marshal(params)
	if err != nil {
		return fmt.Errorf("call json rpc %s: %v", method, err)
	}
	_, err = h.JSONRPC(ctx, &Request{
		Method:       method,
		Params:       rawParams,
		Notification: true,
	})
	return err
}

// ErrorCode is a number that indicates the type of error
// that occurred during a JSON-RPC.
type ErrorCode int

// Error codes defined in JSON-RPC 2.0.
const (
	// ParseError indicates that invalid JSON was received by the server.
	ParseError ErrorCode = -32700
	// InvalidRequest indicates that the JSON sent is not a valid request object.
	InvalidRequest ErrorCode = -32600
	// MethodNotFound indicates that the requested method does not exist or is not available.
	MethodNotFound ErrorCode = -32601
	// InvalidParams indicates that the request's method parameters are invalid.
	InvalidParams ErrorCode = -32602
	// InternalError indicates an internal JSON-RPC error.
	InternalError ErrorCode = -32603
)

// Language Server Protocol error codes.
const (
	UnknownErrorCode ErrorCode = -32001
	RequestCancelled ErrorCode = -32800
)

// CodeFromError returns the error's [ErrorCode],
// if one has been assigned using [Error].
//
// As a special case, if there is a [context.Canceled] or [context.DeadlineExceeded] error
// in the error's Unwrap() chain,
// then CodeFromError returns [RequestCancelled].
func CodeFromError(err error) (_ ErrorCode, ok bool) {
	if err == nil {
		return 0, false
	}
	if e := (*codeError)(nil); errors.As(err, &e) {
		return e.code, true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return RequestCancelled, true
	}
	return 0, false
}

type codeError struct {
	code ErrorCode
	err  error
}

// Error returns a new error that wraps the given error
// and will return the given code from [CodeFromError].
// Error panics if it is given a nil error.
func Error(code ErrorCode, err error) error {
	if err == nil {
		panic("jsonrpc.Error called with nil error")
	}
	return &codeError{code, err}
}

func (e *codeError) Error() string { return e.err.Error() }
func (e *codeError) Unwrap() error { return e.err }

// RequestID is an opaque JSON-RPC request ID.
// IDs can be integers, strings, or null
// (although nulls are discouraged).
// The zero value is null.
type RequestID struct {
	n   int64
	s   string
	typ int8
}

// String returns the request ID's string content if it is a string,
// or the request ID's JSON representation otherwise.
func (id RequestID) String() string {
	switch id.typ {
	case 0:
		return "null"
	case 1:
		return strconv.FormatInt(id.n, 10)
	case 2:
		return id.s
	default:
		return "<invalid request id>"
	}
}

// Int64 returns the request ID as an integer.
func (id RequestID) Int64() (_ int64, ok bool) {
	return id.n, id.typ == 1
}

// IsNull reports whether the request ID is null.
func (id RequestID) IsNull() bool {
	return id.typ == 0
}

// IsInteger reports whether the request ID is an integer.
func (id RequestID) IsInteger() bool {
	return id.typ == 1
}

// IsString reports whether the request ID is a string.
func (id RequestID) IsString() bool {
	return id.typ == 2
}

// MarshalJSON returns the request ID's JSON representation.
func (id RequestID) MarshalJSON() ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := jsontext.NewEncoder(buf)
	if err := id.MarshalJSONTo(enc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MarshalJSONTo writes the request ID's JSON representation to the given encoder.
func (id RequestID) MarshalJSONTo(enc *jsontext.Encoder) error {
	switch id.typ {
	case 0:
		return enc.WriteToken(jsontext.Null)
	case 1:
		return enc.WriteToken(jsontext.Int(id.n))
	case 2:
		return enc.WriteToken(jsontext.String(id.s))
	default:
		return fmt.Errorf("invalid request id type %d (internal error)", id.typ)
	}
}

// UnmarshalJSON parses the request ID.
// UnmarshalJSON returns an error if the data is not null, a string, or an integer.
func (id *RequestID) UnmarshalJSON(data []byte) error {
	buf := bytes.NewBuffer(data)
	dec := jsontext.NewDecoder(buf)
	return id.UnmarshalJSONFrom(dec)
}

// UnmarshalJSONFrom reads the request ID from the decoder.
// UnmarshalJSONFrom returns an error if the data is not null, a string, or an integer.
func (id *RequestID) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	switch kind := dec.PeekKind(); kind {
	case 'n':
		*id = RequestID{}
		return nil
	case '"':
		*id = RequestID{typ: 2}
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}
		id.s = tok.String()
		return nil
	case '0':
		*id = RequestID{typ: 1}
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}
		id.n, err = strconv.ParseInt(tok.String(), 10, 64)
		return err
	default:
		return fmt.Errorf("request id must be null, a string, or a number (got %v)", kind)
	}
}

// cancelMethod is the reserved method name for canceling a request.
// https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#cancelRequest
const cancelMethod = "$/cancelRequest"

// cancelParams is the parameter object for a cancellation request.
// https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#cancelRequest
type cancelParams struct {
	ID RequestID `json:"id"`
}

func marshalJSONRPCVersionTo(enc *jsontext.Encoder) error {
	if err := enc.WriteToken(jsontext.String("jsonrpc")); err != nil {
		return fmt.Errorf("marshal json-rpc version: %v", err)
	}
	if err := enc.WriteToken(jsontext.String("2.0")); err != nil {
		return fmt.Errorf("marshal json-rpc version: %v", err)
	}
	return nil
}

func jsonValueFromBuffer(buf *bytes.Buffer) jsontext.Value {
	return jsontext.Value(bytes.TrimSuffix(buf.Bytes(), []byte{'\n'}))
}

// inverseFilterMap returns a new map that contains all the keys
// for which f(k) reports false.
func inverseFilterMap[K comparable, V any, M ~map[K]V](m M, f func(K) bool) map[K]V {
	n := 0
	for k := range m {
		if !f(k) {
			n++
		}
	}
	if n == 0 {
		return nil
	}
	result := make(map[K]V, n)
	for k, v := range m {
		if !f(k) {
			result[k] = v
		}
	}
	return result
}

func isReservedRequestField(key string) bool {
	return key == "jsonrpc" || key == "method" || key == "params" || key == "id"
}

func isReservedResponseField(key string) bool {
	return key == "jsonrpc" || key == "result" || key == "error" || key == "id"
}
