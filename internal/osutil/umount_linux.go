// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package osutil

import "golang.org/x/sys/unix"

// UnmountNoFollow is the flag to [unix.Unmount] to prevent it from following symbolic links.
const UnmountNoFollow = unix.UMOUNT_NOFOLLOW
