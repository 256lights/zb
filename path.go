// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package zb

import (
	"fmt"
	"os"
	posixpath "path"
	"path/filepath"
	"runtime"
	"strings"

	"zombiezen.com/go/nix/nixbase32"
	"zombiezen.com/go/zb/internal/windowspath"
)

// StoreDirectory is the absolute path of a zb store.
type StoreDirectory string

const (
	// DefaultUnixStoreDirectory is the default zb store directory
	// on Unix-like systems.
	DefaultUnixStoreDirectory StoreDirectory = "/zb/store"

	// DefaultWindowsStoreDirectory is the default zb store directory
	// on Windows systems.
	DefaultWindowsStoreDirectory StoreDirectory = `C:\zb\store`
)

// CleanStoreDirectory cleans an absolute POSIX-style or Windows-style path
// as a [StoreDirectory].
// It returns an error if the path is not absolute.
func CleanStoreDirectory(path string) (StoreDirectory, error) {
	switch detectPathStyle(path) {
	case posixPathStyle:
		if !posixpath.IsAbs(path) {
			return "", fmt.Errorf("store directory %q is not absolute", path)
		}
		return StoreDirectory(posixpath.Clean(path)), nil
	case windowsPathStyle:
		if !windowspath.IsAbs(path) {
			return "", fmt.Errorf("store directory %q is not absolute", path)
		}
		return StoreDirectory(windowspath.Clean(path)), nil
	default:
		return "", fmt.Errorf("store directory %q is not absolute", path)
	}
}

// StoreDirectoryFromEnvironment returns the zb store directory in use
// based on the ZB_STORE_DIR environment variable,
// falling back to [DefaultUnixStoreDirectory] or [DefaultWindowsStoreDirectory] if not set.
func StoreDirectoryFromEnvironment() (StoreDirectory, error) {
	dir := os.Getenv("ZB_STORE_DIR")
	if dir == "" {
		if runtime.GOOS == "windows" {
			return DefaultWindowsStoreDirectory, nil
		}
		return DefaultUnixStoreDirectory, nil
	}
	if !filepath.IsAbs(dir) {
		// The directory must be in the format of the local OS.
		return "", fmt.Errorf("store directory %q is not absolute", dir)
	}
	return CleanStoreDirectory(dir)
}

// Object returns the store path for the given store object name.
func (dir StoreDirectory) Object(name string) (StorePath, error) {
	joined := dir.Join(name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("parse zb store path %s: invalid object name %q", joined, name)
	}
	storePath, err := ParseStorePath(joined)
	if err != nil {
		return "", err
	}
	return storePath, nil
}

// Join joins any number of path elements to the store directory
// separated by slashes.
func (dir StoreDirectory) Join(elem ...string) string {
	switch detectPathStyle(string(dir)) {
	case windowsPathStyle:
		return windowspath.Join(append([]string{string(dir)}, elem...)...)
	default:
		return posixpath.Join(append([]string{string(dir)}, elem...)...)
	}
}

// ParsePath verifies that a given absolute slash-separated path
// begins with the store directory
// and names either a store object or a file inside a store object.
// On success, it returns the store object's name
// and the relative path inside the store object, if any.
func (dir StoreDirectory) ParsePath(path string) (storePath StorePath, sub string, err error) {
	var cleaned, dirPrefix, tail string
	var sep rune
	switch detectPathStyle(string(dir)) {
	case posixPathStyle:
		if !posixpath.IsAbs(path) {
			return "", "", fmt.Errorf("parse zb store path %s: not absolute", path)
		}
		sep = '/'
		cleaned = posixpath.Clean(path)
		dirPrefix = posixpath.Clean(string(dir)) + string(sep)
		var ok bool
		tail, ok = strings.CutPrefix(cleaned, dirPrefix)
		if !ok {
			return "", "", fmt.Errorf("parse zb store path %s: outside %s", path, dir)
		}
	case windowsPathStyle:
		if !windowspath.IsAbs(path) {
			return "", "", fmt.Errorf("parse zb store path %s: not absolute", path)
		}
		sep = windowspath.Separator
		cleaned = windowspath.Clean(path)
		dirPrefix = windowspath.Clean(string(dir)) + string(sep)
		var ok bool
		tail, ok = strings.CutPrefix(cleaned, dirPrefix)
		if !ok {
			return "", "", fmt.Errorf("parse zb store path %s: outside %s", path, dir)
		}
	default:
		return "", "", fmt.Errorf("parse zb store path %s: directory %s not absolute", path, dir)
	}
	childName, sub, _ := strings.Cut(tail, string(sep))
	storePath, err = ParseStorePath(cleaned[:len(dirPrefix)+len(childName)])
	if err != nil {
		return "", "", err
	}
	return storePath, sub, nil
}

// StorePath is a zb store path:
// the absolute path of a zb store object in the filesystem.
// For example: "/zb/store/s66mzxpvicwk07gjbjfw9izjfa797vsw-hello-2.12.1"
// or "C:\zb\store\s66mzxpvicwk07gjbjfw9izjfa797vsw-hello-2.12.1".
type StorePath string

const (
	objectNameDigestLength = 32
	maxObjectNameLength    = objectNameDigestLength + 1 + 211
)

// ParseStorePath parses an absolute slash-separated path as a store path
// (i.e. an immediate child of a zb store directory).
func ParseStorePath(path string) (StorePath, error) {
	var cleaned, base string
	switch detectPathStyle(path) {
	case posixPathStyle:
		cleaned = posixpath.Clean(path)
		_, base = posixpath.Split(cleaned)
	case windowsPathStyle:
		cleaned = windowspath.Clean(path)
		_, base = windowspath.Split(cleaned)
	default:
		return "", fmt.Errorf("parse zb store path %s: not absolute", path)
	}
	if len(base) < objectNameDigestLength+len("-")+1 {
		return "", fmt.Errorf("parse zb store path %s: %q is too short", path, base)
	}
	if len(base) > maxObjectNameLength {
		return "", fmt.Errorf("parse zb store path %s: %q is too long", path, base)
	}
	for i := 0; i < len(base); i++ {
		if !isNameChar(base[i]) {
			return "", fmt.Errorf("parse zb store path %s: %q contains illegal character %q", path, base, base[i])
		}
	}
	if err := nixbase32.ValidateString(base[:objectNameDigestLength]); err != nil {
		return "", fmt.Errorf("parse zb store path %s: %v", path, err)
	}
	if base[objectNameDigestLength] != '-' {
		return "", fmt.Errorf("parse zb store path %s: digest not separated by dash", path)
	}
	return StorePath(cleaned), nil
}

// Dir returns the path's directory.
func (path StorePath) Dir() StoreDirectory {
	switch detectPathStyle(string(path)) {
	case posixPathStyle:
		return StoreDirectory(posixpath.Dir(string(path)))
	case windowsPathStyle:
		return StoreDirectory(windowspath.Dir(string(path)))
	default:
		return ""
	}
}

// Base returns the last element of the path.
func (path StorePath) Base() string {
	if path == "" {
		return ""
	}
	switch detectPathStyle(string(path)) {
	case posixPathStyle:
		return posixpath.Base(string(path))
	case windowsPathStyle:
		return windowspath.Base(string(path))
	default:
		return ""
	}
}

// IsDerivation reports whether the name ends in ".drv".
func (path StorePath) IsDerivation() bool {
	return strings.HasSuffix(path.Base(), ".drv")
}

// Digest returns the digest part of the name.
func (path StorePath) Digest() string {
	base := path.Base()
	if len(base) < objectNameDigestLength {
		return ""
	}
	return string(base[:objectNameDigestLength])
}

// Name returns the part of the name after the digest.
func (path StorePath) Name() string {
	base := path.Base()
	if len(base) <= objectNameDigestLength+len("-") {
		return ""
	}
	return string(base[objectNameDigestLength+len("-"):])
}

// MarshalText returns a byte slice of the path
// or an error if it's empty.
func (path StorePath) MarshalText() ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("marshal zb store path: empty")
	}
	return []byte(path), nil
}

// UnmarshalText validates and cleans the path in the same way as [ParseStorePath]
// and stores it into *path.
func (path *StorePath) UnmarshalText(data []byte) error {
	var err error
	*path, err = ParseStorePath(string(data))
	if err != nil {
		return err
	}
	return nil
}

type pathStyle int8

const (
	posixPathStyle pathStyle = 1 + iota
	windowsPathStyle
)

func localPathStyle() pathStyle {
	if runtime.GOOS == "windows" {
		return windowsPathStyle
	}
	return posixPathStyle
}

// detectPathStyle returns the OS that the given absolute path uses,
// or zero if the path is not absolute.
func detectPathStyle(path string) pathStyle {
	switch {
	case posixpath.IsAbs(path):
		return posixPathStyle
	case windowspath.IsAbs(path):
		return windowsPathStyle
	default:
		return 0
	}
}

func isNameChar(c byte) bool {
	return 'a' <= c && c <= 'z' ||
		'A' <= c && c <= 'Z' ||
		'0' <= c && c <= '9' ||
		c == '+' || c == '-' || c == '.' || c == '_' || c == '='
}
