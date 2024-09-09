// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package main

import "os"

func cacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return dir
}
