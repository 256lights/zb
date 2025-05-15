// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package macho

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestReadUniversalHeader(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "macho-program-universal"))
	if err != nil {
		t.Fatal(err)
	}

	got, err := ReadUniversalHeader(bytes.NewReader(data))
	if err != nil {
		t.Error("ReadUniversalHeader:", err)
	}
	want := []UniversalFileEntry{
		{
			CPU:        16777223,
			CPUSubtype: 3,
			Offset:     4096,
			Size:       4248,
			Alignment:  12,
		},
		{
			CPU:        16777228,
			CPUSubtype: 0,
			Offset:     16384,
			Size:       16824,
			Alignment:  14,
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("-want +got:\n%s", diff)
	}
}
