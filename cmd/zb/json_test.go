// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"testing"
)

func FuzzDedentJSON(f *testing.F) {
	f.Add([]byte(`null`))
	f.Add([]byte(`true`))
	f.Add([]byte(`false`))
	f.Add([]byte(`3.14`))
	f.Add([]byte(`123`))
	f.Add([]byte(`"foo\nbar"`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{   }`))
	f.Add([]byte(`{ "foo": "bar" }`))
	f.Add([]byte(`{ "foo": "bar",` + "\n\t" + `"baz": "quux"   }`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`[   ]`))
	f.Add([]byte(`[ 1  ,  234  ,  "array"  ]`))

	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		d := json.NewDecoder(r)
		d.UseNumber()
		var want any
		if err := d.Decode(&want); err != nil || jsonDecoderHasTrailing(d, r) {
			// Skip invalid JSON.
			return
		}

		gotBytes, err := dedentJSON(data)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(gotBytes)
		d = json.NewDecoder(r)
		d.UseNumber()
		var got any
		if err := d.Decode(&got); err != nil {
			if strconv.CanBackquote(string(data)) && strconv.CanBackquote(string(gotBytes)) {
				t.Fatalf("dedentJSON(`%s`) is invalid:\n%s", data, gotBytes)
			} else {
				t.Fatalf("dedentJSON(%q) = %q (invalid)", data, gotBytes)
			}
		}
		if trailing, err := io.ReadAll(io.MultiReader(d.Buffered(), r)); err != nil {
			if strconv.CanBackquote(string(data)) && strconv.CanBackquote(string(trailing)) {
				t.Fatalf("dedentJSON(`%s`) left trailing data:\n%s", data, trailing)
			} else {
				t.Fatalf("dedentJSON(%q) = %q (trailing data)", data, gotBytes)
			}
		}
	})
}

func jsonDecoderHasTrailing(d *json.Decoder, originalReader io.Reader) bool {
	var buf [1]byte
	n, err := io.ReadFull(io.MultiReader(d.Buffered(), originalReader), buf[:])
	return n > 0 || err != io.EOF
}
