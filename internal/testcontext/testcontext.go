// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package testcontext provides a function that creates a test-scoped [context.Context].
package testcontext

import (
	"context"
	"testing"
	"time"

	"zombiezen.com/go/log/testlog"
)

// New returns a context that associates the test logger with the test
// and obeys the test's deadline if present.
func New(tb testing.TB) context.Context {
	ctx := tb.Context()
	if d, ok := deadline(tb); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, d)
		tb.Cleanup(cancel)
	}
	ctx = testlog.WithTB(ctx, tb)
	return ctx
}

func deadline(x any) (deadline time.Time, ok bool) {
	d, ok := x.(interface {
		Deadline() (deadline time.Time, ok bool)
	})
	if !ok {
		return time.Time{}, false
	}
	return d.Deadline()
}
