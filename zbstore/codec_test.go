// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package zbstore

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"testing"

	"zombiezen.com/go/zb/internal/jsonrpc"
)

func TestCodec(t *testing.T) {
	c1, c2 := net.Pipe()
	serverCodec := NewCodec(c1, nil)
	clientCodec := NewCodec(c2, nil)
	serveDone := make(chan struct{})
	defer func() {
		if err := clientCodec.Close(); err != nil {
			t.Error("clientCodec.Close:", err)
		}
		<-serveDone
		if err := serverCodec.Close(); err != nil {
			t.Error("serverCodec.Close:", err)
		}
	}()

	go func() {
		defer close(serveDone)
		jsonrpc.Serve(context.Background(), serverCodec, jsonrpc.ServeMux{
			"subtract": jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
				var params []int64
				if err := json.Unmarshal(req.Params, &params); err != nil {
					return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
				}
				if len(params) == 0 {
					return nil, jsonrpc.Error(jsonrpc.InvalidParams, fmt.Errorf("empty arguments"))
				}
				result := params[0]
				for _, arg := range params[1:] {
					result -= arg
				}
				return &jsonrpc.Response{
					Result: json.RawMessage(strconv.FormatInt(result, 10)),
				}, nil
			}),
		})
	}()

	client := jsonrpc.NewClient(func(ctx context.Context) (jsonrpc.ClientCodec, error) {
		return clientCodec, nil
	})
	var got int64
	err := jsonrpc.Do(context.Background(), client, "subtract", &got, []int64{42, 23})
	if want := int64(19); got != want || err != nil {
		t.Errorf("subtract[42, 23] = %d, %v; want %d, <nil>", got, err, want)
	}
}
