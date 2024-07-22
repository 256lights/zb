// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package zbstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"zombiezen.com/go/zb/internal/jsonrpc"
)

// apiContentType is the MIME media type for the zb store API.
const apiContentType = "application/zb-store-api+json"

// exportContentType is the MIME media type for a `nix-store --export` stream.
const exportContentType = "application/zb-store-export"

const maxAPIMessageSize = 1 << 20 // 1 MiB

// ServerCodec implements [jsonrpc.ServerCodec] on an [io.ReadWriteCloser]
// using the Language Server Protocol "base protocol" for framing.
type ServerCodec struct {
	c codec
}

// NewServerCodec returns a new [ServerCodec]
// that uses the given connection.
func NewServerCodec(rwc io.ReadWriteCloser, receiver NARReceiver) *ServerCodec {
	sc := new(ServerCodec)
	sc.c.init(rwc, receiver)
	return sc
}

// ReadRequest implements [jsonrpc.ServerCodec].
func (sc *ServerCodec) ReadRequest() (json.RawMessage, error) {
	return sc.c.read()
}

// WriteResponse implements [jsonrpc.ServerCodec].
func (sc *ServerCodec) WriteResponse(response json.RawMessage) error {
	return sc.c.write(response)
}

// Close closes the underlying connection.
func (sc *ServerCodec) Close() error {
	return sc.c.Close()
}

// ClientCodec implements [jsonrpc.ClientCodec] on an [io.ReadWriteCloser]
// using the Language Server Protocol "base protocol" for framing.
type ClientCodec struct {
	c codec
}

// NewClientCodec returns a new [ClientCodec]
// that uses the given connection.
func NewClientCodec(rwc io.ReadWriteCloser) *ClientCodec {
	cc := new(ClientCodec)
	cc.c.init(rwc, nil)
	return cc
}

// WriteRequest implements [jsonrpc.ClientCodec].
func (cc *ClientCodec) WriteRequest(request json.RawMessage) error {
	return cc.c.write(request)
}

// Export sends a `nix-store --export` dump.
func (cc *ClientCodec) Export(r io.Reader) error {
	return cc.c.export(r)
}

// ReadResponse implements [jsonrpc.ClientCodec].
func (cc *ClientCodec) ReadResponse() (json.RawMessage, error) {
	return cc.c.read()
}

// Close closes the underlying connection.
func (cc *ClientCodec) Close() error {
	return cc.c.Close()
}

type codec struct {
	w *jsonrpc.Writer
	c io.Closer

	apiMessages <-chan json.RawMessage
	readDone    <-chan struct{}
	readError   error // can only be read after apiMessages is closed
}

func (c *codec) init(rwc io.ReadWriteCloser, receiver NARReceiver) {
	if receiver == nil {
		receiver = nopReceiver{}
	}
	apiMessages := make(chan json.RawMessage)
	readDone := make(chan struct{})
	*c = codec{
		w:           jsonrpc.NewWriter(rwc),
		c:           rwc,
		apiMessages: apiMessages,
		readDone:    readDone,
	}
	go func() {
		defer func() {
			close(apiMessages)
			close(readDone)
		}()
		c.readError = readLoop(apiMessages, receiver, jsonrpc.NewReader(rwc))
	}()
}

func (c *codec) read() (json.RawMessage, error) {
	msg, ok := <-c.apiMessages
	if !ok {
		return nil, c.readError
	}
	return msg, nil
}

func readLoop(apiMessages chan<- json.RawMessage, receiver NARReceiver, r *jsonrpc.Reader) error {
	for {
		header, bodySize, err := r.NextMessage()
		if err != nil {
			return err
		}
		switch ct := header.Get("Content-Type"); ct {
		case apiContentType:
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
			apiMessages <- body
		case exportContentType:
			err := receiveExport(receiver, r)
			if err != nil && (bodySize < 0 || errors.As(err, new(recvError))) {
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

func (c *codec) write(msg json.RawMessage) error {
	hdr := jsonrpc.Header{
		"Content-Length": {strconv.Itoa(len(msg))},
		"Content-Type":   {apiContentType},
	}
	return c.w.WriteMessage(hdr, bytes.NewReader(msg))
}

func (c *codec) export(r io.Reader) error {
	hdr := jsonrpc.Header{
		"Content-Type": {exportContentType},
	}
	return c.w.WriteMessage(hdr, r)
}

func (c *codec) Close() error {
	err := c.c.Close()
	<-c.readDone
	return err
}
