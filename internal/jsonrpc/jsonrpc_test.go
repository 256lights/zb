// Copyright 2024 Ross Light
// SPDX-License-Identifier: MIT

package jsonrpc

import (
	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"github.com/google/go-cmp/cmp"
)

// parseRawJSON returns a [cmp.Option] that will compare [jsontext.Value]
// by unmarshalling it.
func parseRawJSON() cmp.Option {
	return cmp.Transformer("jsontext.Value", func(msg jsontext.Value) any {
		var x any
		if err := jsonv2.Unmarshal(msg, &x); err != nil {
			return []byte(msg)
		}
		return x
	})
}
