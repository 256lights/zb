// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package macho

import (
	"bytes"
	"encoding/binary"
	"fmt"
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
		name        string
		dataFile    string
		startOffset int64
		imageSize   int
		want        FileHeader
	}{
		{
			name:     "AArch64",
			dataFile: "macho-program-aarch64-apple-macos",
			want:     aarch64FileHeader,
		},
		{
			name:        "UniversalAArch64",
			dataFile:    "macho-program-universal",
			startOffset: 16384,
			imageSize:   16824,
			want:        aarch64FileHeader,
		},
		{
			name:     "X86_64",
			dataFile: "macho-program-x86_64-apple-macos",
			want:     x86_64FileHeader,
		},
		{
			name:        "UniversalX86_64",
			dataFile:    "macho-program-universal",
			startOffset: 4096,
			imageSize:   4248,
			want:        x86_64FileHeader,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, err := openTestFile(test.dataFile, test.startOffset, test.imageSize)
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

func openTestFile(name string, startOffset int64, size int) ([]byte, error) {
	path := filepath.Join("testdata", name)
	if size == 0 {
		return os.ReadFile(path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data := make([]byte, size)
	if _, err := f.ReadAt(data, startOffset); err != nil {
		return nil, fmt.Errorf("read %s: %v", path, err)
	}
	return data, nil
}
