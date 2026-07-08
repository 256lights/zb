// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

// Package fileurl provides URL-based access to the local filesystem.
package fileurl

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"

	"zb.256lights.llc/pkg/internal/windowspath"
)

// Scheme is the URL scheme for [Transport].
const Scheme = "file"

// Parse parses a URL, but permits some amount of sloppiness for Windows paths.
func Parse(s string) (*url.URL, error) {
	if runtime.GOOS == "windows" {
		return windowsParse(s)
	} else {
		return posixParse(s)
	}
}

func posixParse(s string) (*url.URL, error) {
	return url.Parse(s)
}

func windowsParse(s string) (*url.URL, error) {
	if windowspath.VolumeName(s) == "" {
		return url.Parse(s)
	}
	i := strings.IndexByte(s, '#')
	if i < 0 {
		i = len(s)
	}
	path, err := url.PathUnescape(s[:i])
	if err != nil {
		return nil, err
	}
	if i >= len(s)-1 {
		return &url.URL{Path: path, RawPath: s[:i]}, nil
	}

	u, err := url.Parse(s[i:])
	if err != nil {
		return nil, err
	}
	u.Path, err = url.PathUnescape(s[:i])
	if err != nil {
		return nil, err
	}
	u.RawPath = s[:i]
	return u, nil
}

// ToPath returns the filesystem path represented by u.
// ToPath returns an error if u is not a "file:" URL or a path.
func ToPath(u *url.URL) (string, error) {
	if runtime.GOOS == "windows" {
		return toWindowsPath(u)
	} else {
		return toPOSIXPath(u)
	}
}

func toPOSIXPath(u *url.URL) (string, error) {
	if u.Scheme != Scheme && (u.Scheme != "" || u.Host != "") {
		return "", fmt.Errorf("%v is not a %s:// URL", u, Scheme)
	}
	if u.Host != "" && u.Host != "localhost" {
		return "", fmt.Errorf("cannot use %s in %s:// URL", u.Host, Scheme)
	}
	return u.Path, nil
}

const uncPrefix = `\\`

func toWindowsPath(u *url.URL) (string, error) {
	switch {
	case (u.Scheme == "" || u.Scheme == Scheme) && u.Host == "":
		path := windowspath.FromSlash(u.Path)
		if p, abs := strings.CutPrefix(path, `\`); !abs || len(p) >= 2 && p[1] == ':' {
			return p, nil
		}
		return uncPrefix + "localhost" + path, nil
	case u.Scheme == Scheme && u.Host != "":
		return uncPrefix + u.Host + windowspath.FromSlash(u.Path), nil
	default:
		return "", fmt.Errorf("%v is not a %s:// URL", u, Scheme)
	}
}

// FromPath returns a "file://" URL for the given filepath.
func FromPath(path string) *url.URL {
	if runtime.GOOS == "windows" {
		return fromWindowsPath(path)
	} else {
		return fromPOSIXPath(path)
	}
}

func fromPOSIXPath(path string) *url.URL {
	if !strings.HasPrefix(path, "/") {
		return &url.URL{Path: path}
	}
	return &url.URL{
		Scheme: Scheme,
		Path:   path,
	}
}

func fromWindowsPath(path string) *url.URL {
	if !windowspath.IsAbs(path) {
		return &url.URL{Path: windowspath.ToSlash(path)}
	}
	if rest, isUNC := strings.CutPrefix(path, uncPrefix); isUNC {
		i := strings.IndexByte(rest, '\\')
		if i < 0 {
			i = len(rest)
		}
		return &url.URL{
			Scheme: Scheme,
			Host:   rest[:i],
			Path:   windowspath.ToSlash(rest[i:]),
		}
	}
	u := &url.URL{
		Scheme: Scheme,
		Path:   windowspath.ToSlash(path),
	}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	return u
}
