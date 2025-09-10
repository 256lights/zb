// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package zbstorerpc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"strconv"
	"sync"

	"github.com/go-json-experiment/json/jsontext"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
)

const (
	// rpcContentType is the MIME media type for zb store API requests.
	rpcContentType = "application/zb-store-rpc+json"
	// exportContentType is the MIME media type for a `nix-store --export` stream.
	exportContentType = "application/zb-store-export"
)

const maxAPIMessageSize = 1 << 20 // 1 MiB

// Codec implements [jsonrpc.ServerCodec] and [jsonrpc.ClientCodec]
// on an [io.ReadWriteCloser]
// using the Language Server Protocol "base protocol" for framing.
// A Codec must only be used as a ServerCodec or as a ClientCodec, not both.
type Codec struct {
	w *jsonrpc.Writer
	c io.Closer

	messages  <-chan jsontext.Value
	readError error // can only be read after messages is closed
	readDone  <-chan struct{}
}

// CodecOptions is the set of optional parameters to [NewCodec].
type CodecOptions struct {
	// If Importer is non-nil, then it is used to handle application/zb-store-export messages.
	// If Importer is nil, such messages are discarded.
	Importer Importer
}

// Importer is the interface used by [Codec] to handle application/zb-store-export messages.
//
// Import is called with the message's header and a reader for the message's body.
// Import is responsible for reading the entirety of the export from body.
// If the export was not fully read or contains invalid data,
// then Import must return an error.
// Import must not retain header or body after it returns.
type Importer interface {
	Import(header jsonrpc.Header, body io.Reader) error
}

// ImportFunc is a function that implements [Importer].
type ImportFunc func(header jsonrpc.Header, body io.Reader) error

// Import implements [Importer] by calling f.
func (f ImportFunc) Import(header jsonrpc.Header, body io.Reader) error {
	return f(header, body)
}

// NewCodec returns a new [Codec] that uses the given connection.
// If opts is nil, it is treated the same as the zero value.
func NewCodec(rwc io.ReadWriteCloser, opts *CodecOptions) *Codec {
	var importer Importer
	if opts != nil && opts.Importer != nil {
		importer = opts.Importer
	} else {
		importer = NewReceiverImporter(nopReceiver{})
	}

	c := new(Codec)
	messages := make(chan jsontext.Value)
	readDone := make(chan struct{})
	*c = Codec{
		w:        jsonrpc.NewWriter(rwc),
		c:        rwc,
		messages: messages,
		readDone: readDone,
	}
	go func() {
		defer func() {
			close(messages)
			close(readDone)
		}()
		c.readError = readLoop(messages, importer, jsonrpc.NewReader(rwc))
	}()
	return c
}

// ReadRequest implements [jsonrpc.ServerCodec].
func (c *Codec) ReadRequest() (jsontext.Value, error) {
	return c.ReadResponse()
}

// ReadResponse implements [jsonrpc.ClientCodec].
func (c *Codec) ReadResponse() (jsontext.Value, error) {
	msg, ok := <-c.messages
	if !ok {
		return nil, c.readError
	}
	return msg, nil
}

func readLoop(messages chan<- jsontext.Value, importer Importer, r *jsonrpc.Reader) error {
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
			if err := importer.Import(header, r); err != nil {
				if bodySize < 0 {
					return fmt.Errorf("while receiving export: %w", err)
				}
				log.Warnf(context.Background(), "While receiving export: %v", err)
			}
		default:
			// Ignore, if possible.
			if bodySize < 0 {
				return fmt.Errorf("remote sent unknown Content-Type %q without valid Content-Length", ct)
			}
		}
	}
}

// WriteRequest implements [jsonrpc.ClientCodec].
func (c *Codec) WriteRequest(request jsontext.Value) error {
	hdr := jsonrpc.Header{
		"Content-Length": {strconv.Itoa(len(request))},
		"Content-Type":   {rpcContentType},
	}
	return c.w.WriteMessage(hdr, bytes.NewReader(request))
}

// WriteResponse implements [jsonrpc.ServerCodec].
func (c *Codec) WriteResponse(response jsontext.Value) error {
	return c.WriteRequest(response)
}

// Export sends a `nix-store --export` dump.
// The Content-Type header is always sent as "application/zb-store-export".
func (c *Codec) Export(header jsonrpc.Header, r io.Reader) error {
	fullHeader := make(jsonrpc.Header, len(header)+1)
	maps.Copy(fullHeader, header)
	fullHeader.Set("Content-Type", exportContentType)
	return c.w.WriteMessage(fullHeader, r)
}

// Close closes the underlying connection.
func (c *Codec) Close() error {
	err := c.c.Close()
	<-c.readDone
	return err
}

// ReceiverImporter adapts a [zbstore.NARReceiver] into a [Importer].
type ReceiverImporter struct {
	receiver zbstore.NARReceiver
}

// NewReceiverImporter returns a new [ReceiverImporter] that sends data to the given receiver.
// NewReceiverImporter panics if receiver is nil.
func NewReceiverImporter(receiver zbstore.NARReceiver) *ReceiverImporter {
	if receiver == nil {
		panic("nil receiver")
	}
	return &ReceiverImporter{receiver}
}

// Import implements [Importer] by calling [zbstore.ReceiveExport] on the given body.
func (imp *ReceiverImporter) Import(header jsonrpc.Header, body io.Reader) error {
	return zbstore.ReceiveExport(imp.receiver, body)
}

// DeferredImporter allows switching out an [Importer].
// The zero value discards any imports received.
type DeferredImporter struct {
	mu  sync.RWMutex
	imp Importer
}

// SetImporter changes the [Importer] that [*DeferredImporter.Import] calls will be dispatched to.
// nil is treated like an [Importer] that reads the body and discards the contents,
// only returning an error if the body is not well-formed `nix-store --export` data.
//
// SetImporter is safe to call concurrently with [*DeferredImporter.Import].
// After SetImporter returns, calls to Import will use the new importer.
func (di *DeferredImporter) SetImporter(imp Importer) {
	di.mu.Lock()
	di.imp = imp
	di.mu.Unlock()
}

// Import calls Import on its underlying [Importer]
// as set by [*DeferredImporter.SetImporter].
func (di *DeferredImporter) Import(header jsonrpc.Header, body io.Reader) error {
	di.mu.RLock()
	imp := di.imp
	di.mu.RUnlock()

	if imp == nil {
		return zbstore.ReceiveExport(nopReceiver{}, body)
	}
	return imp.Import(header, body)
}

type nopReceiver struct{}

func (nopReceiver) Write(p []byte) (n int, err error)         { return len(p), nil }
func (nopReceiver) ReceiveNAR(trailer *zbstore.ExportTrailer) {}
