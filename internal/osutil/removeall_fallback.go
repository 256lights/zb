// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

//go:build !(linux || darwin)

package osutil

import "os"

func removeAll(path string) error {
	return os.RemoveAll(path)
}
