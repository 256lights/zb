// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

//go:build unix

package backend

import (
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
	"zb.256lights.llc/pkg/internal/xmaps"
	"zb.256lights.llc/pkg/zbstore"
)

func fillBaseEnv(m map[string]string, storeDir zbstore.Directory, workDir string, cores int) {
	xmaps.SetDefault(m, "HOME", "/home-not-set")
	xmaps.SetDefault(m, "PATH", "/path-not-set")
	xmaps.SetDefault(m, "PWD", workDir)
	xmaps.SetDefault(m, "TEMP", workDir)
	xmaps.SetDefault(m, "TEMPDIR", workDir)
	xmaps.SetDefault(m, "TERM", "xterm-256color")
	xmaps.SetDefault(m, "TMP", workDir)
	xmaps.SetDefault(m, "TMPDIR", workDir)
	xmaps.SetDefault(m, "ZB_BUILD_CORES", strconv.Itoa(cores))
	xmaps.SetDefault(m, "ZB_BUILD_TOP", workDir)
	xmaps.SetDefault(m, "ZB_STORE", string(storeDir))
}

func sysProcAttrForUser(user *BuildUser) *syscall.SysProcAttr {
	if user == nil {
		return nil
	}
	return &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(user.UID),
			Gid: uint32(user.GID),
		},
	}
}

func setCancelFunc(c *exec.Cmd) {
	c.Cancel = func() error {
		return c.Process.Signal(unix.SIGTERM)
	}
}
