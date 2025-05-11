// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

// Package xio provides I/O utilities.
package xio

import "io"

type onceCloser struct {
	c      io.Closer
	err    error
	closed bool
}

// CloseOnce returns an [io.Closer] that calls c at most once.
func CloseOnce(c io.Closer) io.Closer {
	return &onceCloser{c: c}
}

func (oc *onceCloser) Close() error {
	if !oc.closed {
		oc.err = oc.c.Close()
		oc.closed = true
	}
	return oc.err
}
