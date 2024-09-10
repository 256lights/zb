// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

//go:build unix

package main

import (
	"os"

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
