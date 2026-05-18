// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/alecthomas/kong"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestMapPathMap(t *testing.T) {
	type customOptions struct {
		PathMap map[string]string `kong:"type=pathmap"`
	}

	tests := []struct {
		args []string
		want customOptions
	}{
		{
			args: []string{},
			want: customOptions{
				PathMap: map[string]string{},
			},
		},
		{
			args: []string{"--path-map", "/bin/sh"},
			want: customOptions{
				PathMap: map[string]string{
					"/bin/sh": "/bin/sh",
				},
			},
		},
		{
			args: []string{"--path-map=/bin/sh"},
			want: customOptions{
				PathMap: map[string]string{
					"/bin/sh": "/bin/sh",
				},
			},
		},
		{
			args: []string{"--path-map", "/bin/sh=/foo/bin/sh"},
			want: customOptions{
				PathMap: map[string]string{
					"/bin/sh": "/foo/bin/sh",
				},
			},
		},
		{
			args: []string{"--path-map", "/bin/sh=/foo/bin/sh /bin/true"},
			want: customOptions{
				PathMap: map[string]string{
					"/bin/sh":   "/foo/bin/sh",
					"/bin/true": "/bin/true",
				},
			},
		},
	}

	for _, test := range tests {
		got := new(customOptions)
		k, err := kong.New(got, kong.NamedMapper("pathmap", kong.MapperFunc(mapPathMap)))
		if err != nil {
			t.Fatal(err)
		}
		k.Stdout = t.Output()
		k.Stderr = t.Output()
		k.Exit = func(int) {}

		if _, err := k.Parse(test.args); err != nil {
			t.Errorf("parse %q: %v", test.args, err)
			continue
		}

		if diff := cmp.Diff(&test.want, got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("parse %q (-want +got):\n%s", test.args, diff)
		}
	}
}
