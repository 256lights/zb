// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

//go:build unix

package main

import (
	"iter"
	"os"
	"os/signal"

	"go4.org/xdgdir"
	"golang.org/x/sys/unix"
)

var interruptSignals = []os.Signal{
	unix.SIGTERM,
	unix.SIGINT,
}

func cacheDir() string {
	return xdgdir.Cache.Path()
}

// systemConfigDirs returns a sequence of configuration directory paths
// in increasing order of preference (i.e. later entries should override earlier entries).
func systemConfigDirs() iter.Seq[string] {
	return func(yield func(string) bool) {
		paths := xdgdir.Config.SearchPaths()
		for i := len(paths) - 1; i >= 0; i-- {
			if !yield(paths[i]) {
				return
			}
		}
	}
}

func ignoreSIGPIPE() {
	signal.Ignore(unix.SIGPIPE)
}
