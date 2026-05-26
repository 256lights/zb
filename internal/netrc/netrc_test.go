// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package netrc

import (
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// curlExample is the test .netrc file used in curl's unit tests.
// See https://github.com/curl/curl/blob/curl-8_20_0/tests/data/test1304
// and https://github.com/curl/curl/blob/curl-8_20_0/tests/unit/unit1304.c
const curlExample = "" +
	"machine example.com login admin password passwd\n" +
	"machine curl.example.com login none password none\n"

func TestFind(t *testing.T) {
	tests := []struct {
		netrc string
		host  string
		want  *url.Userinfo
	}{
		{
			netrc: curlExample,
			host:  "test.example.com",
			want:  nil,
		},
		{
			netrc: curlExample,
			host:  "example.com",
			want:  url.UserPassword("admin", "passwd"),
		},
		{
			netrc: curlExample,
			host:  "EXAMPLE.COM",
			want:  url.UserPassword("admin", "passwd"),
		},
		{
			netrc: curlExample,
			host:  "curl.example.com",
			want:  url.UserPassword("none", "none"),
		},
		{
			netrc: "MACHINE EXAMPLE.COM\nLOGIN admin\nPASSWORD passwd\n",
			host:  "example.com",
			want:  url.UserPassword("admin", "passwd"),
		},
		{
			netrc: "" +
				"machine example.com password passwd\n" +
				"machine example.com login admin\n",
			host: "example.com",
			want: url.User("admin"),
		},
		{
			netrc: "machine example.com login admin password \"passwd",
			host:  "example.com",
			want:  url.UserPassword("admin", "passwd"),
		},
		{
			netrc: "" +
				`machine example.com login admin password "passwd with spaces\\backslashes\r\n\tand newlines"` + "\n",
			host: "example.com",
			want: url.UserPassword("admin", "passwd with spaces\\backslashes\r\n\tand newlines"),
		},
		{
			netrc: "" +
				"machine foo.example.com login admin password passwd\n" +
				"default login user password xyzzy\n",
			host: "foo.example.com",
			want: url.UserPassword("admin", "passwd"),
		},
		{
			netrc: "" +
				"machine foo.example.com login admin password passwd\n" +
				"default login user password xyzzy\n",
			host: "bar.example.com",
			want: url.UserPassword("user", "xyzzy"),
		},
		{
			netrc: "" +
				"macdef\n" +
				"machine example.com login root password bork\n" +
				"\r\n" +
				"machine example.com login admin password passwd\n",
			host: "example.com",
			want: url.UserPassword("admin", "passwd"),
		},
		{
			netrc: "" +
				"machine example.com login admin password passwd\n" +
				"macdef\n" +
				"machine example.com login root password bork\n",
			host: "example.com",
			want: url.UserPassword("admin", "passwd"),
		},
		{
			netrc: "macdef machine example.com login root password bork",
			host:  "example.com",
			want:  nil,
		},
		{
			netrc: "" +
				"macdef\n" +
				"machine example.com login root password bork\n",
			host: "example.com",
			want: nil,
		},
		{
			netrc: "machine example.com login admin account blabber password passwd",
			host:  "example.com",
			want:  url.UserPassword("admin", "passwd"),
		},
		{
			netrc: "machine example.com login admin junk password passwd",
			host:  "example.com",
			want:  url.UserPassword("admin", "passwd"),
		},
	}

	for _, test := range tests {
		got := Find([]byte(test.netrc), test.host)
		if diff := cmp.Diff(test.want, got, compareUserinfo()); diff != "" {
			t.Errorf("Find([]byte(%q), %q) = %v; want %v",
				test.netrc, test.host, got, test.want)
		}
	}
}

func BenchmarkFind(b *testing.B) {
	netrc := []byte("machine example.com login admin password passwd\n")
	const host = "example.com"

	for b.Loop() {
		got := Find(netrc, host)
		if got == nil {
			b.Fatalf("Find(%q, %q) = <nil>; want admin@passwd", netrc, host)
		}
	}
}

func TestFindUser(t *testing.T) {
	tests := []struct {
		netrc string
		host  string
		user  string
		want  *url.Userinfo
	}{
		{
			netrc: curlExample,
			host:  "example.com",
			user:  "me",
			want:  url.User("me"),
		},
		{
			netrc: curlExample,
			host:  "test.example.com",
			user:  "me",
			want:  url.User("me"),
		},
		{
			netrc: curlExample,
			host:  "example.com",
			user:  "a",
			want:  url.User("a"),
		},
		{
			netrc: curlExample,
			host:  "example.com",
			user:  "admin",
			want:  url.UserPassword("admin", "passwd"),
		},
		{
			netrc: curlExample,
			host:  "example.com",
			user:  "administrator",
			want:  url.User("administrator"),
		},
		{
			netrc: curlExample,
			host:  "curl.example.com",
			user:  "hilarious",
			want:  url.User("hilarious"),
		},
		{
			netrc: "" +
				"machine example.com login admin\n" +
				"machine example.com login admin password passwd\n",
			host: "example.com",
			user: "admin",
			want: url.UserPassword("admin", "passwd"),
		},
		{
			netrc: "" +
				`machine example.com login "admi\n" password BORK` + "\n" +
				`machine example.com login admi"n" password BORK` + "\n" +
				`machine example.com login "admin" password passwd` + "\n",
			host: "example.com",
			user: "admin",
			want: url.UserPassword("admin", "passwd"),
		},
		{
			netrc: "" +
				"machine example.com password passwd\n",
			host: "example.com",
			user: "admin",
			want: url.UserPassword("admin", "passwd"),
		},
	}

	for _, test := range tests {
		got := FindUser([]byte(test.netrc), test.host, test.user)
		if diff := cmp.Diff(test.want, got, compareUserinfo()); diff != "" {
			t.Errorf("FindUser([]byte(%q), %q, %q) = %v; want %v",
				test.netrc, test.host, test.user, got, test.want)
		}
	}
}

func compareUserinfo() cmp.Option {
	return cmp.Comparer(func(u1, u2 *url.Userinfo) bool {
		switch {
		case u1 == nil && u2 == nil:
			return true
		case u1 != nil && u2 != nil:
			password1, hasPassword1 := u1.Password()
			password2, hasPassword2 := u2.Password()
			return u1.Username() == u2.Username() &&
				hasPassword1 == hasPassword2 &&
				(!hasPassword1 || password1 == password2)
		default:
			return false
		}
	})
}
