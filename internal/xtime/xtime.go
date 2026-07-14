// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

// Package xtime provides additional time-related functions.
package xtime

import (
	"cmp"
	"context"
	"fmt"
	"math/rand/v2"
	"time"
)

// Sleep pauses the current goroutine for at least the duration d
// or [context.Context.Done] is closed, whichever comes first.
// Further, if [context.Context.Deadline] will be reached before duration d has passed,
// then Sleep will return [context.DeadlineExceeded] without pausing the current goroutine.
func Sleep(ctx context.Context, d time.Duration) error {
	return sleep(ctx, d, time.NewTimer)
}

func sleep(ctx context.Context, d time.Duration, newTimer func(time.Duration) *time.Timer) error {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < d {
		return cmp.Or(ctx.Err(), context.DeadlineExceeded)
	}
	t := newTimer(d)
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		t.Stop()
		return ctx.Err()
	}
}

// A BackoffTimer waits for time durations with random jitter.
type BackoffTimer struct {
	table  []time.Duration
	jitter float64
	timer  *time.Timer
}

// NewBackoffTimer returns a new [*BackoffTimer]
// that uses the maximum jitter coefficient in the range [0,1]
// and a table of base durations to sleep.
// NewBackoffTimer panics if len(table) == 0
// or jitter is not in the range [0,1].
func NewBackoffTimer(table []time.Duration, jitter float64) *BackoffTimer {
	if len(table) == 0 {
		panic("empty table to NewBackoffTimer")
	}
	if jitter < 0 || jitter > 1 {
		panic("jitter out of range")
	}
	return &BackoffTimer{
		table:  table,
		jitter: jitter,
	}
}

// Sleep waits for next duration in its table
// (plus or minus the maximum jitter coefficient)
// or [context.Context.Done] is closed, whichever comes first.
// Further, if [context.Context.Deadline] will be reached before duration d has passed,
// then Sleep will return [context.DeadlineExceeded] without pausing the current goroutine.
func (bt *BackoffTimer) Sleep(ctx context.Context) error {
	if len(bt.table) == 0 {
		return fmt.Errorf("uninitialized backoff timer")
	}
	baseDuration := bt.table[0]
	if len(bt.table) > 1 {
		bt.table = bt.table[1:]
	}
	jitterCoefficient := rand.Float64()*(bt.jitter*2) - bt.jitter
	jitter := time.Duration(float64(baseDuration) * jitterCoefficient)
	return sleep(ctx, baseDuration+jitter, func(d time.Duration) *time.Timer {
		if bt.timer == nil {
			bt.timer = time.NewTimer(d)
		} else {
			bt.timer.Reset(d)
		}
		return bt.timer
	})
}
