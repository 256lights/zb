// Copyright 2024 Ross Light
// SPDX-License-Identifier: MIT

package jsonrpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/go-cmp/cmp"
)

// parseRawJSON returns a [cmp.Option] that will compare [json.RawMessage]
// by unmarshalling it.
func parseRawJSON() cmp.Option {
	return cmp.Transformer("json.RawMessage", func(msg json.RawMessage) any {
		x, err := unmarshalJSONWithNumbers(msg)
		if err != nil {
			return []byte(msg)
		}
		return x
	})
}

func unmarshalJSONWithNumbers(data []byte) (any, error) {
	r := bytes.NewReader(data)
	dec := json.NewDecoder(r)
	dec.UseNumber()
	var parsed any
	if err := dec.Decode(&parsed); err != nil {
		return nil, err
	}

	// Ensure there is no trailing data.
	var b [1]byte
	n, _ := io.MultiReader(dec.Buffered(), r).Read(b[:])
	if n > 0 {
		return parsed, errors.New("unmarshal json: trailing data")
	}
	return parsed, nil
}
