// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package main

import "os"

var interruptSignals = []os.Signal{os.Interrupt}

func cacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return dir
}
