// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package fileurl

import (
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParse(t *testing.T) {
	tests := []struct {
		s       string
		want    *url.URL
		windows bool
	}{
		{
			s:    "foo.txt",
			want: &url.URL{Path: "foo.txt"},
		},
		{
			s:    "foo bar.txt",
			want: &url.URL{Path: "foo bar.txt", RawPath: "foo bar.txt"},
		},
		{
			s:    "foo%20bar.txt",
			want: &url.URL{Path: "foo bar.txt"},
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
		{
			s: `foo\bar.txt`,
			want: &url.URL{
				Path:    `foo\bar.txt`,
				RawPath: `foo\bar.txt`,
			},
			windows: true,
		},
		{
			s: `C:\foo\bar.txt`,
			want: &url.URL{
				Path:    `C:\foo\bar.txt`,
				RawPath: `C:\foo\bar.txt`,
			},
			windows: true,
		},
		{
			s: `C:\foo\bar baz.txt`,
			want: &url.URL{
				Path:    `C:\foo\bar baz.txt`,
				RawPath: `C:\foo\bar baz.txt`,
			},
			windows: true,
		},
		{
			s: `C:\foo\bar.txt#baz`,
			want: &url.URL{
				Path:     `C:\foo\bar.txt`,
				RawPath:  `C:\foo\bar.txt`,
				Fragment: "baz",
			},
			windows: true,
		},
		{
			s: `C:\foo\bar.txt#baz quux`,
			want: &url.URL{
				Path:        `C:\foo\bar.txt`,
				RawPath:     `C:\foo\bar.txt`,
				Fragment:    "baz quux",
				RawFragment: "baz quux",
			},
			windows: true,
		},
		{
			s: `\\example.com\share\foo.txt`,
			want: &url.URL{
				Path:    `\\example.com\share\foo.txt`,
				RawPath: `\\example.com\share\foo.txt`,
			},
			windows: true,
		},
	}

	t.Run("POSIX", func(t *testing.T) {
		for _, test := range tests {
			if test.windows {
				continue
			}
			got, err := posixParse(test.s)
			if err != nil {
				t.Errorf("posixParse(%q) = _, %v; want %v, <nil>", test.s, err, test.want)
				continue
			}
			if diff := cmp.Diff(test.want, got, cmp.Comparer(userinfoEqual)); diff != "" {
				t.Errorf("posixParse(%q) (-want +got):\n%s", test.s, diff)
			}
		}
	})

	t.Run("Windows", func(t *testing.T) {
		for _, test := range tests {
			if !test.windows {
				continue
			}
			got, err := windowsParse(test.s)
			if err != nil {
				t.Errorf("windowsParse(%q) = _, %v; want %v, <nil>", test.s, err, test.want)
				continue
			}
			if diff := cmp.Diff(test.want, got, cmp.Comparer(userinfoEqual)); diff != "" {
				t.Errorf("windowsParse(%q) (-want +got):\n%s", test.s, diff)
			}
		}
	})
}

type pathTest struct {
	path         string
	url          *url.URL
	canonicalURL bool
}

func posixPathTests() []pathTest {
	return []pathTest{
		{
			url:          &url.URL{Path: "foo bar.txt"},
			path:         "foo bar.txt",
			canonicalURL: true,
		},
		{
			url:          &url.URL{Path: "foo/bar.txt"},
			path:         "foo/bar.txt",
			canonicalURL: true,
		},
		{
			url:          &url.URL{Scheme: Scheme, Path: "/foo/bar.txt"},
			path:         "/foo/bar.txt",
			canonicalURL: true,
		},
	}
}

func windowsPathTests() []pathTest {
	return []pathTest{
		{
			url:          &url.URL{Path: "foo bar.txt"},
			path:         "foo bar.txt",
			canonicalURL: true,
		},
		{
			url:          &url.URL{Path: "foo/bar.txt"},
			path:         `foo\bar.txt`,
			canonicalURL: true,
		},
		{
			url:          &url.URL{Scheme: Scheme, Host: "localhost", Path: "/foo/bar.txt"},
			path:         `\\localhost\foo\bar.txt`,
			canonicalURL: true,
		},
		{
			url:  &url.URL{Scheme: Scheme, Path: "/foo/bar.txt"},
			path: `\\localhost\foo\bar.txt`,
		},
		{
			url:          &url.URL{Scheme: Scheme, Host: "example.com", Path: "/share/foo/bar.txt"},
			path:         `\\example.com\share\foo\bar.txt`,
			canonicalURL: true,
		},
		{
			url:          &url.URL{Scheme: Scheme, Path: `/C:/foo/bar.txt`},
			path:         `C:\foo\bar.txt`,
			canonicalURL: true,
		},
		{
			url:  &url.URL{Path: `C:\foo\bar.txt`},
			path: `C:\foo\bar.txt`,
		},
		{
			url:  &url.URL{Path: `C:/foo/bar.txt`},
			path: `C:\foo\bar.txt`,
		},
		{
			url:  &url.URL{Path: `/C:/foo/bar.txt`},
			path: `C:\foo\bar.txt`,
		},
		{
			url:          &url.URL{Scheme: Scheme, Host: "localhost", Path: `/C:/foo/bar.txt`},
			path:         `\\localhost\C:\foo\bar.txt`,
			canonicalURL: true,
		},
	}
}

func TestToPOSIXPath(t *testing.T) {
	for _, test := range posixPathTests() {
		got, err := toPOSIXPath(test.url)
		if got != test.path || err != nil {
			t.Errorf("toPOSIXPath(&url.URL{Scheme: %q, Host: %q, Path: %q}) = %q, %v; want %q, <nil>",
				test.url.Scheme, test.url.Host, test.url.Path, got, err, test.path)
		}
	}
}

func TestToWindowsPath(t *testing.T) {
	for _, test := range windowsPathTests() {
		got, err := toWindowsPath(test.url)
		if got != test.path || err != nil {
			t.Errorf("toWindowsPath(&url.URL{Scheme: %q, Host: %q, Path: %q}) = %q, %v; want %q, <nil>",
				test.url.Scheme, test.url.Host, test.url.Path, got, err, test.path)
		}
	}
}

func TestFromPOSIXPath(t *testing.T) {
	for _, test := range posixPathTests() {
		if !test.canonicalURL {
			continue
		}
		got := fromPOSIXPath(test.path)
		if diff := cmp.Diff(test.url, got, cmp.Comparer(userinfoEqual)); diff != "" {
			t.Errorf("fromPOSIXPath(%q) (-want +got):\n%s", test.path, diff)
		}
	}
}

func TestFromWindowsPath(t *testing.T) {
	for _, test := range windowsPathTests() {
		if !test.canonicalURL {
			continue
		}
		got := fromWindowsPath(test.path)
		if diff := cmp.Diff(test.url, got, cmp.Comparer(userinfoEqual)); diff != "" {
			t.Errorf("fromWindowsPath(%q) (-want +got):\n%s", test.path, diff)
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
