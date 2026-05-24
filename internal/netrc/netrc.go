// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

// Package netrc provides functions to parse [.netrc files].
// .netrc files contain usernames and passwords keyed by case-insensitive hosts.
//
// [.netrc files]: https://everything.curl.dev/usingcurl/netrc.html
package netrc

import (
	"bytes"
	"crypto/subtle"
	"net/url"
	"strings"
)

// Find scans a .netrc file for credentials matching the given hostname.
// If no credentials match the hostname, then Find returns nil.
func Find(netrc []byte, host string) *url.Userinfo {
	return find(netrc, host, func(creds *credentials) bool {
		return len(creds.username) > 0
	}).userinfo()
}

// FindUser scans a .netrc file for credentials matching the given hostname and username.
// FindUser will always return a non-nil [*url.Userinfo],
// but if no credentials match the hostname and user, then FindUser returns nil.
func FindUser(netrc []byte, host, user string) *url.Userinfo {
	userBytes := []byte(user)
	creds := find(netrc, host, func(creds *credentials) bool {
		return len(creds.password) > 0 &&
			(len(creds.username) == 0 || constantTimeCompareToken(userBytes, creds.username) == 1)
	})
	if creds == nil {
		return url.User(user)
	}
	return url.UserPassword(user, unquote(creds.password))
}

func find(data []byte, host string, match func(creds *credentials) bool) *credentials {
	creds := new(credentials)
	matchesHost := false
	for {
		advance, token := nextToken(data)
		data = data[advance:]

		switch {
		case len(token) == 0:
			if matchesHost && match(creds) {
				return creds
			}
			return nil
		case equalCaseInsensitive(token, "machine"):
			if matchesHost && match(creds) {
				return creds
			}
			advance, token = nextToken(data)
			data = data[advance:]
			*creds = credentials{}
			matchesHost = len(token) > 0 && equalCaseInsensitive(token, host)
		case equalCaseInsensitive(token, "default"):
			if matchesHost && match(creds) {
				return creds
			}
			*creds = credentials{}
			matchesHost = true
		case equalCaseInsensitive(token, "macdef"):
			if matchesHost && match(creds) {
				return creds
			}
			advance := skipMacro(data)
			data = data[advance:]
			*creds = credentials{}
		case equalCaseInsensitive(token, "account"):
			// Ignore next token.
			advance, _ = nextToken(data)
			data = data[advance:]
		case equalCaseInsensitive(token, "login"):
			advance, token = nextToken(data)
			data = data[advance:]
			if len(token) > 0 {
				creds.username = token
			}
		case equalCaseInsensitive(token, "password"):
			advance, token = nextToken(data)
			data = data[advance:]
			if len(token) > 0 {
				creds.password = token
			}
		}
	}
}

type credentials struct {
	username []byte
	password []byte
}

func (creds *credentials) userinfo() *url.Userinfo {
	switch {
	case creds == nil || len(creds.username) == 0:
		return nil
	case len(creds.password) > 0:
		return url.UserPassword(unquote(creds.username), unquote(creds.password))
	default:
		return url.User(unquote(creds.username))
	}
}

func nextToken(data []byte) (advance int, token []byte) {
	for advance < len(data) && isSpace(data[advance]) {
		advance++
	}
	if advance >= len(data) {
		return advance, nil
	}

	if data[advance] == '"' {
		start := advance
		advance++
		for advance < len(data) {
			switch data[advance] {
			case '"':
				advance++
				return advance, data[start:advance]
			case '\\':
				advance += 2
			default:
				advance++
			}
		}
		return advance, data[start:]
	}

	start := advance
	advance++ // We've already guaranteed this is a non-space, non-quote byte.
	for advance < len(data) && !isSpace(data[advance]) {
		advance++
	}
	return advance, data[start:advance]
}

func skipMacro(data []byte) (advance int) {
	for {
		i := bytes.IndexByte(data[advance:], '\n')
		if i == -1 {
			return len(data)
		}
		advance += i + 1
		for advance < len(data) && data[advance] == '\r' {
			advance++
		}
		if advance >= len(data) {
			return len(data)
		}
		c := data[advance]
		advance++
		if c == '\n' {
			return advance
		}
	}
}

func unquote(token []byte) string {
	token, isQuoted := bytes.CutPrefix(token, []byte(`"`))
	if !isQuoted {
		return string(token)
	}

	buf := new(strings.Builder)
	buf.Grow(len(token))
	for i := 0; i < len(token); i++ {
		switch b := token[i]; b {
		case '"':
			return buf.String()
		case '\\':
			i++
			if i < len(token) {
				buf.WriteByte(escape(token[i]))
			}
		default:
			buf.WriteByte(b)
		}
	}
	return buf.String()
}

// constantTimeCompareToken returns 1 if token represents the bytes in want and 0 otherwise.
// The time taken is a function of the length of the slices and is independent of the contents.
func constantTimeCompareToken(want, token []byte) int {
	token, isQuoted := bytes.CutPrefix(token, []byte(`"`))
	var result int
	subtle.WithDataIndependentTiming(func() {
		buf := make([]byte, 0, len(token))
		for i := 0; i < len(token); i++ {
			switch b := token[i]; b {
			case '"':
				if isQuoted {
					result = subtle.ConstantTimeCompare(want, buf)
					return
				}
				buf = append(buf, b)
			case '\\':
				i++
				if i < len(token) {
					buf = append(buf, escape(token[i]))
				}
			default:
				buf = append(buf, b)
			}
		}
		result = subtle.ConstantTimeCompare(want, buf)
	})
	return result
}

func escape(b byte) byte {
	switch b {
	case 'n':
		return '\n'
	case 'r':
		return '\r'
	case 't':
		return '\t'
	default:
		return b
	}
}

func equalCaseInsensitive[S1, S2 ~string | ~[]byte](s1 S1, s2 S2) bool {
	if len(s1) != len(s2) {
		return false
	}
	for i := range len(s1) {
		if toUpper(s1[i]) != toUpper(s2[i]) {
			return false
		}
	}
	return true
}

func toUpper(c byte) byte {
	if 'a' <= c && c <= 'z' {
		return c - 'a' + 'A'
	}
	return c
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
