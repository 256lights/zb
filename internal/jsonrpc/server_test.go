// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package jsonrpc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"testing"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestServe(t *testing.T) {
	tests := []struct {
		name      string
		requests  []jsontext.Value
		responses []any

		ignoreErrorMessages bool
	}{
		{
			name: "EOF",
		},
		{
			name: "Positional",
			requests: []jsontext.Value{
				jsontext.Value(`{"jsonrpc": "2.0", "method": "subtract", "params": [42, 23], "id": 1}`),
				jsontext.Value(`{"jsonrpc": "2.0", "method": "subtract", "params": [23, 42], "id": 2}`),
			},
			responses: []any{
				map[string]any{
					"jsonrpc": "2.0",
					"result":  19.0,
					"id":      1.0,
				},
				map[string]any{
					"jsonrpc": "2.0",
					"result":  -19.0,
					"id":      2.0,
				},
			},
		},
		{
			name: "NonExistentMethod",
			requests: []jsontext.Value{
				jsontext.Value(`{"jsonrpc": "2.0", "method": "foobar", "id": "1"}`),
			},
			responses: []any{
				map[string]any{
					"jsonrpc": "2.0",
					"error": map[string]any{
						"code":    -32601.0,
						"message": "unknown method \"foobar\"",
					},
					"id": "1",
				},
			},
		},
		{
			name: "Cancel",
			requests: []jsontext.Value{
				jsontext.Value(`{"jsonrpc": "2.0", "method": "hang", "id": 1}`),
				jsontext.Value(`{"jsonrpc": "2.0", "method": "$/cancelRequest", "params": {"id": 1}}`),
			},
			responses: []any{
				map[string]any{
					"jsonrpc": "2.0",
					"result":  nil,
					"id":      1.0,
				},
			},
		},
		{
			name: "NonExistentNotification",
			requests: []jsontext.Value{
				jsontext.Value(`{"jsonrpc": "2.0", "method": "foobar"}`),
			},
			responses: []any{},
		},
		{
			name: "InvalidJSON",
			requests: []jsontext.Value{
				jsontext.Value(`{"jsonrpc": "2.0", "method": "foobar, "params": "bar", "baz]`),
			},
			responses: []any{
				map[string]any{
					"jsonrpc": "2.0",
					"error": map[string]any{
						"code": -32700.0,
					},
					"id": nil,
				},
			},
			ignoreErrorMessages: true,
		},
		{
			name: "InvalidRequest",
			requests: []jsontext.Value{
				jsontext.Value(`{"jsonrpc": "2.0", "method": 1, "params": "bar"}`),
			},
			responses: []any{
				map[string]any{
					"jsonrpc": "2.0",
					"error": map[string]any{
						"code": -32600.0,
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
			if err := jsonv2.Unmarshal(req.Params, &args); err != nil {
				return nil, Error(InvalidRequest, fmt.Errorf("params: %v", err))
			}
			if len(args) == 0 {
				return &Response{
					Result: jsontext.Value("0"),
				}, nil
			}
			result := args[0]
			for _, arg := range args[1:] {
				result -= arg
			}
			return &Response{
				Result: jsontext.Value(strconv.AppendInt(nil, result, 10)),
			}, nil
		case "hang":
			<-ctx.Done()
			return nil, nil
		default:
			return nil, Error(MethodNotFound, fmt.Errorf("unknown method %q", req.Method))
		}
	})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if d, ok := t.Deadline(); ok {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, d)
				defer cancel()
			}
			codec := &testServerCodec{requests: test.requests}
			Serve(ctx, codec, handler)

			codec.mu.Lock()
			got := slices.Clone(codec.responses)
			codec.mu.Unlock()

			compareJSONValue := func(a, b any) int {
				// Marshaling per-comparison is super-expensive,
				// but we're just in test and it's easy to reason about.
				aJSON, _ := jsonv2.Marshal(a, jsonv2.Deterministic(true))
				bJSON, _ := jsonv2.Marshal(b, jsonv2.Deterministic(true))
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
	requests  []jsontext.Value
	responses []any
}

func (c *testServerCodec) ReadRequest() (jsontext.Value, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.requests) == 0 {
		return nil, io.EOF
	}
	req := c.requests[0]
	c.requests = c.requests[1:]
	return req, nil
}

func (c *testServerCodec) WriteResponse(response jsontext.Value) error {
	var parsed any
	if err := jsonv2.Unmarshal(response, &parsed); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.responses = append(c.responses, parsed)
	return nil
}
