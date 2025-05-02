// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package frontend

import (
	"net/url"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseURL(t *testing.T) {
	type urlTest struct {
		s    string
		want *url.URL
	}
	tests := []urlTest{
		{
			s:    "foo.txt",
			want: &url.URL{Path: "foo.txt"},
		},
		{
			s: "foo.txt#bar",
			want: &url.URL{
				Path:     "foo.txt",
				Fragment: "bar",
			},
		},
		{
			s: "foo.txt#bar baz",
			want: &url.URL{
				Path:        "foo.txt",
				Fragment:    "bar baz",
				RawFragment: "bar baz",
			},
		},
		{
			s: "http://www.example.com/foo#bar",
			want: &url.URL{
				Scheme:   "http",
				Host:     "www.example.com",
				Path:     "/foo",
				Fragment: "bar",
			},
		},
		{
			s:    "data:abc",
			want: &url.URL{Scheme: "data", Opaque: "abc"},
		},
	}
	if runtime.GOOS == "windows" {
		tests = append(tests,
			urlTest{
				s: `foo\bar.txt`,
				want: &url.URL{
					Path: "foo/bar.txt",
				},
			},
			urlTest{
				s: `C:\foo\bar.txt`,
				want: &url.URL{
					Scheme: "file",
					Path:   "/C:/foo/bar.txt",
				},
			},
			urlTest{
				s: `C:\foo\bar baz.txt`,
				want: &url.URL{
					Scheme: "file",
					Path:   "/C:/foo/bar%20baz.txt",
				},
			},
			urlTest{
				s: `C:\foo\bar.txt#baz`,
				want: &url.URL{
					Scheme:   "file",
					Path:     `/C:/foo/bar.txt`,
					Fragment: "baz",
				},
			},
			urlTest{
				s: `C:\foo\bar.txt#baz quux`,
				want: &url.URL{
					Scheme:      "file",
					Path:        "/C:/foo/bar.txt",
					Fragment:    "baz quux",
					RawFragment: "baz quux",
				},
			},
			urlTest{
				s: `\\example.com\share\foo.txt`,
				want: &url.URL{
					Scheme: "file",
					Host:   "example.com",
					Path:   "/share/foo.txt",
				},
			},
		)
	}

	for _, test := range tests {
		got, err := ParseURL(test.s)
		if err != nil {
			t.Errorf("parseURL(%q) = _, %v; want %v, <nil>", test.s, err, test.want)
			continue
		}
		if diff := cmp.Diff(test.want, got, cmp.Comparer(userinfoEqual)); diff != "" {
			t.Errorf("parseURL(%q) (-want +got):\n%s", test.s, diff)
		}
	}
}

func TestURLToPath(t *testing.T) {
	type urlTest struct {
		url  *url.URL
		want string
	}
	tests := []urlTest{
		{
			url:  &url.URL{Path: "foo/bar.txt"},
			want: filepath.Join("foo", "bar.txt"),
		},
		{
			url:  &url.URL{Path: "foo/bar.txt"},
			want: filepath.Join("foo", "bar.txt"),
		},
	}
	if runtime.GOOS == "windows" {
		tests = append(tests,
			urlTest{
				url:  &url.URL{Scheme: "file", Path: "/foo/bar.txt"},
				want: `\\localhost\foo\bar.txt`,
			},
			urlTest{
				url:  &url.URL{Scheme: "file", Host: "localhost", Path: "/foo/bar.txt"},
				want: `\\localhost\foo\bar.txt`,
			},
			urlTest{
				url:  &url.URL{Scheme: "file", Host: "example.com", Path: "/share/foo/bar.txt"},
				want: `\\example.com\share\foo\bar.txt`,
			},
			urlTest{
				url:  &url.URL{Path: `C:\foo\bar.txt`},
				want: `C:\foo\bar.txt`,
			},
			urlTest{
				url:  &url.URL{Path: `C:/foo/bar.txt`},
				want: `C:\foo\bar.txt`,
			},
		)
	} else {
		tests = append(tests,
			urlTest{
				url:  &url.URL{Scheme: "file", Path: "/foo/bar.txt"},
				want: "/foo/bar.txt",
			},
		)
	}

	for _, test := range tests {
		got, err := URLToPath(test.url)
		if got != test.want || err != nil {
			t.Errorf("urlToPath(&url.URL{Scheme: %q, Host: %q, Path: %q}) = %q, %v; want %q, <nil>",
				test.url.Scheme, test.url.Host, test.url.Path, got, err, test.want)
		}
	}
}

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

func userinfoEqual(u1, u2 *url.Userinfo) bool {
	user1 := u1.Username()
	pass1, hasPass1 := u1.Password()
	user2 := u2.Username()
	pass2, hasPass2 := u2.Password()
	return user1 == user2 && pass1 == pass2 && hasPass1 == hasPass2
}
