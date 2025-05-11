// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package system

import "strings"

// OS is the name of an operating system of a [System].
// The empty string is treated the same as [Unknown].
type OS string

// IsVendor reports whether vendor is the empty string or [Unknown].
func (os OS) IsUnknown() bool {
	return os == "" || os == Unknown
}

func (os OS) isKnown() bool {
	return os.IsLinux() || os.IsWindows() || os.IsDarwin()
}

// String returns string(os) or [Unknown] if os is the empty string.
func (os OS) String() string {
	if os == "" {
		return Unknown
	}
	return string(os)
}

// IsDarwin reports whether sys.OS is in the Darwin family of operating systems
// (e.g. macOS, iOS, etc.).
func (os OS) IsDarwin() bool {
	return os.IsMacOS() || os.IsiOS()
}

// IsMacOS reports whether sys.OS indicates macOS.
func (os OS) IsMacOS() bool {
	return strings.HasPrefix(string(os), "darwin") || strings.HasPrefix(string(os), "macos")
}

// IsiOS reports whether sys.OS indicates iOS.
func (os OS) IsiOS() bool {
	return strings.HasPrefix(string(os), "ios")
}

// IsWindows reports whether sys.OS indicates Windows.
func (os OS) IsWindows() bool {
	return strings.HasPrefix(string(os), "windows") ||
		strings.HasPrefix(string(os), "win32")
}

// IsLinux reports whether sys.OS indicates Linux.
func (os OS) IsLinux() bool {
	return strings.HasPrefix(string(os), "linux")
}

func (os OS) defaultVendor() Vendor {
	switch {
	case os.IsDarwin():
		return "apple"
	case os.IsWindows():
		return "pc"
	default:
		return Unknown
	}
}

func (os OS) defaultEnvironment() Environment {
	switch {
	case os.IsWindows():
		return "msvc"
	default:
		return Unknown
	}
}
