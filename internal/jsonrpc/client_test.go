// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package jsonrpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"github.com/google/go-cmp/cmp"
)

type clientTestWireInteraction struct {
	wantRequests []any
	responses    []jsontext.Value
}

func TestClient(t *testing.T) {
	type clientCall struct {
		// waitUntil is the index of an interaction to wait for.
		waitUntil int

		request       *Request
		wantResponse  *Response
		wantError     bool
		wantErrorCode ErrorCode
	}

	tests := []struct {
		name  string
		calls []clientCall
		wire  []clientTestWireInteraction
	}{
		{
			name: "ImmediateDisconnect",
		},
		{
			name: "PositionalParams",
			calls: []clientCall{
				{
					request: &Request{
						Method: "subtract",
						Params: jsontext.Value(`[42, 23]`),
					},
					wantResponse: &Response{
						Result: jsontext.Value(`19`),
					},
				},
			},
			wire: []clientTestWireInteraction{
				{
					wantRequests: []any{
						map[string]any{
							"jsonrpc": "2.0",
							"method":  "subtract",
							"params": []any{
								42.0,
								23.0,
							},
							"id": "1",
						},
					},
					responses: []jsontext.Value{
						jsontext.Value(`{"jsonrpc": "2.0", "result": 19, "id": "1"}`),
					},
				},
			},
		},
		{
			name: "Notification",
			calls: []clientCall{
				{
					request: &Request{
						Method:       "update",
						Params:       jsontext.Value(`[1,2,3,4,5]`),
						Notification: true,
					},
					wantResponse: nil,
				},
			},
			wire: []clientTestWireInteraction{
				{
					wantRequests: []any{
						map[string]any{
							"jsonrpc": "2.0",
							"method":  "update",
							"params": []any{
								1.0,
								2.0,
								3.0,
								4.0,
								5.0,
							},
						},
					},
					responses: []jsontext.Value{},
				},
			},
		},
		{
			name: "ErrorResponse",
			calls: []clientCall{
				{
					request: &Request{
						Method: "foobar",
					},
					wantResponse:  nil,
					wantError:     true,
					wantErrorCode: -32601,
				},
			},
			wire: []clientTestWireInteraction{
				{
					wantRequests: []any{
						map[string]any{
							"jsonrpc": "2.0",
							"method":  "foobar",
							"id":      "1",
						},
					},
					responses: []jsontext.Value{
						jsontext.Value(`{"jsonrpc": "2.0", "error": {"code": -32601, "message": "Method not found"}, "id": "1"}`),
					},
				},
			},
		},
		{
			name: "WrongParamsType",
			calls: []clientCall{
				{
					request: &Request{
						Method: "foobar",
						Params: jsontext.Value(`42`),
					},
					wantResponse:  nil,
					wantError:     true,
					wantErrorCode: -32600,
				},
			},
		},
		{
			name: "MultipleCalls",
			calls: []clientCall{
				{
					request: &Request{
						Method: "subtract",
						Params: jsontext.Value(`[42, 23]`),
					},
					wantResponse: &Response{
						Result: jsontext.Value(`19`),
					},
				},
				{
					waitUntil: 1,
					request: &Request{
						Method: "subtract",
						Params: jsontext.Value(`[23, 42]`),
					},
					wantResponse: &Response{
						Result: jsontext.Value(`-19`),
					},
				},
			},
			wire: []clientTestWireInteraction{
				{
					wantRequests: []any{
						map[string]any{
							"jsonrpc": "2.0",
							"method":  "subtract",
							"params": []any{
								42.0,
								23.0,
							},
							"id": "1",
						},
					},
				},
				{
					wantRequests: []any{
						map[string]any{
							"jsonrpc": "2.0",
							"method":  "subtract",
							"params": []any{
								23.0,
								42.0,
							},
							"id": "2",
						},
					},
					responses: []jsontext.Value{
						jsontext.Value(`{"jsonrpc": "2.0", "result": -19, "id": "2"}`),
						jsontext.Value(`{"jsonrpc": "2.0", "result": 19, "id": "1"}`),
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			openCount := 0

			codec := newTestClientCodec(t, test.wire)
			client := NewClient(func(ctx context.Context) (ClientCodec, error) {
				openCount++
				if openCount > 1 {
					t.Errorf("OpenFunc called %d times", openCount)
					return nil, fmt.Errorf("open called %d times", openCount)
				}
				return codec, nil
			})
			defer func() {
				if err := client.Close(); err != nil {
					t.Error("Close:", err)
				}
			}()

			var wg sync.WaitGroup
			wg.Add(len(test.calls))
			for i, call := range test.calls {
				i, call := i, call
				go func() {
					defer wg.Done()
					codec.waitUntil(call.waitUntil)
					got, err := client.JSONRPC(ctx, call.request)
					if err != nil {
						t.Logf("call[%d] error: %v", i, err)
						if !call.wantError {
							t.Fail()
						} else if got, _ := CodeFromError(err); got != call.wantErrorCode {
							t.Errorf("call[%d] error code = %d; want %d", i, got, call.wantErrorCode)
						}
					} else if call.wantError {
						t.Errorf("call[%d] did not return an error", i)
					}
					if diff := cmp.Diff(call.wantResponse, got, parseRawJSON()); diff != "" {
						t.Errorf("call[%d] response (-want +got):\n%s", i, diff)
					}
				}()
			}
			wg.Wait()
		})
	}
}

func TestClientCancel(t *testing.T) {
	ctx := context.Background()
	if d, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, d)
		defer cancel()
	}
	openCount := 0

	codec := newTestClientCodec(t, []clientTestWireInteraction{
		{
			wantRequests: []any{
				map[string]any{
					"jsonrpc": "2.0",
					"method":  "hang",
					"id":      "1",
				},
				map[string]any{
					"jsonrpc": "2.0",
					"method":  "$/cancelRequest",
					"params": map[string]any{
						"id": "1",
					},
				},
			},
			responses: []jsontext.Value{
				jsontext.Value(`{"jsonrpc": "2.0", "result": 123, "id": "1"}`),
			},
		},
	})
	client := NewClient(func(ctx context.Context) (ClientCodec, error) {
		openCount++
		if openCount > 1 {
			t.Errorf("OpenFunc called %d times", openCount)
			return nil, fmt.Errorf("open called %d times", openCount)
		}
		return codec, nil
	})
	defer func() {
		if err := client.Close(); err != nil {
			t.Error("Close:", err)
		}
	}()

	callCtx, cancelCall := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancelCall()
	got, err := client.JSONRPC(callCtx, &Request{
		Method: "hang",
	})
	if !errors.Is(err, context.DeadlineExceeded) || got != nil {
		t.Errorf("client.JSONRPC(...) = %v, %v; want <nil>, %v", got, err, context.DeadlineExceeded)
	} else {
		t.Log("error (expected):", err)
	}
}

func TestClientCodec(t *testing.T) {
	ctx := context.Background()
	openCount := 0

	codec := newTestClientCodec(t, []clientTestWireInteraction{
		{
			wantRequests: []any{
				map[string]any{
					"jsonrpc": "2.0",
					"method":  "foobar",
				},
			},
		},
	})
	client := NewClient(func(ctx context.Context) (ClientCodec, error) {
		openCount++
		if openCount > 1 {
			t.Errorf("OpenFunc called %d times", openCount)
			return nil, fmt.Errorf("open called %d times", openCount)
		}
		return codec, nil
	})
	defer func() {
		if err := client.Close(); err != nil {
			t.Error("Close:", err)
		}
	}()

	got, releaseCodec, err := client.Codec(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseCodec()

	if got != codec {
		t.Errorf("client.Codec(ctx) = %#v; want %#v", got, codec)
	}
	if err := got.WriteRequest(jsontext.Value(`{"jsonrpc": "2.0", "method": "foobar"}`)); err != nil {
		t.Error("WriteRequest:", err)
	}
	codec.waitUntil(1)
}

type testClientCodec struct {
	tb testing.TB

	mu              sync.Mutex
	closed          bool
	interactions    []clientTestWireInteraction
	currInteraction int
	interactionCond sync.Cond
	requestIndex    int

	responsesCond sync.Cond
	responses     []jsontext.Value
}

func newTestClientCodec(tb testing.TB, interactions []clientTestWireInteraction) *testClientCodec {
	c := &testClientCodec{
		tb:           tb,
		interactions: interactions,
	}
	c.responsesCond.L = &c.mu
	c.interactionCond.L = &c.mu
	c.lockedAdvance()
	return c
}

func (c *testClientCodec) waitUntil(interactionIndex int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for c.currInteraction < interactionIndex {
		c.interactionCond.Wait()
	}
}

func (c *testClientCodec) WriteRequest(request jsontext.Value) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return errors.New("WriteRequest on closed connection")
	}

	if c.currInteraction >= len(c.interactions) {
		c.tb.Errorf("Unexpected request: %s", request)
		return nil
	}
	var parsed any
	if err := jsonv2.Unmarshal(request, &parsed); err != nil {
		c.tb.Errorf("Client wrote invalid request: %v", err)
		c.requestIndex++
		c.lockedAdvance()
		return err
	}
	if diff := cmp.Diff(c.interactions[c.currInteraction].wantRequests[c.requestIndex], parsed); diff != "" {
		c.tb.Errorf("client request (-want +got):\n%s", diff)
	}
	c.requestIndex++
	c.lockedAdvance()
	return nil
}

func (c *testClientCodec) lockedAdvance() {
	origLength := len(c.responses)
	origInteraction := c.currInteraction

	for c.currInteraction < len(c.interactions) && c.requestIndex >= len(c.interactions[c.currInteraction].wantRequests) {
		c.responses = append(c.responses, c.interactions[c.currInteraction].responses...)

		c.requestIndex = 0
		c.currInteraction++
	}

	if len(c.responses) > origLength {
		c.responsesCond.Broadcast()
	}
	if c.currInteraction > origInteraction {
		c.interactionCond.Broadcast()
	}
}

func (c *testClientCodec) ReadResponse() (jsontext.Value, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.responses) == 0 && !c.closed {
		c.responsesCond.Wait()
	}

	if c.closed {
		return nil, io.EOF
	}
	r := c.responses[0]
	c.responses = c.responses[1:]
	return r, nil
}

func (c *testClientCodec) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		c.tb.Error("testClientCodec.Close called multiple times")
		return errors.New("connection already closed")
	}
	c.closed = true
	c.interactionCond.Broadcast()
	c.responsesCond.Broadcast()
	return nil
}
