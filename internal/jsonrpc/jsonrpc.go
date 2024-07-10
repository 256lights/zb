// Copyright 2024 Ross Light
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

	"zombiezen.com/go/zb/internal/jsonstring"
)

// Request represents a parsed [JSON-RPC request].
//
// [JSON-RPC request]: https://www.jsonrpc.org/specification#request_object
type Request struct {
	// Method is the name of the method to be invoked.
	Method string
	// Params is the raw JSON of the parameters.
	// If len(Params) == 0, then the parameters are omitted on the wire.
	// Otherwise, Params must hold a valid JSON array or valid JSON object.
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

// ErrorCode is a number that indicates the type of error
// that occurred during a JSON-RPC.
type ErrorCode int

// Error codes defined in JSON-RPC 2.0.
const (
	ParseError     ErrorCode = -32700
	InvalidRequest ErrorCode = -32600
	MethodNotFound ErrorCode = -32601
	InvalidParams  ErrorCode = -32602
	InternalError  ErrorCode = -32603
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

// requestID is an opaque JSON-RPC request ID.
// IDs can be integers, strings, or null
// (although nulls are discouraged).
// The zero value is null.
type requestID struct {
	n   int64
	s   string
	typ int8
}

func (id requestID) String() string {
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

func (id requestID) MarshalJSON() ([]byte, error) {
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

func (id *requestID) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("empty request id json")
	}
	switch {
	case string(data) == "null":
		*id = requestID{}
		return nil
	case data[0] == '"':
		*id = requestID{typ: 2}
		return json.Unmarshal(data, &id.s)
	default:
		*id = requestID{typ: 1}
		var err error
		id.n, err = strconv.ParseInt(string(data), 10, 64)
		return err
	}
}
