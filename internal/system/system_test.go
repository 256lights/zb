// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package system

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tailscale/hujson"
	"zb.256lights.llc/pkg/internal/xmaps"
)

func TestSystem(t *testing.T) {
	goldenPath := filepath.Join("testdata", "known_triples.jwcc")
	goldenJSON, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	goldenJSON, err = hujson.Standardize(goldenJSON)
	if err != nil {
		t.Fatalf("parse %s: %v", goldenPath, err)
	}

	type testCase struct {
		Arch   string
		Vendor string
		OS     string
		Env    string

		IsX86      bool
		IsARM      bool
		Is32Bit    bool
		Is64Bit    bool
		IsMacOS    bool
		IsiOS      bool
		IsDarwin   bool
		IsLinux    bool
		IsWindows  bool
		Normalized string
	}
	var tests map[string]testCase
	if err := json.Unmarshal(goldenJSON, &tests); err != nil {
		t.Fatalf("parse %s: %v", goldenPath, err)
	}

	for s, want := range xmaps.Sorted(tests) {
		got, err := Parse(s)
		if err != nil {
			t.Errorf("Parse(%q): %v", s, err)
			continue
		}
		if got, want := got.Arch, Architecture(want.Arch); got != want {
			t.Errorf("Parse(%q).Arch = %q; want %q", s, got, want)
		}
		if got, want := got.Vendor, Vendor(want.Vendor); got != want {
			t.Errorf("Parse(%q).Vendor = %q; want %q", s, got, want)
		}
		if got, want := got.OS, OS(want.OS); got != want {
			t.Errorf("Parse(%q).OS = %q; want %q", s, got, want)
		}
		if got, want := got.Env, Environment(want.Env); got != want {
			t.Errorf("Parse(%q).Env = %q; want %q", s, got, want)
		}
		if got, want := got.Arch.IsX86(), want.IsX86; got != want {
			t.Errorf("Parse(%q).Arch.IsX86() = %t; want %t", s, got, want)
		}
		if got, want := got.Arch.IsARM(), want.IsARM; got != want {
			t.Errorf("Parse(%q).Arch.IsARM() = %t; want %t", s, got, want)
		}
		if got, want := got.Arch.Is32Bit(), want.Is32Bit; got != want {
			t.Errorf("Parse(%q).Arch.Is32Bit() = %t; want %t", s, got, want)
		}
		if got, want := got.Arch.Is64Bit(), want.Is64Bit; got != want {
			t.Errorf("Parse(%q).Arch.Is64Bit() = %t; want %t", s, got, want)
		}
		if got, want := got.OS.IsMacOS(), want.IsMacOS; got != want {
			t.Errorf("Parse(%q).OS.IsMacOS() = %t; want %t", s, got, want)
		}
		if got, want := got.OS.IsiOS(), want.IsiOS; got != want {
			t.Errorf("Parse(%q).OS.IsiOS() = %t; want %t", s, got, want)
		}
		if got, want := got.OS.IsDarwin(), want.IsDarwin; got != want {
			t.Errorf("Parse(%q).OS.IsDarwin() = %t; want %t", s, got, want)
		}
		if got, want := got.OS.IsLinux(), want.IsLinux; got != want {
			t.Errorf("Parse(%q).OS.IsLinux() = %t; want %t", s, got, want)
		}
		if got, want := got.OS.IsWindows(), want.IsWindows; got != want {
			t.Errorf("Parse(%q).OS.IsWindows() = %t; want %t", s, got, want)
		}
		if got, want := got.String(), want.Normalized; got != want {
			t.Errorf("Parse(%q).String() = %q; want %q", s, got, want)
		}
	}

	badPath := filepath.Join("testdata", "bad_triples.jwcc")
	badJSON, err := os.ReadFile(badPath)
	if err != nil {
		t.Fatal(err)
	}
	badJSON, err = hujson.Standardize(badJSON)
	if err != nil {
		t.Fatalf("parse %s: %v", badPath, err)
	}
	var badTests []string
	if err := json.Unmarshal(badJSON, &badTests); err != nil {
		t.Fatalf("parse %s: %v", badPath, err)
	}
	for _, test := range badTests {
		if got, err := Parse(test); err == nil {
			t.Errorf("Parse(%q) = %v, <nil>; want error", test, got)
		}
	}
}

func TestCurrent(t *testing.T) {
	got := Current()
	t.Logf("Current() = %q", got)
	if got.OS == "" || got.Vendor == "" || got.Arch == "" || got.Env == "" {
		t.Errorf("Current() = %+v (should not have empty fields)", got)
	}
}
