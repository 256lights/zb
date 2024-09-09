// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package backend

import (
	"os/exec"

	"zombiezen.com/go/zb/internal/xmaps"
	"zombiezen.com/go/zb/zbstore"
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
