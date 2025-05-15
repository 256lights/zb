// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package macho

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMagic(t *testing.T) {
	tests := []struct {
		filename           string
		singleArchitecture bool
		universal          bool
	}{
		{
			filename:           "macho-program-aarch64-apple-macos",
			singleArchitecture: true,
		},
		{
			filename:           "macho-program-x86_64-apple-macos",
			singleArchitecture: true,
		},
		{
			filename:  "macho-program-universal",
			universal: true,
		},
	}

	for _, test := range tests {
		data, err := os.ReadFile(filepath.Join("testdata", test.filename))
		if err != nil {
			t.Error(err)
			continue
		}
		if got, want := IsSingleArchitecture(data), test.singleArchitecture; got != want {
			t.Errorf("IsSingleArchitecture(%s) = %t; want %t", test.filename, got, want)
		}
		if got, want := IsUniversal(data), test.universal; got != want {
			t.Errorf("IsUniversal(%s) = %t; want %t", test.filename, got, want)
		}
	}
}
