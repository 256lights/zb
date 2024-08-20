// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package backend

import (
	"os/exec"

	"zombiezen.com/go/zb/zbstore"
)

func addBaseEnv(m map[string]string, storeDir zbstore.Directory, workDir string) {
	m["PATH"] = `C:\path-not-set`
	m["HOME"] = `C:\home-not-set`
	m["ZB_STORE"] = string(storeDir)
	m["ZB_BUILD_TOP"] = workDir
	// TODO(someday): More.
}

func setCancelFunc(c *exec.Cmd) {
	// Default behavior of exec.CommandContext is fine, no-op.
}
