// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

//go:build unix

package backend

import (
	"os/exec"

	"golang.org/x/sys/unix"
	"zombiezen.com/go/zb/internal/xmaps"
	"zombiezen.com/go/zb/zbstore"
)

func fillBaseEnv(m map[string]string, storeDir zbstore.Directory, workDir string) {
	xmaps.SetDefault(m, "PATH", "/path-not-set")
	xmaps.SetDefault(m, "HOME", "/home-not-set")
	xmaps.SetDefault(m, "ZB_STORE", string(storeDir))
	xmaps.SetDefault(m, "ZB_BUILD_TOP", workDir)
	xmaps.SetDefault(m, "TMPDIR", workDir)
	xmaps.SetDefault(m, "TEMPDIR", workDir)
	xmaps.SetDefault(m, "TMP", workDir)
	xmaps.SetDefault(m, "TEMP", workDir)
	xmaps.SetDefault(m, "PWD", workDir)
	xmaps.SetDefault(m, "TERM", "xterm-256color")
}

func setCancelFunc(c *exec.Cmd) {
	c.Cancel = func() error {
		return c.Process.Signal(unix.SIGTERM)
	}
}
