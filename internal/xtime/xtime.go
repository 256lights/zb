// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

// Package xtime provides additional time-related functions.
package xtime

import (
	"cmp"
	"context"
	"time"
)

// Sleep pauses the current goroutine for at least the duration d
// or [context.Context.Done] is closed, whichever comes first.
// Further, if [context.Context.Deadline] will be reached before duration d has passed,
// then Sleep will return [context.DeadlineExceeded] without pausing the current goroutine.
func Sleep(ctx context.Context, d time.Duration) error {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < d {
		return cmp.Or(ctx.Err(), context.DeadlineExceeded)
	}
	t := time.NewTimer(d)
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		t.Stop()
		return ctx.Err()
	}
}
