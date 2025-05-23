// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

//go:build unix

package main

import (
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

func ignoreSIGPIPE() {
	signal.Ignore(unix.SIGPIPE)
}
