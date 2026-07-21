// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package httpcache

import (
	"net/http"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestCacheControlDirectives(t *testing.T) {
	tests := []struct {
		values []string
		want   []cacheControlDirective
	}{
		{values: nil, want: nil},
		{
			values: []string{"no-cache"},
			want: []cacheControlDirective{
				{name: "no-cache"},
			},
		},
		{
			values: []string{" \tno-cache\t "},
			want: []cacheControlDirective{
				{name: "no-cache"},
			},
		},
		{
			values: []string{"NO-CACHE"},
			want: []cacheControlDirective{
				{name: "NO-CACHE"},
			},
		},
		{
			values: []string{"no-cache="},
			want:   []cacheControlDirective{},
		},
		{
			values: []string{"no-cache("},
			want:   []cacheControlDirective{},
		},
		{
			values: []string{"no-cache,,"},
			want: []cacheControlDirective{
				{name: "no-cache"},
			},
		},
		{
			values: []string{"no-cache  ,  , \t "},
			want: []cacheControlDirective{
				{name: "no-cache"},
			},
		},
		{
			values: []string{`no-cache="abc`},
			want:   []cacheControlDirective{},
		},
		{
			values: []string{`no-cache="X-Foo,X-Bar"`},
			want: []cacheControlDirective{
				{name: "no-cache", rawArgument: `"X-Foo,X-Bar"`},
			},
		},
		{
			values: []string{`no-cache="X-Foo,X-Bar" abc`},
			want:   []cacheControlDirective{},
		},
		{
			values: []string{`no-cache="X-Foo,X-Bar" , abc`},
			want: []cacheControlDirective{
				{name: "no-cache", rawArgument: `"X-Foo,X-Bar"`},
				{name: "abc"},
			},
		},
		{
			values: []string{`NO-CACHE="X-Foo,X-Bar"`},
			want: []cacheControlDirective{
				{name: "NO-CACHE", rawArgument: `"X-Foo,X-Bar"`},
			},
		},
		{
			values: []string{`NO-CACHE="X-Foo,\"X-Bar"`},
			want: []cacheControlDirective{
				{name: "NO-CACHE", rawArgument: `"X-Foo,\"X-Bar"`},
			},
		},
		{
			values: []string{`NO-CACHE="X-Foo\,\"X-Bar"`},
			want: []cacheControlDirective{
				{name: "NO-CACHE", rawArgument: `"X-Foo\,\"X-Bar"`},
			},
		},
		{
			values: []string{`NO-CACHE="X-Foo\,\"X-Bar`},
			want:   []cacheControlDirective{},
		},
		{
			values: []string{`NO-CACHE="X-Foo\`},
			want:   []cacheControlDirective{},
		},
		{
			values: []string{`NO-CACHE="X-Foo\,\"X-Bar\`},
			want:   []cacheControlDirective{},
		},
		{
			values: []string{"max-age=0"},
			want: []cacheControlDirective{
				{name: "max-age", rawArgument: "0"},
			},
		},
		{
			values: []string{"max-age=5"},
			want: []cacheControlDirective{
				{name: "max-age", rawArgument: "5"},
			},
		},
		{
			values: []string{"max-age=604800"},
			want: []cacheControlDirective{
				{name: "max-age", rawArgument: "604800"},
			},
		},
		{
			values: []string{"public, max-age=31536000"},
			want: []cacheControlDirective{
				{name: "public"},
				{name: "max-age", rawArgument: "31536000"},
			},
		},
		{
			values: []string{"public", "max-age=31536000"},
			want: []cacheControlDirective{
				{name: "public"},
				{name: "max-age", rawArgument: "31536000"},
			},
		},
		{
			values: []string{`private=""`},
			want: []cacheControlDirective{
				{name: "private", rawArgument: `""`},
			},
		},
	}

	for _, test := range tests {
		h := http.Header{
			http.CanonicalHeaderKey("Cache-Control"): slices.Clone(test.values),
		}
		got := slices.Collect(cacheControlDirectives(h))
		diff := cmp.Diff(
			test.want, got,
			cmp.AllowUnexported(cacheControlDirective{}),
			cmpopts.EquateEmpty(),
		)
		if diff != "" {
			t.Errorf("cacheControlDirectives({Cache-Control: %q}) (-want +got):\n%s", test.values, diff)
		}
	}
}
