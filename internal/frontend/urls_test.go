// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package frontend

import (
	"slices"
	"testing"
)

func TestParseFragment(t *testing.T) {
	tests := []struct {
		s           string
		archivePath string
		keyPath     string
		err         bool
	}{
		{
			s:           "",
			archivePath: "",
			keyPath:     "",
		},
		{
			s:           "foo",
			archivePath: "",
			keyPath:     "foo",
		},
		{
			s:           "1.2.3",
			archivePath: "",
			keyPath:     "1.2.3",
		},
		{
			s:           "/",
			archivePath: "",
			keyPath:     "/",
			err:         true,
		},
		{
			s:           "foo/bar",
			archivePath: "",
			keyPath:     "foo/bar",
		},
		{
			s:           "foo//bar",
			archivePath: "",
			keyPath:     "foo//bar",
		},
		{
			s:           "/foo/bar",
			archivePath: "",
			keyPath:     "/foo/bar",
			err:         true,
		},
		{
			s:           "foo/bar/",
			archivePath: "",
			keyPath:     "foo/bar/",
			err:         true,
		},
		{
			s:           "foo.lua:bar",
			archivePath: "foo.lua",
			keyPath:     "bar",
		},
		{
			s:           "foo.lua:",
			archivePath: "foo.lua",
			keyPath:     "",
		},
		{
			s:           "foo/bar.lua:baz",
			archivePath: "foo/bar.lua",
			keyPath:     "baz",
		},
		{
			s:           "foo/bar.lua:baz",
			archivePath: "foo/bar.lua",
			keyPath:     "baz",
		},
		{
			s:           "foo/bar:baz.lua:quux",
			archivePath: "foo/bar:baz.lua",
			keyPath:     "quux",
		},
	}

	for _, test := range tests {
		archivePath, keyPath, err := parseFragment(test.s)
		if archivePath != test.archivePath || keyPath != test.keyPath || (err != nil) != test.err {
			errString := "<nil>"
			if test.err {
				errString = "<error>"
			}
			t.Errorf("parseFragment(%q) = %q, %q, %v; want %q, %q, %s",
				test.s, archivePath, keyPath, err, test.archivePath, test.keyPath, errString)
		}
	}
}

func TestSplitKeyPath(t *testing.T) {
	tests := []struct {
		s    string
		want []string
	}{
		{"", []string{}},
		{"/", []string{"", ""}},
		{"foo", []string{"foo"}},
		{"foo.bar", []string{"foo.bar"}},
		{"foo/bar", []string{"foo", "bar"}},
		{"foo//bar", []string{"foo", "bar"}},
		{"/foo", []string{"", "foo"}},
		{"foo/", []string{"foo", ""}},
		{"/foo/", []string{"", "foo", ""}},
		{"//foo", []string{"", "foo"}},
		{"foo//", []string{"foo", ""}},
		{"//foo//", []string{"", "foo", ""}},
	}

	for _, test := range tests {
		got := slices.Collect(splitKeyPath(test.s))
		if !slices.Equal(test.want, got) {
			t.Errorf("slices.Collect(splitKeyPath(%q)) = %q; want %q", test.s, got, test.want)
		}
	}
}
