// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package testcontext

import (
	"context"
	"testing"
	"time"

	"zombiezen.com/go/log/testlog"
)

// New returns a context that associates the test logger with the test
// and obeys the test's deadline if present.
func New(tb testing.TB) (context.Context, context.CancelFunc) {
	// TODO(someday): Go 1.24 includes a Context method
	// that cancels the context when the test function returns.
	ctx := context.Background()
	cancel := context.CancelFunc(func() {})
	if d, ok := deadline(tb); ok {
		ctx, cancel = context.WithDeadline(ctx, d)
	}
	ctx = testlog.WithTB(ctx, tb)
	return ctx, cancel
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
