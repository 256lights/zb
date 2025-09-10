// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package jsonrpc_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"zb.256lights.llc/pkg/internal/jsonrpc"
)

func Example() {
	// Set up a server handler.
	srv := jsonrpc.ServeMux{
		"subtract": jsonrpc.HandlerFunc(subtractHandler),
	}

	// Start a goroutine to serve JSON-RPC on an in-memory pipe.
	srvConn, clientConn := net.Pipe()
	srvDone := make(chan struct{})
	go func() {
		defer close(srvDone)
		defer srvConn.Close()
		jsonrpc.Serve(context.Background(), newCodec(srvConn), srv)
	}()
	defer func() {
		// Wait for server to finish.
		<-srvDone
	}()

	// Create a client that communicates on the in-memory pipe.
	client := jsonrpc.NewClient(func(ctx context.Context) (jsonrpc.ClientCodec, error) {
		return newCodec(clientConn), nil
	})
	defer client.Close()

	// Call the server using the client.
	response, err := client.JSONRPC(context.Background(), &jsonrpc.Request{
		Method: "subtract",
		Params: jsontext.Value(`[42, 23]`),
	})
	if err != nil {
		panic(err)
	}
	var x int64
	if err := jsonv2.Unmarshal(response.Result, &x); err != nil {
		panic(err)
	}
	fmt.Println("Server returned", x)
	// Output:
	// Server returned 19
}

// subtractHandler is a [jsonrpc.HandlerFunc]
// that takes in an array of one or more integers
// and returns the result of subtracting the subsequent integers from the first argument.
func subtractHandler(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	// Parse the arguments into the desired JSON structure.
	var params []int64
	if err := jsonv2.Unmarshal(req.Params, &params); err != nil {
		return nil, err
	}

	// Input validation.
	// We can use jsonrpc.Error to specify the error code to use.
	if len(params) == 0 {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, fmt.Errorf("params must include at least one number"))
	}

	// Perform the calculation.
	result := params[0]
	for _, x := range params[1:] {
		result -= x
	}
	return &jsonrpc.Response{
		Result: jsontext.Value(strconv.FormatInt(result, 10)),
	}, nil
}

// codec is a simple implementation of [jsonrpc.ServerCodec] and [jsonrpc.ClientCodec]
// that reads and writes JSON messages with no framing.
type codec struct {
	enc *jsontext.Encoder
	dec *jsontext.Decoder
	c   io.Closer
}

// newCodec returns a new codec that reads, writes, and closes the given stream.
func newCodec(rwc io.ReadWriteCloser) *codec {
	c := &codec{
		enc: jsontext.NewEncoder(rwc),
		dec: jsontext.NewDecoder(rwc),
		c:   rwc,
	}
	return c
}

// ReadRequest implements [jsonrpc.ServerCodec].
func (c *codec) ReadRequest() (jsontext.Value, error) {
	msg, err := c.dec.ReadValue()
	if err != nil {
		return nil, err
	}
	return msg.Clone(), nil
}

// ReadResponse implements [jsonrpc.ClientCodec].
func (c *codec) ReadResponse() (jsontext.Value, error) {
	msg, err := c.dec.ReadValue()
	if err != nil {
		return nil, err
	}
	return msg.Clone(), nil
}

// WriteRequest implements [jsonrpc.ClientCodec].
func (c *codec) WriteRequest(request jsontext.Value) error {
	return c.enc.WriteValue(request)
}

// WriteResponse implements [jsonrpc.ServerCodec].
func (c *codec) WriteResponse(response jsontext.Value) error {
	return c.enc.WriteValue(response)
}

// Close closes the underlying connection.
// (Part of [jsonrpc.ClientCodec].)
func (c *codec) Close() error {
	return c.c.Close()
}
