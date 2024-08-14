// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package system

import "testing"

func TestSystem(t *testing.T) {
	tests := []struct {
		name string
		sys  System
	}{
		{"i686-linux", System{OS: "linux", Arch: "i686", ABI: "gnu"}},
		{"x86_64-linux", System{OS: "linux", Arch: "x86_64", ABI: "gnu"}},
		{"aarch64-linux", System{OS: "linux", Arch: "aarch64", ABI: "gnu"}},
		{"x86_64-unknown-linux-musl", System{OS: "linux", Arch: "x86_64", ABI: "musl"}},
		{"x86_64-macos", System{OS: "macos", Arch: "x86_64", ABI: "unknown"}},
		{"aarch64-macos", System{OS: "macos", Arch: "aarch64", ABI: "unknown"}},
		{"arm-ios", System{OS: "ios", Arch: "arm", ABI: "unknown"}},
		{"aarch64-ios", System{OS: "ios", Arch: "aarch64", ABI: "unknown"}},
		{"i686-windows", System{OS: "windows", Arch: "i686", ABI: "msvc"}},
		{"x86_64-windows", System{OS: "windows", Arch: "x86_64", ABI: "msvc"}},
		{"aarch64-windows", System{OS: "windows", Arch: "aarch64", ABI: "msvc"}},
	}

	t.Run("Parse", func(t *testing.T) {
		for _, test := range tests {
			got, err := Parse(test.name)
			if got != test.sys || err != nil {
				t.Errorf("Parse(%q) = %+v, %v; want %+v, <nil>", test.name, got, err, test.sys)
			}
		}
	})

	t.Run("String", func(t *testing.T) {
		for _, test := range tests {
			got := test.sys.String()
			if got != test.name {
				t.Errorf("%+v.String() = %q; want %q", test.sys, got, test.name)
			}
		}
	})
}

func TestCurrent(t *testing.T) {
	got := Current()
	if got.OS == "" || got.Arch == "" || got.ABI == "" {
		t.Errorf("Current() = %+v (should not have empty fields)", got)
	}
	// This has the side-effect of validating all fields.
	if _, err := got.Fill(); err != nil {
		t.Errorf("Current().Fill(): %v", err)
	}
}
