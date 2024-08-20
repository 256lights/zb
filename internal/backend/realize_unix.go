// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

//go:build unix

package backend

import (
	"os/exec"

	"golang.org/x/sys/unix"
	"zombiezen.com/go/zb/zbstore"
)

func addBaseEnv(m map[string]string, storeDir zbstore.Directory, workDir string) {
	m["PATH"] = "/path-not-set"
	m["HOME"] = "/home-not-set"
	m["ZB_STORE"] = string(storeDir)
	m["ZB_BUILD_TOP"] = workDir
	m["TMPDIR"] = workDir
	m["TEMPDIR"] = workDir
	m["TMP"] = workDir
	m["TEMP"] = workDir
	m["PWD"] = workDir
	m["TERM"] = "xterm-256color"
}

func setCancelFunc(c *exec.Cmd) {
	c.Cancel = func() error {
		return c.Process.Signal(unix.SIGTERM)
	}
}
