// Copyright 2024 Ross Light
// SPDX-License-Identifier: MIT

package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestServe(t *testing.T) {
	tests := []struct {
		name      string
		requests  []json.RawMessage
		responses []any

		ignoreErrorMessages bool
	}{
		{
			name: "EOF",
		},
		{
			name: "Positional",
			requests: []json.RawMessage{
				json.RawMessage(`{"jsonrpc": "2.0", "method": "subtract", "params": [42, 23], "id": 1}`),
				json.RawMessage(`{"jsonrpc": "2.0", "method": "subtract", "params": [23, 42], "id": 2}`),
			},
			responses: []any{
				map[string]any{
					"jsonrpc": "2.0",
					"result":  json.Number("19"),
					"id":      json.Number("1"),
				},
				map[string]any{
					"jsonrpc": "2.0",
					"result":  json.Number("-19"),
					"id":      json.Number("2"),
				},
			},
		},
		{
			name: "NonExistentMethod",
			requests: []json.RawMessage{
				json.RawMessage(`{"jsonrpc": "2.0", "method": "foobar", "id": "1"}`),
			},
			responses: []any{
				map[string]any{
					"jsonrpc": "2.0",
					"error": map[string]any{
						"code":    json.Number("-32601"),
						"message": "unknown method \"foobar\"",
					},
					"id": "1",
				},
			},
		},
		{
			name: "NonExistentNotification",
			requests: []json.RawMessage{
				json.RawMessage(`{"jsonrpc": "2.0", "method": "foobar"}`),
			},
			responses: []any{},
		},
		{
			name: "InvalidJSON",
			requests: []json.RawMessage{
				json.RawMessage(`{"jsonrpc": "2.0", "method": "foobar, "params": "bar", "baz]`),
			},
			responses: []any{
				map[string]any{
					"jsonrpc": "2.0",
					"error": map[string]any{
						"code": json.Number("-32700"),
					},
					"id": nil,
				},
			},
			ignoreErrorMessages: true,
		},
		{
			name: "InvalidRequest",
			requests: []json.RawMessage{
				json.RawMessage(`{"jsonrpc": "2.0", "method": 1, "params": "bar"}`),
			},
			responses: []any{
				map[string]any{
					"jsonrpc": "2.0",
					"error": map[string]any{
						"code": json.Number("-32600"),
					},
					"id": nil,
				},
			},
			ignoreErrorMessages: true,
		},
	}

	handler := HandlerFunc(func(ctx context.Context, req *Request) (*Response, error) {
		switch req.Method {
		case "subtract":
			var args []int64
			if err := json.Unmarshal(req.Params, &args); err != nil {
				return nil, Error(InvalidRequest, fmt.Errorf("params: %v", err))
			}
			if len(args) == 0 {
				return &Response{
					Result: json.RawMessage("0"),
				}, nil
			}
			result := args[0]
			for _, arg := range args[1:] {
				result -= arg
			}
			return &Response{
				Result: json.RawMessage(strconv.AppendInt(nil, result, 10)),
			}, nil
		default:
			return nil, Error(MethodNotFound, fmt.Errorf("unknown method %q", req.Method))
		}
	})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			codec := &testServerCodec{requests: test.requests}
			Serve(ctx, codec, handler)

			codec.mu.Lock()
			got := slices.Clone(codec.responses)
			codec.mu.Unlock()

			compareJSONValue := func(a, b any) int {
				// Marshaling per-comparison is super-expensive,
				// but we're just in test and it's easy to reason about.
				aJSON, _ := json.Marshal(a)
				bJSON, _ := json.Marshal(b)
				return bytes.Compare(aJSON, bJSON)
			}
			slices.SortFunc(got, compareJSONValue)
			want := slices.Clone(test.responses)
			slices.SortFunc(want, compareJSONValue)

			options := cmp.Options{
				cmpopts.EquateEmpty(),
			}
			if test.ignoreErrorMessages {
				options = append(options, cmp.FilterPath(
					func(p cmp.Path) bool {
						// Path index 0: Root of path.
						// Path index 1: Slice into []any.
						if p.Index(2).Type() != mapStringAnyType {
							return false
						}
						if idx, ok := p.Index(3).(cmp.MapIndex); !ok || idx.Key().Interface() != "error" || idx.Type() != anyType {
							return false
						}
						if assert, ok := p.Index(4).(cmp.TypeAssertion); !ok || assert.Type() != mapStringAnyType {
							return false
						}
						if idx, ok := p.Index(5).(cmp.MapIndex); !ok || idx.Key().Interface() != "message" {
							return false
						}
						return true
					},
					cmp.Ignore(),
				))
			}

			if diff := cmp.Diff(test.responses, got, options); diff != "" {
				t.Errorf("server responses (-want +got):\n%s", diff)
			}
		})
	}
}

var (
	anyType          = reflect.TypeOf((*any)(nil)).Elem()
	mapStringAnyType = reflect.TypeOf((*map[string]any)(nil)).Elem()
)

type testServerCodec struct {
	mu        sync.Mutex
	requests  []json.RawMessage
	responses []any
}

func (c *testServerCodec) ReadRequest() (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.requests) == 0 {
		return nil, io.EOF
	}
	req := c.requests[0]
	c.requests = c.requests[1:]
	return req, nil
}

func (c *testServerCodec) WriteResponse(response json.RawMessage) error {
	dec := json.NewDecoder(bytes.NewReader(response))
	dec.UseNumber()
	var parsed any
	if err := dec.Decode(&parsed); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.responses = append(c.responses, parsed)
	return nil
}
