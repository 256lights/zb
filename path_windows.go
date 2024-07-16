// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package zb

import "io/fs"

func inode(info fs.FileInfo) uint64 {
	return 0
}

func owner(info fs.FileInfo) (uid, gid uint32) {
	return 0, 0
}
