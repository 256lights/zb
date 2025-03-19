// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

//go:build !unix

package osutil

// O_NOFOLLOW is a flag to [os.OpenFile] to not follow a symbolic link
// on the final path component.
// It will be zero on platforms that do not support it.
const O_NOFOLLOW = 0
