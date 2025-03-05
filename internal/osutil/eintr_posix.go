// Copyright 2025 The zb Authors
// Copyright 2009 The Go Authors. All rights reserved.
// SPDX-License-Identifier: BSD 3-Clause
//
// This is a copy of ignoringEINTR and ignoringEINTR2
// from https://cs.opensource.google/go/go/+/refs/tags/go1.24.1:src/os/file_posix.go

//go:build unix || windows

package osutil

import "syscall"

// ignoringEINTR makes a function call and repeats it if it returns an
// EINTR error. This appears to be required even though we install all
// signal handlers with SA_RESTART: see #22838, #38033, #38836, #40846.
// Also #20400 and #36644 are issues in which a signal handler is
// installed without setting SA_RESTART. None of these are the common case,
// but there are enough of them that it seems that we can't avoid
// an EINTR loop.
func ignoringEINTR(fn func() error) error {
	for {
		err := fn()
		if err != syscall.EINTR {
			return err
		}
	}
}

// ignoringEINTR2 is [ignoringEINTR], but returning an additional value.
func ignoringEINTR2[T any](fn func() (T, error)) (T, error) {
	for {
		v, err := fn()
		if err != syscall.EINTR {
			return v, err
		}
	}
}
