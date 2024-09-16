// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package backend

import (
	"os/exec"

	"zb.256lights.llc/pkg/internal/xmaps"
	"zb.256lights.llc/pkg/zbstore"
)

func fillBaseEnv(m map[string]string, storeDir zbstore.Directory, workDir string) {
	xmaps.SetDefault(m, "PATH", `C:\path-not-set`)
	xmaps.SetDefault(m, "HOME", `C:\home-not-set`)
	xmaps.SetDefault(m, "ZB_STORE", string(storeDir))
	xmaps.SetDefault(m, "ZB_BUILD_TOP", workDir)
	// TODO(someday): More.
}

func setCancelFunc(c *exec.Cmd) {
	// Default behavior of exec.CommandContext is fine, no-op.
}
