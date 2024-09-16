// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"zb.256lights.llc/pkg/internal/jsonstring"
	"zb.256lights.llc/pkg/internal/xslices"
)

func dedentJSON(data json.RawMessage) ([]byte, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var buf []byte
	var stack []json.Delim
	delimState := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if len(stack) > 0 && tok != json.Delim('}') && tok != json.Delim(']') {
			outer := xslices.Last(stack)
			if delimState != 0 {
				buf = append(buf, byte(delimState))
			}
			switch delimState {
			case 0, ',':
				if outer == '{' {
					delimState = ':'
				} else {
					delimState = ','
				}
			case ':':
				delimState = ','
			default:
				panic("unreachable")
			}
		}

		switch tok := tok.(type) {
		case nil:
			buf = append(buf, "null"...)
		case json.Delim:
			buf = append(buf, string(rune(tok))...)
			switch tok {
			case '{', '[':
				stack = append(stack, tok)
				delimState = 0
			case ']', '}':
				stack = xslices.Pop(stack, 1)
				delimState = ','
			}
		case bool:
			buf = strconv.AppendBool(buf, tok)
		case json.Number:
			buf = append(buf, tok...)
		case string:
			buf = jsonstring.Append(buf, tok)
		default:
			return nil, fmt.Errorf("unhandled token type %T", tok)
		}
	}

	return buf, nil
}
