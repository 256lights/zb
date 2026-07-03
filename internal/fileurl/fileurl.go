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

// ToPath returns the filesystem path represented by u.
// ToPath returns an error if u is not a "file:" URL or a path.
func ToPath(u *url.URL) (string, error) {
	if u.Scheme == "" {
		return strings.ReplaceAll(u.Path, "/", string(filepath.Separator)), nil
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("%v is not a file:// URL", u)
	}
	if runtime.GOOS == "windows" {
		path := `\\`
		if u.Host != "" {
			path += u.Host
		} else {
			path += "localhost"
		}
		path += strings.ReplaceAll(u.Path, "/", `\`)
		return path, nil
	}
	if u.Host != "" && u.Host != "localhost" {
		return "", fmt.Errorf("cannot use %s in file:// URL", u.Host)
	}
	return strings.ReplaceAll(u.Path, "/", string(filepath.Separator)), nil
}
