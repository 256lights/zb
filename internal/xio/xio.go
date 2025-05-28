// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

// Package xio provides I/O utilities.
package xio

import "io"

// A WriteCounter counts the number of bytes written to it.
type WriteCounter int64

// Write increments wc by len(p).
func (wc *WriteCounter) Write(p []byte) (n int, err error) {
	*wc += WriteCounter(len(p))
	return len(p), nil
}

// WriteString increments wc by len(s).
func (wc *WriteCounter) WriteString(s string) (n int, err error) {
	*wc += WriteCounter(len(s))
	return len(s), nil
}

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
