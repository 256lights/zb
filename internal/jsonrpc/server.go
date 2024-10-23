// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	"zb.256lights.llc/pkg/internal/jsonstring"
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
//
// The jsonrpc package provides [Client], [ServeMux], and [HandlerFunc]
// as implementations of Handler.
type Handler interface {
	JSONRPC(ctx context.Context, req *Request) (*Response, error)
}

// HandlerFunc is a function that implements [Handler].
type HandlerFunc func(ctx context.Context, req *Request) (*Response, error)

// JSONRPC calls f.
func (f HandlerFunc) JSONRPC(ctx context.Context, req *Request) (*Response, error) {
	return f(ctx, req)
}

// MethodNotFoundHandler implements [Handler]
// by returning a [MethodNotFound] error for all requests.
type MethodNotFoundHandler struct{}

// JSONRPC returns an error for which [ErrorCode] returns [MethodNotFound].
func (MethodNotFoundHandler) JSONRPC(ctx context.Context, req *Request) (*Response, error) {
	return nil, Error(MethodNotFound, fmt.Errorf("method %q not found", req.Method))
}

type server struct {
	writeLock sync.Mutex
	codec     ServerCodec

	mu        sync.Mutex
	cancelMap map[requestID]context.CancelFunc
}

// Serve serves JSON-RPC requests for a connection.
// Serve will read requests from the codec until ReadRequest returns an error,
// which Serve will return once all requests have completed.
func Serve(ctx context.Context, codec ServerCodec, handler Handler) error {
	srv := &server{
		codec:     codec,
		cancelMap: make(map[requestID]context.CancelFunc),
	}
	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		content, err := codec.ReadRequest()
		if err != nil {
			return err
		}

		// TODO(someday): Support batches.
		parsed := new(serverRequest)
		if err := parsed.UnmarshalJSON(content); err != nil {
			srv.writeError(err)
			continue
		}

		requestCtx, cancel := context.WithCancel(ctx)
		if !parsed.Notification {
			srv.mu.Lock()
			srv.cancelMap[parsed.id] = cancel
			srv.mu.Unlock()
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.single(requestCtx, handler, parsed, cancel)
		}()
	}
}

func (srv *server) single(ctx context.Context, handler Handler, req *serverRequest, cancel context.CancelFunc) {
	defer cancel()
	// Make defensive copy of request information.
	notification := req.Notification

	var resp *Response
	var err error
	switch req.Method {
	case cancelMethod:
		resp, err = srv.cancel(&req.Request)
	default:
		resp, err = handler.JSONRPC(ctx, &req.Request)
	}
	cancel()

	if notification {
		// Notifications do not receive a response.
		return
	}

	srv.mu.Lock()
	delete(srv.cancelMap, req.id)
	srv.mu.Unlock()

	buf := new(bytes.Buffer)
	if err != nil {
		marshalError(buf, req.id, err)
	} else {
		buf.WriteString(`{"jsonrpc":"2.0","id":`)
		idJSON, err := req.id.MarshalJSON()
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

	srv.writeLock.Lock()
	defer srv.writeLock.Unlock()
	srv.codec.WriteResponse(json.RawMessage(buf.Bytes()))
}

// cancel handles a [cancelMethod] request.
func (srv *server) cancel(req *Request) (*Response, error) {
	var args cancelParams
	if err := json.Unmarshal(req.Params, &args); err != nil {
		return nil, Error(InvalidParams, err)
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	if cancel := srv.cancelMap[args.ID]; cancel != nil {
		cancel()
	}
	delete(srv.cancelMap, args.ID)
	return nil, nil
}

func (srv *server) writeError(err error) {
	buf := new(bytes.Buffer)
	marshalError(buf, requestID{}, err)

	srv.writeLock.Lock()
	defer srv.writeLock.Unlock()
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

type serverRequest struct {
	id requestID
	Request
}

func (req *serverRequest) UnmarshalJSON(data []byte) error {
	raw := make(map[string]json.RawMessage)
	if err := json.Unmarshal(data, &raw); err != nil {
		return Error(ParseError, err)
	}

	version := raw["jsonrpc"]
	if len(version) == 0 {
		return Error(InvalidRequest, fmt.Errorf("jsonrpc version missing in request"))
	}
	var versionString string
	if err := json.Unmarshal(version, &versionString); err != nil {
		return Error(InvalidRequest, fmt.Errorf("jsonrpc version: %v", err))
	}
	if versionString != "2.0" {
		return Error(InvalidRequest, fmt.Errorf("jsonrpc version %q not supported", versionString))
	}

	err := json.Unmarshal(raw["method"], &req.Method)
	if err != nil {
		return Error(InvalidRequest, fmt.Errorf("jsonrpc method: %v", err))
	}

	req.Params = raw["params"]

	rawID := raw["id"]
	req.Notification = len(rawID) == 0
	if req.Notification {
		req.id = requestID{}
	} else {
		err = req.id.UnmarshalJSON(rawID)
		if err != nil {
			return Error(InvalidRequest, fmt.Errorf("jsonrpc id: %v", err))
		}
	}

	req.Extra = inverseFilterMap(raw, isReservedRequestField)

	return nil
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
