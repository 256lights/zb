// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestDefaultGlobalConfig(t *testing.T) {
	got := defaultGlobalConfig()
	if got.Directory == "" {
		t.Errorf("defaultGlobalConfig().Directory is empty")
	}
	if got.StoreSocket == "" {
		t.Errorf("defaultGlobalConfig().Directory is empty")
	}
}

func TestGlobalConfigMergeFiles(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  globalConfig
	}{
		{
			name: "MergeScalar",
			files: []string{
				`{"debug": true, "storeDirectory": "/foo"}` + "\n",
				`{"storeDirectory": "/bar"}` + "\n",
			},
			want: globalConfig{
				Debug:     true,
				Directory: "/bar",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			paths := make([]string, len(test.files))
			for i, content := range test.files {
				path := filepath.Join(dir, fmt.Sprintf("config%d.jwcc", i+1))
				if err := os.WriteFile(path, []byte(content), 0o666); err != nil {
					t.Fatal(err)
				}
				paths[i] = path
			}

			got := new(globalConfig)
			err := got.mergeFiles(slices.Values(paths))
			if err != nil {
				t.Error("mergeFiles:", err)
			}
			if diff := cmp.Diff(&test.want, got, cmp.AllowUnexported(stringAllowList{})); diff != "" {
				t.Errorf("-want +got:\n%s", diff)
			}
		})
	}
}
