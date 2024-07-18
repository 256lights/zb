// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	"zombiezen.com/go/zb/internal/jsonstring"
)

// ServerCodec represents a single connection from a server to a client.
// ReadRequest and WriteResponse must be safe to call concurrently with each other,
// but [Server] guarantees that it will never make multiple concurrent ReadRequest calls
// nor multiple concurrent WriteResponse calls.
type ServerCodec interface {
	ReadRequest() (json.RawMessage, error)
	WriteResponse(response json.RawMessage) error
}

// A type that implements Handler responds to JSON-RPC requests.
// Implementations of JSONRPC must be safe to call from multiple goroutines concurrently.
type Handler interface {
	JSONRPC(ctx context.Context, req *Request) (*Response, error)
}

// HandlerFunc is a function that implements [Handler].
type HandlerFunc func(ctx context.Context, req *Request) (*Response, error)

// JSONRPC calls f.
func (f HandlerFunc) JSONRPC(ctx context.Context, req *Request) (*Response, error) {
	return f(ctx, req)
}

type server struct {
	mu    sync.Mutex
	codec ServerCodec
}

// Serve serves JSON-RPC requests for a connection.
// Serve will read requests from the codec until ReadRequest returns an error,
// which Serve will return once all requests have completed.
func Serve(ctx context.Context, codec ServerCodec, handler Handler) error {
	srv := &server{codec: codec}
	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		content, err := codec.ReadRequest()
		if err != nil {
			return err
		}

		// TODO(someday): Support batches.
		var parsed rawRequest
		if err := json.Unmarshal(content, &parsed); err != nil {
			err = Error(ParseError, err)
			srv.writeError(err)
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.single(ctx, handler, parsed)
		}()
	}
}

func (srv *server) single(ctx context.Context, handler Handler, raw rawRequest) {
	id, req, err := raw.toRequest()
	if err != nil {
		srv.writeError(err)
		return
	}

	// Make copies of Server fields so we can't race later.
	resp, err := handler.JSONRPC(ctx, req)
	if req.Notification {
		// Notifications do not receive a response.
		return
	}

	buf := new(bytes.Buffer)
	if err != nil {
		marshalError(buf, id, err)
	} else {
		buf.WriteString(`{"jsonrpc":"2.0","id":`)
		idJSON, err := id.MarshalJSON()
		if err != nil {
			panic(err)
		}
		buf.Write(idJSON)
		buf.WriteString(`,"result":`)
		if resp == nil || len(resp.Result) == 0 {
			buf.WriteString("null")
		} else {
			buf.Write(resp.Result)
		}
		if resp != nil {
			for k, v := range resp.Extra {
				if !isReservedResponseField(k) {
					buf.WriteString(",")
					buf.Write(jsonstring.Append(nil, k))
					buf.WriteString(":")
					buf.Write(v)
				}
			}
		}
		buf.WriteString("}")
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	srv.codec.WriteResponse(json.RawMessage(buf.Bytes()))
}

func (srv *server) writeError(err error) {
	buf := new(bytes.Buffer)
	marshalError(buf, requestID{}, err)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	srv.codec.WriteResponse(json.RawMessage(buf.Bytes()))
}

// ServeMux is a mapping of method names to JSON-RPC handlers.
type ServeMux map[string]Handler

// JSONRPC calls the handler that corresponds to the request's method
// or returns a [MethodNotFound] error if no such handler is present.
func (mux ServeMux) JSONRPC(ctx context.Context, req *Request) (*Response, error) {
	h := mux[req.Method]
	if h == nil {
		return nil, Error(MethodNotFound, fmt.Errorf("method %s not found", req.Method))
	}
	return h.JSONRPC(ctx, req)
}

type rawRequest map[string]json.RawMessage

func (req rawRequest) toRequest() (requestID, *Request, error) {
	if err := req.checkVersion(); err != nil {
		return requestID{}, nil, err
	}
	req2 := new(Request)
	var err error
	req2.Method, err = req.method()
	if err != nil {
		return requestID{}, nil, err
	}
	req2.Params = req.params()
	id, idPresent, err := req.id()
	if err != nil {
		return requestID{}, nil, err
	}
	req2.Notification = !idPresent
	req2.Extra = inverseFilterMap(req, isReservedRequestField)

	return id, req2, nil
}

func (req rawRequest) checkVersion() error {
	version := req["jsonrpc"]
	if len(version) == 0 {
		return Error(InvalidRequest, fmt.Errorf("jsonrpc version missing in request"))
	}
	var s string
	if err := json.Unmarshal(version, &s); err != nil {
		return Error(InvalidRequest, fmt.Errorf("jsonrpc version: %v", err))
	}
	if s != "2.0" {
		return Error(InvalidRequest, fmt.Errorf("jsonrpc version %q not supported", s))
	}
	return nil
}

func (req rawRequest) method() (string, error) {
	var s string
	err := json.Unmarshal(req["method"], &s)
	if err != nil {
		err = Error(InvalidRequest, fmt.Errorf("jsonrpc method: %v", err))
	}
	return s, err
}

func (req rawRequest) id() (id requestID, present bool, err error) {
	raw := req["id"]
	if len(raw) == 0 {
		return requestID{}, false, nil
	}
	err = id.UnmarshalJSON(raw)
	if err != nil {
		return requestID{}, false, Error(InvalidRequest, fmt.Errorf("jsonrpc id: %v", err))
	}
	return id, true, nil
}

func (req rawRequest) params() json.RawMessage {
	return req["params"]
}

func marshalError(buf *bytes.Buffer, id requestID, err error) {
	code, ok := CodeFromError(err)
	if !ok {
		code = UnknownErrorCode
	}

	buf.WriteString(`{"jsonrpc":"2.0","id":`)
	idJSON, idError := id.MarshalJSON()
	if idError != nil {
		panic(err)
	}
	buf.Write(idJSON)
	buf.WriteString(`,"error":{"code":`)
	buf.Write(strconv.AppendInt(nil, int64(code), 10))
	buf.WriteString(`,"message":`)
	buf.Write(jsonstring.Append(nil, err.Error()))
	buf.WriteString("}}")
}
