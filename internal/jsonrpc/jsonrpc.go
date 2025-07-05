// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package jsonrpc provides a stream-based implementation of the JSON-RPC 2.0 specification,
// inspired by the Language Server Protocol (LSP) framing format.
package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"zb.256lights.llc/pkg/internal/jsonstring"
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
	Params json.RawMessage
	// Notification is true if the client does not care about a response.
	Notification bool
	// Extra holds a map of additional top-level fields on the request object.
	Extra map[string]json.RawMessage
}

// Response represents a parsed [JSON-RPC response].
//
// [JSON-RPC response]: https://www.jsonrpc.org/specification#response_object
type Response struct {
	// Result is the result of invoking the method.
	// This may be any JSON.
	Result json.RawMessage
	// Extra holds a map of additional top-level fields on the response object.
	Extra map[string]json.RawMessage
}

// Do makes a single JSON-RPC to the given [Handler].
// request should be any Go value that can be passed to [json.Marshal].
// If request is nil, then the parameters are omitted.
// response should be any Go value (usually a pointer) that can be passed to [json.Unmarshal].
// If response is the nil interface, then any result data is ignored
// (but Do will still wait for the call to complete).
func Do(ctx context.Context, h Handler, method string, response, request any) error {
	var params json.RawMessage
	if request != nil {
		var err error
		params, err = json.Marshal(request)
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
	if err := json.Unmarshal(fullResponse.Result, response); err != nil {
		return fmt.Errorf("call json rpc %s: %v", method, err)
	}
	return nil
}

// Notify makes a single JSON-RPC to the given [Handler].
// params should be any Go value that can be passed to [json.Marshal].
func Notify(ctx context.Context, h Handler, method string, params any) error {
	rawParams, err := json.Marshal(params)
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
	switch id.typ {
	case 0:
		return []byte("null"), nil
	case 1:
		return strconv.AppendInt(nil, id.n, 10), nil
	case 2:
		return jsonstring.Append(nil, id.s), nil
	default:
		return nil, fmt.Errorf("invalid request id type %d (internal error)", id.typ)
	}
}

// UnmarshalJSON parses the request ID.
// UnmarshalJSON returns an error if the data is not null, a string, or an integer.
func (id *RequestID) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("empty request id json")
	}
	switch {
	case string(data) == "null":
		*id = RequestID{}
		return nil
	case data[0] == '"':
		*id = RequestID{typ: 2}
		return json.Unmarshal(data, &id.s)
	default:
		*id = RequestID{typ: 1}
		var err error
		id.n, err = strconv.ParseInt(string(data), 10, 64)
		return err
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
