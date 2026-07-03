// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package fileurl

import (
	"net/url"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParse(t *testing.T) {
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
	}
	if runtime.GOOS == "windows" {
		tests = append(tests,
			urlTest{
				s: `foo\bar.txt`,
				want: &url.URL{
					Path:    `foo\bar.txt`,
					RawPath: `foo\bar.txt`,
				},
			},
			urlTest{
				s: `C:\foo\bar.txt`,
				want: &url.URL{
					Path:    `C:\foo\bar.txt`,
					RawPath: `C:\foo\bar.txt`,
				},
			},
			urlTest{
				s: `C:\foo\bar baz.txt`,
				want: &url.URL{
					Path:    `C:\foo\bar baz.txt`,
					RawPath: `C:\foo\bar baz.txt`,
				},
			},
			urlTest{
				s: `C:\foo\bar.txt#baz`,
				want: &url.URL{
					Path:     `C:\foo\bar.txt`,
					RawPath:  `C:\foo\bar.txt`,
					Fragment: "baz",
				},
			},
			urlTest{
				s: `C:\foo\bar.txt#baz quux`,
				want: &url.URL{
					Path:        `C:\foo\bar.txt`,
					RawPath:     `C:\foo\bar.txt`,
					Fragment:    "baz quux",
					RawFragment: "baz quux",
				},
			},
			urlTest{
				s: `\\example.com\share\foo.txt`,
				want: &url.URL{
					Path:    `\\example.com\share\foo.txt`,
					RawPath: `\\example.com\share\foo.txt`,
				},
			},
		)
	}

	for _, test := range tests {
		got, err := Parse(test.s)
		if err != nil {
			t.Errorf("Parse(%q) = _, %v; want %v, <nil>", test.s, err, test.want)
			continue
		}
		if diff := cmp.Diff(test.want, got, cmp.Comparer(userinfoEqual)); diff != "" {
			t.Errorf("Parse(%q) (-want +got):\n%s", test.s, diff)
		}
	}
}

func TestToPath(t *testing.T) {
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
		got, err := ToPath(test.url)
		if got != test.want || err != nil {
			t.Errorf("ToPath(&url.URL{Scheme: %q, Host: %q, Path: %q}) = %q, %v; want %q, <nil>",
				test.url.Scheme, test.url.Host, test.url.Path, got, err, test.want)
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
