// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

//go:build unix

package main

import "go4.org/xdgdir"

func cacheDir() string {
	return xdgdir.Cache.Path()
}
