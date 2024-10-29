// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package zbstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"zb.256lights.llc/pkg/internal/jsonrpc"
)

const (
	// requestContentType is the MIME media type for zb store API requests.
	requestContentType = "application/zb-store-request+json"
	// responseContentType is the MIME media type for zb store API responses.
	responseContentType = "application/zb-store-response+json"
	// exportContentType is the MIME media type for a `nix-store --export` stream.
	exportContentType = "application/zb-store-export"
)

const maxAPIMessageSize = 1 << 20 // 1 MiB

// Codec implements [jsonrpc.ServerCodec] and [jsonrpc.ClientCodec]
// on an [io.ReadWriteCloser]
// using the Language Server Protocol "base protocol" for framing.
type Codec struct {
	w *jsonrpc.Writer
	c io.Closer

	requestMessages  <-chan json.RawMessage
	responseMessages <-chan json.RawMessage
	readError        error // can only be read after requestMessages and responseMessages or closed
	readDone         <-chan struct{}
}

// NewCodec returns a new [Codec] that uses the given connection.
func NewCodec(rwc io.ReadWriteCloser, receiver NARReceiver) *Codec {
	if receiver == nil {
		receiver = nopReceiver{}
	}

	c := new(Codec)
	requestMessages := make(chan json.RawMessage)
	responseMessages := make(chan json.RawMessage)
	readDone := make(chan struct{})
	*c = Codec{
		w:                jsonrpc.NewWriter(rwc),
		c:                rwc,
		requestMessages:  requestMessages,
		responseMessages: responseMessages,
		readDone:         readDone,
	}
	go func() {
		defer func() {
			close(requestMessages)
			close(responseMessages)
			close(readDone)
		}()
		c.readError = readLoop(requestMessages, responseMessages, receiver, jsonrpc.NewReader(rwc))
	}()
	return c
}

// ReadRequest implements [jsonrpc.ServerCodec].
func (c *Codec) ReadRequest() (json.RawMessage, error) {
	msg, ok := <-c.requestMessages
	if !ok {
		return nil, c.readError
	}
	return msg, nil
}

// ReadResponse implements [jsonrpc.ClientCodec].
func (c *Codec) ReadResponse() (json.RawMessage, error) {
	msg, ok := <-c.responseMessages
	if !ok {
		return nil, c.readError
	}
	return msg, nil
}

func readLoop(requestMessages, responseMessages chan<- json.RawMessage, receiver NARReceiver, r *jsonrpc.Reader) error {
	for {
		header, bodySize, err := r.NextMessage()
		if err != nil {
			return err
		}
		switch ct := header.Get("Content-Type"); ct {
		case requestContentType, responseContentType:
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
			if ct == requestContentType {
				requestMessages <- body
			} else {
				responseMessages <- body
			}
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

// WriteRequest implements [jsonrpc.ClientCodec].
func (c *Codec) WriteRequest(request json.RawMessage) error {
	return c.write(requestContentType, request)
}

// WriteResponse implements [jsonrpc.ServerCodec].
func (c *Codec) WriteResponse(response json.RawMessage) error {
	return c.write(responseContentType, response)
}

func (c *Codec) write(contentType string, msg json.RawMessage) error {
	hdr := jsonrpc.Header{
		"Content-Length": {strconv.Itoa(len(msg))},
		"Content-Type":   {contentType},
	}
	return c.w.WriteMessage(hdr, bytes.NewReader(msg))
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
