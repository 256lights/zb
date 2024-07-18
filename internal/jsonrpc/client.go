// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"zombiezen.com/go/log"
	"zombiezen.com/go/xcontext"
	"zombiezen.com/go/zb/internal/jsonstring"
)

// ClientCodec represents a single connection from a client to a server.
// WriteRequest and ReadResponse must be safe to call concurrently with each other,
// but [Client] guarantees that it will never make multiple concurrent WriteRequest calls
// nor multiple concurrent ReadResponse calls.
//
// Close is called when the Client no longer intends to use the connection.
// Close may be called concurrently with WriteRequest or ReadResponse:
// doing so should interrupt either call and cause the call to return an error.
type ClientCodec interface {
	RequestWriter
	ReadResponse() (json.RawMessage, error)
	Close() error
}

// RequestWriter holds the WriteRequest method of [ClientCodec].
type RequestWriter interface {
	WriteRequest(request json.RawMessage) error
}

// OpenFunc opens a connection for a [Client].
type OpenFunc func(ctx context.Context) (ClientCodec, error)

// A Client represents a JSON-RPC client
// that automatically reconnects after I/O errors.
// Methods on Client are safe to call from multiple goroutines concurrently.
type Client struct {
	// comms is a channel of RPCs to send to the server
	// with optional responses.
	comms chan clientRequest
	// cancelComms stops the communicate method.
	cancelComms context.CancelFunc
	// commsDone is closed once the communicate method returns.
	commsDone chan struct{}
	// codecRequests is a channel for requests of the codec.
	codecRequests chan clientCodecRequest
}

// NewClient returns a new [Client] that opens connections using the given function.
// The caller is responsible for calling [Client.Close]
// when the Client is no longer in use.
//
// NewClient will start opening a connection in the background,
// but will return before the connection is established.
// The first call to [Client.JSONRPC] will block on the connection.
func NewClient(open OpenFunc) *Client {
	c := &Client{
		comms:         make(chan clientRequest),
		commsDone:     make(chan struct{}),
		codecRequests: make(chan clientCodecRequest),
	}
	var commsCtx context.Context
	commsCtx, c.cancelComms = context.WithCancel(context.Background())
	go func() {
		defer close(c.commsDone)
		c.communicate(commsCtx, open)
	}()
	return c
}

// Close closes the client connection.
func (c *Client) Close() error {
	c.cancelComms()
	<-c.commsDone
	return nil
}

// JSONRPC sends a request to the server.
func (c *Client) JSONRPC(ctx context.Context, req *Request) (*Response, error) {
	if !isValidParamStruct(req.Params) {
		return nil, Error(InvalidRequest, fmt.Errorf("call json rpc %s: params must be an object or an array", req.Method))
	}

	if req.Notification {
		write := make(chan error, 1)
		creq := clientRequest{
			Request: req,
			write:   write,
		}
		select {
		case c.comms <- creq:
			select {
			case err := <-write:
				if err != nil {
					return nil, fmt.Errorf("call json rpc %s: %w", req.Method, err)
				}
				return nil, nil
			case <-ctx.Done():
				return nil, fmt.Errorf("call json rpc %s: %w", req.Method, ctx.Err())
			}
		case <-ctx.Done():
			return nil, fmt.Errorf("call json rpc %s: %w", req.Method, ctx.Err())
		}
	}

	responseChan := make(chan rawResponse, 1)
	creq := clientRequest{
		Request:  req,
		response: responseChan,
	}
	select {
	case c.comms <- creq:
	case <-ctx.Done():
		return nil, fmt.Errorf("call json rpc %s: %w", req.Method, ctx.Err())
	}
	select {
	case raw := <-responseChan:
		if raw == nil {
			// Disconnected before response received.
			return nil, fmt.Errorf("call json rpc %s: connection interrupted", req.Method)
		}

		resp, err := raw.toResponse()
		if err != nil {
			return resp, fmt.Errorf("call json rpc %s: %w", req.Method, err)
		}
		return resp, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("call json rpc %s: %w", req.Method, ctx.Err())
	}
}

func (c *Client) communicate(ctx context.Context, open OpenFunc) {
	for {
		if ctx.Err() != nil {
			return
		}

		log.Debugf(ctx, "Opening new JSON-RPC connection...")
		conn, err := open(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				log.Warnf(ctx, "Failed to open JSON-RPC connection (will retry): %v", err)
				t := time.NewTimer(1 * time.Second)
				select {
				case <-t.C:
				case <-ctx.Done():
					t.Stop()
					return
				}
			}

			continue
		}

		c.handleConn(ctx, conn, xcontext.CloseWhenDone(ctx, conn))
	}
}

func (c *Client) handleConn(ctx context.Context, conn ClientCodec, closer io.Closer) {
	// Read messages in a separate goroutine,
	// so we can select on them down below.
	messages := make(chan json.RawMessage)
	go func() {
		defer close(messages)
		for {
			msg, err := conn.ReadResponse()
			if err != nil {
				log.Debugf(ctx, "Failed to read JSON-RPC response: %v", err)
				return
			}
			messages <- msg
		}
	}()

	responseChans := make(map[int64]chan<- rawResponse)

	defer func() {
		log.Debugf(ctx, "Shutting down JSON-RPC connection")
		closer.Close()
		// Inform any pending application calls that the response will never come.
		for id, c := range responseChans {
			log.Debugf(ctx, "Response for %v dropped", id)
			c <- nil
		}
		responseChans = nil
		// Drain the reader goroutine.
		for range messages {
		}
	}()

	nextID := int64(1)
	buf := new(bytes.Buffer)
	for {
		select {
		case msg := <-messages:
			if msg == nil {
				// Connection fault.
				return
			}
			dispatchResponse(ctx, msg, responseChans)
		case req := <-c.comms:
			// Handle incoming application requests.

			buf.Reset()
			buf.WriteString(`{"jsonrpc":"2.0","method":`)
			buf.Write(jsonstring.Append(nil, req.Method))
			if req.Notification {
				log.Debugf(ctx, "Writing %s JSON-RPC notification", req.Method)
			} else {
				id := nextID
				nextID++
				if req.response != nil {
					responseChans[id] = req.response
				}
				buf.WriteString(`,"id":`)
				clientID := marshalClientID(nil, id)
				buf.Write(clientID)
				log.Debugf(ctx, "Writing %s JSON-RPC with id=%s", req.Method, clientID)
			}
			if len(req.Params) > 0 {
				buf.WriteString(`,"params":`)
				buf.Write(req.Params)
			}
			buf.WriteString("}")

			err := conn.WriteRequest(json.RawMessage(buf.Bytes()))
			if req.write != nil {
				req.write <- err
			}
			if err != nil {
				log.Debugf(ctx, "Failed to send message: %v", err)
				return
			}
		case r := <-c.codecRequests:
			r.codec <- conn
		appUse:
			for {
				// We intentionally do not listen for ctx.Done() in this loop
				// because we want holding the codec to delay client shutdown.
				// Responses can continue to be handled.
				select {
				case <-r.release:
					break appUse
				case msg := <-messages:
					if msg == nil {
						// Connection fault.
						return
					}
					dispatchResponse(ctx, msg, responseChans)
				}
			}
		case <-ctx.Done():
			// If the application is closing the client,
			// end the loop and close the connection (via the defer).
			return
		}
	}
}

// dispatchResponse sends a server response (possibly a batch)
// to the corresponding listener(s).
func dispatchResponse(ctx context.Context, msg json.RawMessage, responseChans map[int64]chan<- rawResponse) {
	batch, err := unmarshalResponseBatch(msg)
	if err != nil {
		log.Warnf(ctx, "JSON-RPC server returned invalid JSON: %v", err)
		return
	}
	for _, resp := range batch {
		// We specifically don't check anything beyond ID here
		// because we want most errors to be returned to the application.
		id, err := resp.id()
		if err != nil {
			continue
		}
		idText, ok := id.toString()
		if !ok {
			// We only make string IDs.
			continue
		}
		idNum, ok := unmarshalClientID(idText)
		if !ok {
			// We only make *numeric* string IDs.
			continue
		}
		c := responseChans[idNum]
		if c == nil {
			continue
		}
		c <- resp
		delete(responseChans, idNum)
	}
}

// Codec obtains the client's currently active codec,
// waiting for a connection to be established if necessary.
// The caller is responsible for calling the release function
// when it is no longer using the codec:
// all new calls to [Client.JSONRPC] will block
// to avoid concurrent message writes.
// After the first call to the release function,
// subsequent calls are no-ops.
// Client will continue to read responses from the codec
// while the caller is holding onto the codec.
//
// Codec guarantees that if it succeeds,
// it returns a value that was returned by the [OpenFunc] provided to [NewClient].
// However, applications should not call either ReadResponse or Close on the returned codec.
func (c *Client) Codec(ctx context.Context) (codec RequestWriter, release func(), err error) {
	// Buffer not strictly necessary,
	// but allows communication goroutine to go back to reading messages quickly.
	codecChan := make(chan RequestWriter, 1)
	releaseChan := make(chan struct{})

	select {
	case c.codecRequests <- clientCodecRequest{codecChan, releaseChan}:
	case <-ctx.Done():
		return nil, nil, fmt.Errorf("obtain jsonrpc client connection: %w", err)
	}
	codec = <-codecChan
	var once sync.Once
	return codec, func() {
		once.Do(func() { close(releaseChan) })
	}, nil
}

// A clientRequest is sent from client methods to the connection handler
// to be written on the wire.
type clientRequest struct {
	*Request

	// write is a channel that will receive a notification of the write's result if non-nil.
	// It must have a buffer of at least 1.
	write chan<- error

	// response is the channel that will receive the response message if non-nil.
	// It must have a buffer of at least 1.
	// If the connection is interrupted before a response is recieved,
	// it will receive a nil message.
	response chan<- rawResponse
}

type clientCodecRequest struct {
	codec   chan<- RequestWriter
	release <-chan struct{}
}

type rawResponse map[string]json.RawMessage

func (resp rawResponse) toResponse() (*Response, error) {
	if err := resp.checkVersion(); err != nil {
		return nil, err
	}
	extra := inverseFilterMap(resp, isReservedResponseField)

	switch resultField, errorField := resp["result"], resp["error"]; {
	case len(resultField) > 0 && len(errorField) > 0:
		err := fmt.Errorf("jsonrpc response contains both result and error")
		if len(extra) > 0 {
			return &Response{Extra: extra}, err
		}
		return nil, err
	case len(resultField) > 0:
		return &Response{
			Result: resultField,
			Extra:  extra,
		}, nil
	case len(errorField) > 0:
		var errorObject struct {
			Code    ErrorCode `json:"code"`
			Message string    `json:"message"`
		}
		err := json.Unmarshal(errorField, &errorObject)
		if err != nil {
			err = fmt.Errorf("failed to unmarshal jsonrpc error: %v", err)
		} else if errorObject.Message != "" {
			err = Error(errorObject.Code, errors.New(errorObject.Message))
		} else {
			err = Error(errorObject.Code, fmt.Errorf("jsonrpc error %d", errorObject.Code))
		}
		if len(extra) > 0 {
			return &Response{Extra: extra}, err
		}
		return nil, err
	default:
		err := fmt.Errorf("jsonrpc response does not contain result nor error")
		if len(extra) > 0 {
			return &Response{Extra: extra}, err
		}
		return nil, err
	}
}

func (resp rawResponse) checkVersion() error {
	version := resp["jsonrpc"]
	if len(version) == 0 {
		return fmt.Errorf("jsonrpc version missing in response")
	}
	var s string
	if err := json.Unmarshal(version, &s); err != nil {
		return fmt.Errorf("jsonrpc version: %v", err)
	}
	if s != "2.0" {
		return fmt.Errorf("jsonrpc version %q not supported", s)
	}
	return nil
}

func (resp rawResponse) id() (requestID, error) {
	raw := resp["id"]
	if len(raw) == 0 {
		return requestID{}, fmt.Errorf("jsonrpc response missing id")
	}
	var id requestID
	err := id.UnmarshalJSON(raw)
	if err != nil {
		return requestID{}, fmt.Errorf("jsonrpc response id: %v", err)
	}
	return id, nil
}

// marshalClientID appends a hex-encoded JSON string of an integer ID
// to the given byte slice.
func marshalClientID(dst []byte, id int64) []byte {
	// To avoid any potential problems with JSON number precision,
	// encode the ID as a JSON string.
	dst = slices.Grow(dst, 8*2+len(`"-"`))
	dst = append(dst, '"')
	dst = strconv.AppendInt(dst, id, 16)
	dst = append(dst, '"')
	return dst
}

// unmarshalClientID parses a string back to an integer ID,
// ensuring that it is of the format from [marshalClientID].
func unmarshalClientID(s string) (int64, bool) {
	s2, neg := strings.CutPrefix(s, "-")
	s2, zero := strings.CutPrefix(s2, "0")
	if zero {
		// Zero is only an acceptable prefix if it is the entire string.
		return 0, !neg && s2 == ""
	}
	for _, c := range []byte(s2) {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
			// Not a lowercase hex digit.
			return 0, false
		}
	}

	id, err := strconv.ParseInt(s, 16, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// unmarshalResponseBatch unmarshals either a JSON-RPC response object
// or an array of such objects.
func unmarshalResponseBatch(msg json.RawMessage) ([]rawResponse, error) {
	if len(msg) == 0 || msg[0] != '[' {
		var response rawResponse
		if err := json.Unmarshal(msg, &response); err != nil {
			return nil, err
		}
		return []rawResponse{response}, nil
	}

	// First pass: split apart the array.
	// If one element isn't an object, don't fail the entire batch.
	var array []json.RawMessage
	if err := json.Unmarshal(msg, &array); err != nil {
		return nil, err
	}

	responses := make([]rawResponse, len(array))
	for i, r := range array {
		var rr rawResponse
		if err := json.Unmarshal(r, &rr); err == nil {
			responses[i] = rr
		}
	}
	return responses, nil
}

func isValidParamStruct(msg json.RawMessage) bool {
	if len(msg) == 0 {
		// Omitted is fine.
		return true
	}
	return msg[0] == '{' && msg[len(msg)-1] == '}' ||
		msg[0] == '[' && msg[len(msg)-1] == ']'
}
