// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

//go:build unix && !linux

package osutil

// UnmountNoFollow is the flag to [golang.org/x/sys/unix.Unmount] to prevent it from following symbolic links.
const UnmountNoFollow = 0
