// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"

	"zb.256lights.llc/pkg/zbstore"
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
	dir := t.TempDir()
	var paths [2]string
	paths[0] = filepath.Join(dir, "config1.jwcc")
	if err := os.WriteFile(paths[0], []byte(`{"debug": true, "storeDirectory": "/foo"}`+"\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	paths[1] = filepath.Join(dir, "config2.jwcc")
	if err := os.WriteFile(paths[1], []byte(`{"storeDirectory": "/bar"}`+"\n"), 0o666); err != nil {
		t.Fatal(err)
	}

	g := new(globalConfig)
	err := g.mergeFiles(func(yield func(string) bool) {
		for _, path := range paths {
			if !yield(path) {
				return
			}
		}
	})
	if err != nil {
		t.Error("mergeFiles:", err)
	}
	if !g.Debug {
		t.Error("g.Debug = false; want true (config1.jwcc ignored)")
	}
	if got, want := g.Directory, zbstore.Directory("/bar"); got != want {
		t.Errorf("g.Directory = %q; want %q", got, want)
	}
}
