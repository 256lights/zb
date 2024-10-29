// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package system implements parsing of "system" values.
package system

import (
	"fmt"
	"runtime"
	"strings"
)

// System represents a platform target for zb.
type System struct {
	OS   string
	Arch string
	ABI  string
}

// Parse parses a system string into a [System].
// If Parse does not return an error,
// it guarantees that all of the System's fields will be filled in.
func Parse(s string) (System, error) {
	var sys System
	parts := strings.Split(s, "-")
	switch {
	case len(parts) < 2:
		return System{}, fmt.Errorf("parse system %q: not enough hyphen-separated components", s)
	case len(parts) == 2 && parts[1] == "cygwin":
		sys = System{
			OS:   "windows",
			Arch: parts[0],
			ABI:  "cygnus",
		}
	case len(parts) == 2 && parts[1] == "windows":
		sys = System{
			OS:   "windows",
			Arch: parts[0],
			ABI:  "msvc",
		}
	case len(parts) == 2:
		sys = System{
			Arch: parts[0],
			OS:   parts[1],
		}
	case len(parts) == 3 && parts[1] == "linux" && parts[2] == "gnu":
		sys = System{
			Arch: parts[0],
			OS:   parts[1],
			ABI:  parts[2],
		}
	case len(parts) == 3:
		return System{}, fmt.Errorf("parse system %q: invalid triple", s)
	case len(parts) == 4 && parts[1] == osVendor(parts[2]):
		sys = System{
			Arch: parts[0],
			OS:   parts[2],
			ABI:  parts[3],
		}
	case len(parts) == 4:
		return System{}, fmt.Errorf("parse system %q: invalid vendor %q", s, parts[1])
	default:
		return System{}, fmt.Errorf("parse system %q: too many hyphen-separated components", s)
	}
	sys, err := sys.Fill()
	if err != nil {
		return System{}, fmt.Errorf("parse system %q: %v", s, err)
	}
	return sys, nil
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
		sys.OS = "linux"
		sys.ABI = "gnu"
	case "android":
		sys.OS = "linux"
		if sys.Arch == "arm" {
			sys.ABI = "androideabi"
		} else {
			sys.ABI = "android"
		}
	case "darwin":
		sys.OS = "macos"
		sys.ABI = "unknown"
	case "windows":
		sys.OS = "windows"
		sys.ABI = "msvc"
	case "ios":
		sys.OS = "ios"
		sys.ABI = "unknown"
	default:
		panic("unknown GOOS=" + runtime.GOOS)
	}
	return sys
}

// Fill returns a [System] with any missing fields filled in,
// if they can be inferred.
// An error is returned otherwise.
func (sys System) Fill() (System, error) {
	switch sys.Arch {
	case "":
		return sys, fmt.Errorf("architecture missing")
	case "i686", "x86_64", "arm", "aarch64", "riscv32", "riscv64":
		// Valid.
	default:
		return sys, fmt.Errorf("unknown architecture %q", sys.Arch)
	}

	switch sys.OS {
	case "":
		return sys, fmt.Errorf("os missing")
	case "linux", "macos", "ios", "windows":
		// Valid.
	default:
		return sys, fmt.Errorf("unknown os %q", sys.OS)
	}

	switch sys.ABI {
	case "":
		switch sys.OS {
		case "linux":
			// TODO(someday): Specific versions of ARM and PPC.
			sys.ABI = "gnu"
		case "windows":
			sys.ABI = "msvc"
		default:
			sys.ABI = "unknown"
		}
	case "gnu", "musl", "android", "androideabi":
		if sys.OS != "linux" {
			return sys, fmt.Errorf("cannot use %s abi with %s", sys.ABI, sys.OS)
		}
	case "msvc", "cygnus":
		if sys.OS != "windows" {
			return sys, fmt.Errorf("cannot use %s abi with %s", sys.ABI, sys.OS)
		}
	case "unknown":
		// Valid.
	default:
		return sys, fmt.Errorf("unknown abi %q", sys.ABI)
	}

	return sys, nil
}

// String returns sys as a string that can be passed to [Parse].
func (sys System) String() string {
	switch {
	case isDarwin(sys.OS) && sys.ABI == "unknown" && sys.Arch != "":
		return sys.Arch + "-" + sys.OS
	case sys.OS == "linux" && sys.ABI == "gnu" && sys.Arch != "":
		return sys.Arch + "-linux"
	case sys.OS == "windows" && sys.ABI == "msvc" && sys.Arch != "":
		return sys.Arch + "-windows"
	case sys.OS == "windows" && sys.ABI == "cygnus" && sys.Arch != "":
		return sys.Arch + "-cygwin"
	default:
		return sys.Arch + "-" + osVendor(sys.OS) + "-" + sys.OS + "-" + sys.ABI
	}
}

// IsIntel reports whether sys uses an Intel-based instruction set.
func (sys System) IsIntel() bool {
	return sys.IsIntel32() || sys.IsIntel64()
}

// IsIntel32 reports whether sys uses a 32-bit Intel-based instruction set.
func (sys System) IsIntel32() bool {
	return sys.Arch == "i686"
}

// IsIntel64 reports whether sys uses a 64-bit Intel-based instruction set.
func (sys System) IsIntel64() bool {
	return sys.Arch == "x86_64"
}

// IsARM reports whether sys uses an ARM-based instruction set.
func (sys System) IsARM() bool {
	return sys.IsARM32() || sys.IsARM64()
}

// IsARM32 reports whether sys uses a 32-bit ARM-based instruction set.
func (sys System) IsARM32() bool {
	return sys.Arch == "arm"
}

// IsARM64 reports whether sys uses a 64-bit ARM-based instruction set.
func (sys System) IsARM64() bool {
	return sys.Arch == "aarch64"
}

func osVendor(os string) string {
	switch {
	case isDarwin(os):
		return "apple"
	case os == "windows":
		return "pc"
	default:
		return "unknown"
	}
}

func isDarwin(os string) bool {
	return os == "macos" || os == "ios"
}
