// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package jsonrpc_test

import (
	"context"
	"encoding/json"
	"fmt"

	"zb.256lights.llc/pkg/internal/jsonrpc"
)

func ExampleClient_Codec() {
	// Assuming that we have Context and client from elsewhere.
	ctx := context.Background()
	var client *jsonrpc.Client

	// Obtain a codec.
	codec, releaseCodec, err := client.Codec(ctx)
	if err != nil {
		// handle error...
	}
	defer releaseCodec()

	// Send a notification manually.
	err = codec.WriteRequest(json.RawMessage(`{"jsonrpc": "2.0", "method": "foobar"}`))
	if err != nil {
		// handle error...
	}
}

func ExampleDo() {
	ctx := context.Background()
	handler := jsonrpc.ServeMux{
		"subtract": jsonrpc.HandlerFunc(subtractHandler),
	}

	var result int64
	if err := jsonrpc.Do(ctx, handler, "subtract", &result, []int64{42, 23}); err != nil {
		panic(err)
	}
	fmt.Println("Handler returned", result)
	// Output:
	// Handler returned 19
}

func ExampleNotify() {
	ctx := context.Background()
	handler := jsonrpc.ServeMux{
		"update": jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
			var params []int64
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return nil, err
			}
			fmt.Println(params)
			return nil, nil
		}),
	}

	if err := jsonrpc.Notify(ctx, handler, "update", []int64{1, 2, 3, 4, 5}); err != nil {
		panic(err)
	}
	// Output:
	// [1 2 3 4 5]
}
