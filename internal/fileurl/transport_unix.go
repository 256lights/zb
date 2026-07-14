// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

//go:build unix

package fileurl

import (
	"errors"

	"golang.org/x/sys/unix"
)

func isDirectoryError(err error) bool {
	return errors.Is(err, unix.EISDIR)
}
