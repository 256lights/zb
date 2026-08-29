// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package zbstorerpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
	"weak"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zombiezen.com/go/log"
)

// Server is the interface used by [Serve] to handle zb JSON-RPC messages.
type Server interface {
	jsonrpc.Handler
	Importer
}

// Importer wraps the StoreImport method from [zbstore.Importer].
type Importer interface {
	StoreImport(ctx context.Context, r io.Reader) error
}

// Serve serves zb JSON-RPC requests for a connection.
// Serve will read requests from the [io.ReadWriteCloser] until Read returns an error.
// When the [context.Context]'s Done() channel is closed,
// Serve will attempt to shut down the reading side of the connection to trigger an error.
//
// Calling [ContextImporter] on the [context.Context] that Serve sends to srv.JSONRPC
// will return an [Importer] that writes an export message to rwc.
//
// Serve will always close the [io.ReadWriteCloser] before returning.
func Serve(ctx context.Context, rwc io.ReadWriteCloser, srv Server) error {
	if f, ok := closeReadFunc(rwc); ok {
		readClosed := make(chan struct{})
		stop := context.AfterFunc(ctx, func() {
			defer close(readClosed)
			f()
		})
		defer func() {
			if !stop() {
				<-readClosed
			}
		}()
	}

	c := newServerCodec(ctx, rwc, srv)
	serveError := jsonrpc.Serve(ctx, c, jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
		if idJSON := req.Extra[exportIDExtraFieldName]; len(idJSON) > 0 {
			var id string
			if err := jsonv2.Unmarshal(idJSON, &id); err != nil {
				return nil, jsonrpc.Error(jsonrpc.InvalidParams, fmt.Errorf("%s: %v", exportIDExtraFieldName, err))
			}
			ctx = withRequestExportID(ctx, c, id)
		}
		ctx = WithImporter(ctx, c)
		return srv.JSONRPC(ctx, req)
	}))
	closeError := rwc.Close()
	return errors.Join(serveError, closeError)
}

// exportIDExtraFieldName is the name of the extra field in [jsonrpc.Request]
// used to pass a value that will be passed through with [exportIDHeaderName].
const exportIDExtraFieldName = "zbExportID"

type requestExportIDContextKey struct {
	codec weak.Pointer[serverCodec]
}

func withRequestExportID(parent context.Context, c *serverCodec, id string) context.Context {
	return context.WithValue(parent, requestExportIDContextKey{weak.Make(c)}, id)
}

func contextRequestExportID(ctx context.Context, c *serverCodec) (id string, ok bool) {
	v := ctx.Value(requestExportIDContextKey{weak.Make(c)})
	if v == nil {
		return "", false
	}
	return v.(string), true
}

type importerContextKey struct{}

// WithImporter returns a copy of parent
// in which an [Importer] is used to send back export information in a [jsonrpc.Handler].
//
// [Serve] automatically calls WithImporter.
// WithImporter is exported for testing purposes.
func WithImporter(parent context.Context, i Importer) context.Context {
	return context.WithValue(parent, importerContextKey{}, i)
}

// ContextImporter returns an [Importer] for the [context.Context].
// For contexts that are not derived from [WithImporter],
// ContextImporter returns an [Importer] that discards data it receives.
func ContextImporter(ctx context.Context) Importer {
	v := ctx.Value(importerContextKey{})
	if v == nil {
		return nopImporter{}
	}
	return v.(Importer)
}

// serverCodec implements [jsonrpc.ServerCodec] on an [io.ReadWriter]
// using the Language Server Protocol "base protocol" for framing.
type serverCodec struct {
	writeLock sync.Mutex
	w         *jsonrpc.Writer

	messages  <-chan jsontext.Value
	readError error // can only be read after messages is closed
	readDone  <-chan struct{}
}

func newServerCodec(ctx context.Context, rw io.ReadWriter, importer Importer) *serverCodec {
	c := new(serverCodec)
	messages := make(chan jsontext.Value)
	readDone := make(chan struct{})
	*c = serverCodec{
		w:        jsonrpc.NewWriter(rw),
		messages: messages,
		readDone: readDone,
	}
	go func() {
		defer func() {
			close(messages)
			close(readDone)
		}()
		c.readError = serverReadLoop(ctx, messages, importer, jsonrpc.NewReader(rw))
	}()
	return c
}

func (sc *serverCodec) ReadRequest() (jsontext.Value, error) {
	msg, ok := <-sc.messages
	if !ok {
		return nil, sc.readError
	}
	return msg, nil
}

func serverReadLoop(ctx context.Context, messages chan<- jsontext.Value, importer Importer, r *jsonrpc.Reader) error {
	for {
		header, bodySize, err := r.NextMessage()
		if err != nil {
			return err
		}
		switch ct := header.Get("Content-Type"); ct {
		case rpcContentType:
			if bodySize < 0 {
				return fmt.Errorf("remote sent api message without valid Content-Length")
			}
			if bodySize > maxAPIMessageSize {
				return fmt.Errorf("remote sent large api message (%d bytes)", maxAPIMessageSize)
			}
			body, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			messages <- body
		case exportContentType:
			if err := importer.StoreImport(ctx, r); err != nil {
				err = fmt.Errorf("while receiving export: %v", err)
				if bodySize < 0 {
					return err
				}
				log.Warnf(ctx, "%v", err)
			}
		default:
			// Ignore, if possible.
			if bodySize < 0 {
				return fmt.Errorf("remote sent unknown Content-Type %q without valid Content-Length", ct)
			}
		}
	}
}

func (sc *serverCodec) WriteResponse(response jsontext.Value) error {
	sc.writeLock.Lock()
	defer sc.writeLock.Unlock()
	return writeRPCMessage(sc.w, response)
}

func (sc *serverCodec) StoreImport(ctx context.Context, r io.Reader) error {
	id, ok := contextRequestExportID(ctx, sc)
	if !ok {
		id = ""
	}
	sc.writeLock.Lock()
	defer sc.writeLock.Unlock()
	return writeExport(ctx, sc.w, id, r)
}

func closeReadFunc(r io.Reader) (f func() error, ok bool) {
	if cr, ok := r.(interface{ CloseRead() error }); ok {
		return cr.CloseRead, true
	} else if rd, ok := r.(interface{ SetReadDeadline(time.Time) error }); ok {
		return func() error {
			return rd.SetReadDeadline(time.Now())
		}, true
	}
	return func() error { return nil }, false
}
