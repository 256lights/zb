// Copyright 2024 Ross Light
// SPDX-License-Identifier: MIT

package jsonrpc

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestReader(t *testing.T) {
	type testMessage struct {
		header        Header
		contentLength int64
		body          string
	}
	tests := []struct {
		name   string
		source string
		want   []testMessage
	}{
		{
			name: "Empty",
		},
		{
			name: "SingleMessage",
			source: "Content-Length: 14\r\n" +
				"\r\n" +
				"Hello, World!\n",
			want: []testMessage{
				{
					header: Header{
						"Content-Length": {"14"},
					},
					contentLength: 14,
					body:          "Hello, World!\n",
				},
			},
		},
		{
			name: "MultipleMessage",
			source: "Content-Length: 14\r\n" +
				"\r\n" +
				"Hello, World!\n" +
				"Content-Length: 3\r\n" +
				"\r\n" +
				"foo",
			want: []testMessage{
				{
					header: Header{
						"Content-Length": {"14"},
					},
					contentLength: 14,
					body:          "Hello, World!\n",
				},
				{
					header: Header{
						"Content-Length": {"3"},
					},
					contentLength: 3,
					body:          "foo",
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := NewReader(strings.NewReader(test.source))
			var got []testMessage
			for {
				var next testMessage
				var err error
				next.header, next.contentLength, err = r.NextMessage()
				if err != nil {
					if diff := cmp.Diff(test.want, got, cmp.AllowUnexported(testMessage{})); diff != "" {
						t.Errorf("messages (-want +got):\n%s", diff)
					}
					if !errors.Is(err, io.EOF) {
						t.Errorf("finished with error: %v", err)
					}
					return
				}
				if next.contentLength < 0 {
					t.Fatalf("Unable to handle unbounded content length in messages[%d]", len(got))
				}
				body, err := io.ReadAll(r)
				if err != nil {
					t.Fatalf("Reading body in messages[%d]: %v", len(got), err)
				}
				next.body = string(body)
				got = append(got, next)
			}
		})
	}
}
