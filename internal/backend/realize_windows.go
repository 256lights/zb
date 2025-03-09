// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package backend

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"syscall"

	"zb.256lights.llc/pkg/internal/xmaps"
	"zb.256lights.llc/pkg/zbstore"
)

func fillBaseEnv(m map[string]string, storeDir zbstore.Directory, workDir string, cores int) {
	xmaps.SetDefault(m, "HOME", `C:\home-not-set`)
	xmaps.SetDefault(m, "PATH", `C:\path-not-set`)
	xmaps.SetDefault(m, "TEMP", workDir)
	xmaps.SetDefault(m, "TMP", workDir)
	xmaps.SetDefault(m, "ZB_STORE", string(storeDir))
	xmaps.SetDefault(m, "ZB_BUILD_CORES", strconv.Itoa(cores))
	xmaps.SetDefault(m, "ZB_BUILD_TOP", workDir)
	// TODO(someday): More.
}

func sysProcAttrForUser(user *BuildUser) *syscall.SysProcAttr {
	return nil
}

func setCancelFunc(c *exec.Cmd) {
	// Default behavior of exec.CommandContext is fine, no-op.
}

func defaultSystemCertFile() (string, error) {
	return "", nil
}

func runSandboxed(ctx context.Context, invocation *builderInvocation) error {
	return fmt.Errorf("TODO(someday)")
}
