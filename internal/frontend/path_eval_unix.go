// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

//go:build !windows

package frontend

import (
	"io/fs"
	"syscall"
)

func inode(info fs.FileInfo) uint64 {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	return st.Ino
}

func owner(info fs.FileInfo) (uid, gid uint32) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0
	}
	return st.Uid, st.Gid
}
