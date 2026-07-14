// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package fileurl

import (
	"errors"
	"syscall"
)

func isDirectoryError(err error) bool {
	// syscall has a special syscall.EISDIR error code.
	return errors.Is(err, syscall.EISDIR)
}
