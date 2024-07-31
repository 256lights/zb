// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package zbstore

import (
	"fmt"
	"os"
	posixpath "path"
	"path/filepath"
	"runtime"
	"strings"

	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nixbase32"
	"zombiezen.com/go/zb/internal/sortedset"
	"zombiezen.com/go/zb/internal/windowspath"
)

// Directory is the absolute path of a zb store.
type Directory string

const (
	// DefaultUnixDirectory is the default zb store directory
	// on Unix-like systems.
	DefaultUnixDirectory Directory = "/zb/store"

	// DefaultWindowsDirectory is the default zb store directory
	// on Windows systems.
	DefaultWindowsDirectory Directory = `C:\zb\store`
)

// CleanDirectory cleans an absolute POSIX-style or Windows-style path
// as a [Directory].
// It returns an error if the path is not absolute.
func CleanDirectory(path string) (Directory, error) {
	switch detectPathStyle(path) {
	case posixPathStyle:
		if !posixpath.IsAbs(path) {
			return "", fmt.Errorf("store directory %q is not absolute", path)
		}
		return Directory(posixpath.Clean(path)), nil
	case windowsPathStyle:
		if !windowspath.IsAbs(path) {
			return "", fmt.Errorf("store directory %q is not absolute", path)
		}
		return Directory(windowspath.Clean(path)), nil
	default:
		return "", fmt.Errorf("store directory %q is not absolute", path)
	}
}

// DirectoryFromEnvironment returns the zb store [Directory] in use
// based on the ZB_STORE_DIR environment variable,
// falling back to [DefaultUnixDirectory] or [DefaultWindowsDirectory] if not set.
func DirectoryFromEnvironment() (Directory, error) {
	dir := os.Getenv("ZB_STORE_DIR")
	if dir == "" {
		if runtime.GOOS == "windows" {
			return DefaultWindowsDirectory, nil
		}
		return DefaultUnixDirectory, nil
	}
	if !filepath.IsAbs(dir) {
		// The directory must be in the format of the local OS.
		return "", fmt.Errorf("store directory %q is not absolute", dir)
	}
	return CleanDirectory(dir)
}

// Object returns the store path for the given store object name.
func (dir Directory) Object(name string) (Path, error) {
	joined := dir.Join(name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("parse zb store path %s: invalid object name %q", joined, name)
	}
	storePath, err := ParsePath(joined)
	if err != nil {
		return "", err
	}
	return storePath, nil
}

// Join joins any number of path elements to the store directory
// separated by the store directory's separator type.
func (dir Directory) Join(elem ...string) string {
	switch detectPathStyle(string(dir)) {
	case windowsPathStyle:
		return windowspath.Join(append([]string{string(dir)}, elem...)...)
	default:
		return posixpath.Join(append([]string{string(dir)}, elem...)...)
	}
}

// ParsePath verifies that a given absolute path
// begins with the store directory
// and names either a store object or a file inside a store object.
// On success, it returns the store object's name
// and the relative path inside the store object, if any.
func (dir Directory) ParsePath(path string) (storePath Path, sub string, err error) {
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
	storePath, err = ParsePath(cleaned[:len(dirPrefix)+len(childName)])
	if err != nil {
		return "", "", err
	}
	return storePath, sub, nil
}

// IsNative reports whether the directory uses the same path style
// as the running operating system.
func (dir Directory) IsNative() bool {
	return detectPathStyle(string(dir)) == localPathStyle()
}

// Path is a zb store path:
// the absolute path of a zb store object in the filesystem.
// For example: "/zb/store/s66mzxpvicwk07gjbjfw9izjfa797vsw-hello-2.12.1"
// or "C:\zb\store\s66mzxpvicwk07gjbjfw9izjfa797vsw-hello-2.12.1".
type Path string

const (
	objectNameDigestLength = 32
	maxObjectNameLength    = objectNameDigestLength + 1 + 211
)

// ParsePath parses an absolute path as a store path
// (i.e. an immediate child of a zb store directory).
func ParsePath(path string) (Path, error) {
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
	return Path(cleaned), nil
}

// Dir returns the path's directory.
func (path Path) Dir() Directory {
	switch detectPathStyle(string(path)) {
	case posixPathStyle:
		return Directory(posixpath.Dir(string(path)))
	case windowsPathStyle:
		return Directory(windowspath.Dir(string(path)))
	default:
		return ""
	}
}

// Base returns the last element of the path.
func (path Path) Base() string {
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

// IsDerivation reports whether the name ends in [DerivationExt].
func (path Path) IsDerivation() bool {
	return strings.HasSuffix(path.Base(), DerivationExt)
}

// Digest returns the digest part of the name.
func (path Path) Digest() string {
	base := path.Base()
	if len(base) < objectNameDigestLength {
		return ""
	}
	return string(base[:objectNameDigestLength])
}

// Name returns the part of the name after the digest.
func (path Path) Name() string {
	base := path.Base()
	if len(base) <= objectNameDigestLength+len("-") {
		return ""
	}
	return string(base[objectNameDigestLength+len("-"):])
}

// Join joins any number of path elements to the store path
// separated by the store path's separator type.
func (path Path) Join(elem ...string) string {
	elem = append([]string{path.Base()}, elem...)
	return path.Dir().Join(elem...)
}

// IsNative reports whether the path uses the same path style
// as the running operating system.
func (path Path) IsNative() bool {
	return detectPathStyle(string(path)) == localPathStyle()
}

// MarshalText returns a byte slice of the path
// or an error if it's empty.
func (path Path) MarshalText() ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("marshal zb store path: empty")
	}
	return []byte(path), nil
}

// UnmarshalText validates and cleans the path in the same way as [ParsePath]
// and stores it into *path.
func (path *Path) UnmarshalText(data []byte) error {
	var err error
	*path, err = ParsePath(string(data))
	if err != nil {
		return err
	}
	return nil
}

// FixedCAOutputPath computes the path of a store object
// with the given directory, name, content address, and reference set.
func FixedCAOutputPath(dir Directory, name string, ca nix.ContentAddress, refs References) (Path, error) {
	h := ca.Hash()
	htype := h.Type()
	switch {
	case ca.IsText():
		if want := nix.SHA256; htype != want {
			return "", fmt.Errorf("compute fixed output path for %s: text must be content-addressed by %v (got %v)",
				name, want, htype)
		}
		return makeStorePath(dir, "text", h, name, refs)
	case htype == nix.SHA256 && ca.IsRecursiveFile():
		return makeStorePath(dir, "source", h, name, refs)
	default:
		if !refs.IsEmpty() {
			return "", fmt.Errorf("compute fixed output path for %s: references not allowed", name)
		}
		h2 := nix.NewHasher(nix.SHA256)
		h2.WriteString("fixed:out:")
		h2.WriteString(methodOfContentAddress(ca).prefix())
		h2.WriteString(h.Base16())
		h2.WriteString(":")
		return makeStorePath(dir, "output:out", h2.SumHash(), name, References{})
	}
}

// References represents a set of references to other store paths
// that a store object contains.
// The zero value is an empty set.
type References struct {
	// Self is true if the store object contains one or more references to itself.
	Self bool
	// Others holds paths of other store objects that the store object references.
	Others sortedset.Set[Path]
}

// IsEmpty reports whether refs represents the empty set.
func (refs References) IsEmpty() bool {
	return !refs.Self && refs.Others.Len() == 0
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
