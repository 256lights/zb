// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package zbstore

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	posixpath "path"
	"path/filepath"
	"runtime"
	"strings"

	"zb.256lights.llc/pkg/internal/storepath"
	"zb.256lights.llc/pkg/internal/windowspath"
	"zb.256lights.llc/pkg/sets"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nixbase32"
)

// Directory is the absolute path of a zb store.
type Directory string

const (
	// DefaultUnixDirectory is the default zb store directory
	// on Unix-like systems.
	DefaultUnixDirectory Directory = "/opt/zb/store"

	// DefaultWindowsDirectory is the default zb store directory
	// on Windows systems.
	DefaultWindowsDirectory Directory = `C:\zb\store`
)

// DefaultDirectory returns the default zb store directory for the running operating system.
// This will be one of [DefaultUnixDirectory] or [DefaultWindowsDirectory].
func DefaultDirectory() Directory {
	switch localPathStyle() {
	case posixPathStyle:
		return DefaultUnixDirectory
	case windowsPathStyle:
		return DefaultWindowsDirectory
	default:
		panic("unreachable")
	}
}

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
// falling back to [DefaultDirectory] if not set.
func DirectoryFromEnvironment() (Directory, error) {
	dir := os.Getenv("ZB_STORE_DIR")
	if dir == "" {
		return DefaultDirectory(), nil
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
// and the slash-separated relative path inside the store object, if any.
func (dir Directory) ParsePath(path string) (storePath Path, sub string, err error) {
	var cleaned, dirPrefix, tail string
	var sep rune
	style := detectPathStyle(string(dir))
	switch style {
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
	if style == windowsPathStyle {
		sub = windowspath.ToSlash(sub)
	}
	return storePath, sub, nil
}

// IsNative reports whether the directory uses the same path style
// as the running operating system.
func (dir Directory) IsNative() bool {
	return detectPathStyle(string(dir)) == localPathStyle()
}

// MarshalText returns a byte slice of the path
// or an error if it's empty.
func (dir Directory) MarshalText() ([]byte, error) {
	if dir == "" {
		return nil, fmt.Errorf("marshal zb store directory: empty")
	}
	return []byte(dir), nil
}

// UnmarshalText validates and cleans the directory in the same way as [CleanDirectory]
// and stores it into *dir.
func (dir *Directory) UnmarshalText(data []byte) error {
	var err error
	*dir, err = CleanDirectory(string(data))
	if err != nil {
		return err
	}
	return nil
}

// Path is a zb store path:
// the absolute path of a zb store object in the filesystem.
// For example: "/opt/zb/store/s66mzxpvicwk07gjbjfw9izjfa797vsw-hello-2.12.1"
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
	_, isDrv := path.DerivationName()
	return isDrv
}

// Digest returns the digest part of the last element of the path.
func (path Path) Digest() string {
	base := path.Base()
	if len(base) < objectNameDigestLength {
		return ""
	}
	return string(base[:objectNameDigestLength])
}

// Name returns the part of the last element of the path after the digest,
// excluding the separating dash.
func (path Path) Name() string {
	base := path.Base()
	if len(base) <= objectNameDigestLength+len("-") {
		return ""
	}
	return string(base[objectNameDigestLength+len("-"):])
}

// DerivationName returns [Path.Name] with a suffix of [DerivationExt] stripped.
// If the path does not end in [DerivationExt],
// DerivationName returns ("", false).
func (path Path) DerivationName() (drvName string, isDrv bool) {
	drvName, isDrv = strings.CutSuffix(path.Name(), DerivationExt)
	if !isDrv {
		return "", false
	}
	return drvName, true
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

// makeStorePath computes a store path
// according to https://nixos.org/manual/nix/stable/protocols/store-path.
func makeStorePath(dir Directory, typ string, hash nix.Hash, name string, refs References) (Path, error) {
	h := sha256.New()
	io.WriteString(h, typ)
	for _, ref := range refs.Others.All() {
		io.WriteString(h, ":")
		io.WriteString(h, string(ref))
	}
	if refs.Self {
		io.WriteString(h, ":self")
	}
	digest := storepath.MakeDigest(h, string(dir), hash, name)
	return dir.Object(digest + "-" + name)
}

// References represents a set of references to other store paths
// that a store object contains for the purpose of generating a [Path].
// The zero value is an empty set.
type References struct {
	// Self is true if the store object contains one or more references to itself.
	Self bool
	// Others holds paths of other store objects that the store object references.
	Others sets.Sorted[Path]
}

// MakeReferences converts a set of complete store paths into a [References] value.
func MakeReferences(self Path, refSet *sets.Sorted[Path]) References {
	refs := References{
		Self:   refSet.Has(self),
		Others: *refSet.Clone(),
	}
	if refs.Self {
		refs.Others.Delete(self)
	}
	return refs
}

// IsEmpty reports whether refs represents the empty set.
func (refs References) IsEmpty() bool {
	return !refs.Self && refs.Others.Len() == 0
}

// ToSet converts the references to a set of paths
// given the store object's own path.
func (refs References) ToSet(self Path) *sets.Sorted[Path] {
	result := refs.Others.Clone()
	if refs.Self {
		result.Add(self)
	}
	return result
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
