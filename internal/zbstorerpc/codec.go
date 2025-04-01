// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package zbstorerpc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/zbstore"
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

	messages  <-chan json.RawMessage
	readError error // can only be read after messages is closed
	readDone  <-chan struct{}
}

// NewCodec returns a new [Codec] that uses the given connection.
// receiver may be nil.
func NewCodec(rwc io.ReadWriteCloser, receiver zbstore.NARReceiver) *Codec {
	if receiver == nil {
		receiver = nopReceiver{}
	}

	c := new(Codec)
	messages := make(chan json.RawMessage)
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
		c.readError = readLoop(messages, receiver, jsonrpc.NewReader(rwc))
	}()
	return c
}

// ReadRequest implements [jsonrpc.ServerCodec].
func (c *Codec) ReadRequest() (json.RawMessage, error) {
	return c.ReadResponse()
}

// ReadResponse implements [jsonrpc.ClientCodec].
func (c *Codec) ReadResponse() (json.RawMessage, error) {
	msg, ok := <-c.messages
	if !ok {
		return nil, c.readError
	}
	return msg, nil
}

func readLoop(messages chan<- json.RawMessage, receiver zbstore.NARReceiver, r *jsonrpc.Reader) error {
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
			err := zbstore.ReceiveExport(receiver, r)
			if err != nil && (bodySize < 0 || zbstore.IsReceiverError(err)) {
				return fmt.Errorf("while receiving export: %w", err)
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
func (c *Codec) WriteRequest(request json.RawMessage) error {
	hdr := jsonrpc.Header{
		"Content-Length": {strconv.Itoa(len(request))},
		"Content-Type":   {rpcContentType},
	}
	return c.w.WriteMessage(hdr, bytes.NewReader(request))
}

// WriteResponse implements [jsonrpc.ServerCodec].
func (c *Codec) WriteResponse(response json.RawMessage) error {
	return c.WriteRequest(response)
}

// Export sends a `nix-store --export` dump.
func (c *Codec) Export(r io.Reader) error {
	hdr := jsonrpc.Header{
		"Content-Type": {exportContentType},
	}
	return c.w.WriteMessage(hdr, r)
}

// Close closes the underlying connection.
func (c *Codec) Close() error {
	err := c.c.Close()
	<-c.readDone
	return err
}

type nopReceiver struct{}

func (nopReceiver) Write(p []byte) (n int, err error)         { return len(p), nil }
func (nopReceiver) ReceiveNAR(trailer *zbstore.ExportTrailer) {}
