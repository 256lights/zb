// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package system implements parsing of target triples.
package system

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
)

const Unknown = "unknown"

// System represents a platform target for zb.
type System struct {
	Arch   Architecture
	Vendor Vendor
	OS     OS
	Env    Environment
}

// Parse parses a system string into a [System].
// If Parse does not return an error,
// it guarantees that all of the System's fields will be filled in.
func Parse(s string) (System, error) {
	var parts [4]string
	var numParts int
	for part := range strings.SplitSeq(s, "-") {
		if numParts == cap(parts) {
			return System{}, fmt.Errorf("parse system %q: trailing components after %s", s, parts[numParts-1])
		}
		parts[numParts] = part
		numParts++
	}
	isNonEmpty := func(s string) bool { return s != "" }
	for i, part := range parts[:len(parts)-1] {
		if (isCygwin(part) || isMinGW32(part)) && slices.ContainsFunc(parts[i+1:], isNonEmpty) {
			return System{}, fmt.Errorf("parse system %q: trailing components after %s", s, parts[1])
		}
	}
	switch numParts {
	case 1:
		return System{}, fmt.Errorf("parse system %q: missing operating system", s)
	case 2:
		parts[1], parts[2] = "", parts[1]
	case 3:
		if OS(parts[1]).isKnown() || parts[1] == "none" {
			parts[1], parts[2], parts[3] = "", parts[1], parts[2]
		}
	}

	// Expand Cygwin/MinGW first, since that can trigger other implications.
	if isCygwin(parts[2]) {
		parts[2] = "windows"
		parts[3] = "cygnus"
	} else if isMinGW32(parts[2]) {
		parts[2] = "windows"
		parts[3] = "gnu"
	}

	// Now fill in the system based on the parts.
	sys := System{
		Arch:   Unknown,
		Vendor: Unknown,
		OS:     Unknown,
		Env:    Unknown,
	}
	if got := Architecture(parts[0]); got != "" {
		sys.Arch = got
	}
	if got := OS(parts[2]); got != "" {
		sys.OS = got
	}
	if got := Vendor(parts[1]); got != "" {
		sys.Vendor = got
	} else {
		sys.Vendor = sys.OS.defaultVendor()
	}
	if got := Environment(parts[3]); got != "" {
		sys.Env = got
	} else {
		sys.Env = sys.OS.defaultEnvironment()
	}
	return sys, nil
}

func isCygwin(s string) bool {
	return strings.HasPrefix(s, "cygwin")
}

func isMinGW32(s string) bool {
	return strings.HasPrefix(s, "mingw")
}

// Current returns a [System] value for the current process's execution environment.
func Current() System {
	var sys System
	switch runtime.GOARCH {
	case "386":
		sys.Arch = "i686"
	case "amd64":
		sys.Arch = "x86_64"
	case "arm":
		sys.Arch = "arm"
	case "arm64":
		sys.Arch = "aarch64"
	case "riscv":
		sys.Arch = "riscv32"
	case "riscv64":
		sys.Arch = "riscv64"
	default:
		panic("unknown GOARCH=" + runtime.GOARCH)
	}
	switch runtime.GOOS {
	case "linux":
		sys.Vendor = Unknown
		sys.OS = "linux"
		sys.Env = Unknown
	case "android":
		sys.Vendor = Unknown
		sys.OS = "linux"
		if sys.Arch == "arm" {
			sys.Env = "androideabi"
		} else {
			sys.Env = "android"
		}
	case "darwin":
		sys.Vendor = "apple"
		sys.OS = "macos"
		sys.Env = Unknown
	case "windows":
		sys.Vendor = "pc"
		sys.OS = "windows"
		sys.Env = "msvc"
	case "ios":
		sys.Vendor = "apple"
		sys.OS = "ios"
		sys.Env = Unknown
	default:
		panic("unknown GOOS=" + runtime.GOOS)
	}
	return sys
}

// String returns sys as a string that can be passed to [Parse].
func (sys System) String() string {
	switch {
	case sys.OS == "linux" && sys.Vendor.IsUnknown() && sys.Env.IsUnknown():
		// All parsers should know about "linux" as an OS.
		// If vendor and environment are unknown (a very common case),
		// we can drop them it should parse correctly.
		return sys.Arch.String() + "-" + sys.OS.String()
	case sys.OS == "windows" && sys.Env == "cygnus":
		// Abbreviate Cygwin.
		return sys.Arch.String() + "-" + sys.Vendor.String() + "-cygwin"
	case Environment(sys.Env.String()) == sys.OS.defaultEnvironment():
		// Prefer showing a triple.
		return sys.Arch.String() + "-" + sys.Vendor.String() + "-" + sys.OS.String()
	default:
		// Full 4-part "triple".
		return sys.Arch.String() + "-" + sys.Vendor.String() + "-" + sys.OS.String() + "-" + sys.Env.String()
	}
}
