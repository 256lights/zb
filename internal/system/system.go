// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package system implements parsing of target triples.
package system

import (
	"fmt"
	"runtime"
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
	parts := make([]string, 0, 4)
	for part := range strings.SplitSeq(s, "-") {
		if len(parts) == cap(parts) {
			return System{}, fmt.Errorf("parse system %q: too many hyphen-separated components", s)
		}
		parts = append(parts, part)
	}

	var found [4]bool
	for i, part := range parts {
		found[i] = isKnown(i, part)
	}

	for i := range found {
		if found[i] {
			continue
		}
		for j := 0; j < len(parts); j++ {
			if j < len(found) && found[j] {
				continue
			}
			// Move the component to the target position,
			// pushing any non-fixed components that are in the way to the right.
			// This tends to give good results in the common cases of a forgotten vendor component
			// or a wrongly positioned environment.
			if isKnown(i, parts[j]) {
				switch {
				case i < j:
					// Insert left, pushing the existing components to the right.
					// For example, a-b-i386 -> i386-a-b when moving i386 to the front.
					curr := parts[j]
					parts[j] = ""
					for k := i; curr != ""; k++ {
						for k < len(found) && found[k] {
							k++
						}
						curr, parts[k] = parts[k], curr
					}
				case i > j:
					// Push right by inserting empty components
					// until the component at j reaches the target position i.
					// For example, pc-a -> -pc-a when moving pc to the second position.
					for {
						curr := ""
						for k := j; k < len(parts); {
							curr, parts[k] = parts[k], curr
							if curr == "" {
								break
							}
							k++
							for k < len(found) && found[k] {
								k++
							}
						}
						if curr != "" {
							parts = append(parts, curr)
						}
						j++
						for j < len(found) && found[j] {
							j++
						}
						if j >= i {
							break
						}
					}
				}
				found[i] = true
				break
			}
		}
	}

	if len(parts) > 4 {
		return System{}, fmt.Errorf("parse system %q: trailing components after %s", s, parts[3])
	}
	if len(parts) < 4 {
		parts = parts[:4]
	}
	// If "none" is in the middle component in a three-component triple,
	// treat it as the OS instead of the vendor.
	if found[0] && !found[1] && !found[2] && !found[3] && parts[1] == "none" && parts[2] == "" {
		parts[1], parts[2] = parts[2], parts[1]
	}

	// Expand Cygwin/MinGW first, since that can trigger other implications.
	if isCygwin(parts[2]) {
		if parts[3] != "" {
			return System{}, fmt.Errorf("parse system %q: trailing components after %s", s, parts[2])
		}
		parts[2] = "windows"
		parts[3] = "cygnus"
	} else if isMinGW32(parts[2]) {
		if parts[3] != "" {
			return System{}, fmt.Errorf("parse system %q: trailing components after %s", s, parts[2])
		}
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

func isKnown(i int, s string) bool {
	switch i {
	case 0:
		return Architecture(s).isKnown()
	case 1:
		return Vendor(s).isKnown()
	case 2:
		return OS(s).isKnown() || isCygwin(s) || isMinGW32(s)
	case 3:
		return Environment(s).isKnown()
	default:
		return false
	}
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
