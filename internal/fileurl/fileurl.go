// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

// Package fileurl provides URL-based access to the local filesystem.
package fileurl

import (
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// Scheme is the URL scheme for [Transport].
const Scheme = "file"

// Parse parses a URL, but permits some amount of sloppiness for Windows paths.
func Parse(s string) (*url.URL, error) {
	if filepath.VolumeName(s) != "" {
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

	return url.Parse(s)
}

const uncPrefix = `\\`

// ToPath returns the filesystem path represented by u.
// ToPath returns an error if u is not a "file:" URL or a path.
func ToPath(u *url.URL) (string, error) {
	if u.Scheme == "" {
		return strings.ReplaceAll(u.Path, "/", string(filepath.Separator)), nil
	}
	if u.Scheme != Scheme {
		return "", fmt.Errorf("%v is not a %s:// URL", u, Scheme)
	}
	if runtime.GOOS == "windows" {
		path := uncPrefix
		if u.Host != "" {
			path += u.Host
		} else {
			path += "localhost"
		}
		path += strings.ReplaceAll(u.Path, "/", `\`)
		return path, nil
	}
	if u.Host != "" && u.Host != "localhost" {
		return "", fmt.Errorf("cannot use %s in %s:// URL", u.Host, Scheme)
	}
	return strings.ReplaceAll(u.Path, "/", string(filepath.Separator)), nil
}

// FromPath returns a "file://" URL for the given filepath.
func FromPath(path string) *url.URL {
	if !filepath.IsAbs(path) {
		return &url.URL{Path: filepath.ToSlash(path)}
	}
	if runtime.GOOS == "windows" {
		if rest, isUNC := strings.CutPrefix(path, uncPrefix); isUNC {
			i := strings.IndexByte(rest, '\\')
			if i < 0 {
				i = len(rest)
			}
			return &url.URL{
				Scheme: Scheme,
				Host:   rest[:i],
				Path:   filepath.ToSlash(rest[i:]),
			}
		}
	}
	u := &url.URL{
		Scheme: Scheme,
		Path:   filepath.ToSlash(path),
	}
	if runtime.GOOS == "windows" {
		u.Host = "localhost"
	}
	return u
}
