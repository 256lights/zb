// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package backend

import (
	"context"
	"fmt"
)

func defaultSystemCertFile() (string, error) {
	return "", nil
}

func runSandboxed(ctx context.Context, invocation *builderInvocation) error {
	return fmt.Errorf("TODO(someday)")
}
