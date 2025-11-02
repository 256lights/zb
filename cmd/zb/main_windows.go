// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"iter"
	"os"
)

var interruptSignals = []os.Signal{os.Interrupt}

func cacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return dir
}

// systemConfigDirs returns a sequence of configuration directory paths
// in increasing order of preference (i.e. later entries should override earlier entries).
func systemConfigDirs() iter.Seq[string] {
	return func(yield func(string) bool) {
		if dir, err := os.UserConfigDir(); err == nil {
			yield(dir)
		}
	}
}

func ignoreSIGPIPE() {}
