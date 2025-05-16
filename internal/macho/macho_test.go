// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package macho

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

var aarch64FileHeader = FileHeader{
	ByteOrder:    binary.LittleEndian,
	AddressWidth: 64,
	Type:         TypeExec,

	LoadCommandCount:      15,
	LoadCommandRegionSize: 760,
}

var x86_64FileHeader = FileHeader{
	ByteOrder:    binary.LittleEndian,
	AddressWidth: 64,
	Type:         TypeExec,

	LoadCommandCount:      14,
	LoadCommandRegionSize: 744,
}

func TestReadFileHeader(t *testing.T) {
	tests := []struct {
		name     string
		dataFile string
		want     FileHeader
	}{
		{
			name:     "AArch64",
			dataFile: "macho-program-aarch64-apple-macos",
			want:     aarch64FileHeader,
		},
		{
			name:     "X86_64",
			dataFile: "macho-program-x86_64-apple-macos",
			want:     x86_64FileHeader,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", test.dataFile))
			if err != nil {
				t.Fatal(err)
			}

			r := bytes.NewReader(data)
			gotHeader, err := ReadFileHeader(r)
			if err != nil {
				t.Fatal("ReadFileHeader:", err)
			}

			if diff := cmp.Diff(&test.want, gotHeader); diff != "" {
				t.Errorf("header (-want +got):\n%s", diff)
			}
		})
	}
}
