// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package jsonrpc_test

import (
	"context"
	"encoding/json"

	"zombiezen.com/go/zb/internal/jsonrpc"
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
