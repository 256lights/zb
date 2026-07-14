// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

//go:build !unix && !windows

package fileurl

func isDirectoryError(err error) bool {
	return false
}
