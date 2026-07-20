// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package zbstorehttp

import (
	"net/http"
	"net/url"
	"testing"
)

func TestComputeOrigin(t *testing.T) {
	tests := []struct {
		method  string
		url     string
		origin  string
		want    string
		include bool
	}{
		{
			url:     "http://www.example.com/foo",
			want:    "http://www.example.com",
			include: false,
		},
		{
			url:     "http://www.example.com/foo",
			origin:  "http://www.example.com/foo",
			want:    "http://www.example.com",
			include: false,
		},
		{
			url:     "http://www.example.com/foo",
			origin:  "http://www.example.com/bar",
			want:    "http://www.example.com",
			include: false,
		},
		{
			method:  http.MethodHead,
			url:     "http://www.example.com/foo",
			origin:  "http://www.example.com/bar",
			want:    "http://www.example.com",
			include: false,
		},
		{
			method:  http.MethodPost,
			url:     "http://www.example.com/foo",
			origin:  "http://www.example.com/bar",
			want:    "http://www.example.com",
			include: true,
		},
		{
			url:     "http://www.example.com/foo",
			origin:  "http://other.example.com/bar",
			want:    "http://other.example.com",
			include: true,
		},
		{
			url:     "http://www.example.com/foo",
			origin:  "https://www.example.com/bar",
			want:    "https://www.example.com",
			include: true,
		},
		{
			url:     "http://www.example.com/foo",
			origin:  "file:///home/foo/bar",
			want:    "null",
			include: true,
		},
		{
			url:     "file:///etc/passwd",
			origin:  "file:///home/foo/bar",
			want:    "null",
			include: true,
		},
		{
			url:     "file:///home/foo/bar",
			origin:  "file:///home/foo/bar",
			want:    "null",
			include: true,
		},
	}

	for _, test := range tests {
		u, err := url.Parse(test.url)
		if err != nil {
			t.Error(err)
			continue
		}
		var origin *url.URL
		if test.origin != "" {
			origin, err = url.Parse(test.origin)
			if err != nil {
				t.Error(err)
				continue
			}
		}

		got, gotSameOrigin := computeOrigin(test.method, u, origin)
		if got != test.want || gotSameOrigin != test.include {
			t.Errorf("computeOrigin(%q, %v, %v) = %q, %t; want %q, %t",
				test.method, u, origin, got, gotSameOrigin, test.want, test.include)
		}
	}
}
