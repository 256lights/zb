// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package httpcache

import (
	"cmp"
	"context"
	"math/rand/v2"
	"time"
)

// A backoffTimer waits for progressively longer time durations
// with random jitter.
// The zero value is ready to use.
type backoffTimer struct {
	i int
}

// wait waits for a random, but generally increasing, amount of time
// or until the ctx.Done() channel is closed,
// whichever comes first.
// If the amount of time that the timer would wait exceeds ctx.Deadline(),
// then wait returns [context.DeadlineExceeded] without waiting.
// wait returns an error if the waiting was interrupted by ctx's deadline or cancellation.
func (bt *backoffTimer) wait(ctx context.Context) error {
	var baseDuration time.Duration
	if bt.i < len(backoffTable) {
		baseDuration = backoffTable[bt.i]
		bt.i++
	} else {
		baseDuration = backoffTable[len(backoffTable)-1]
	}
	const jitterPercentage = 0.25
	jitterCoefficient := rand.Float64()*(jitterPercentage*2) - jitterPercentage
	jitter := time.Duration(float64(baseDuration) * jitterCoefficient)
	duration := baseDuration + jitter
	if deadline, ok := ctx.Deadline(); ok && duration >= time.Until(deadline) {
		return cmp.Or(ctx.Err(), context.DeadlineExceeded)
	}
	t := time.NewTimer(duration)
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		t.Stop()
		return ctx.Err()
	}
}

var backoffTable = [...]time.Duration{
	50 * time.Millisecond,
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	500 * time.Millisecond,
	1000 * time.Millisecond,
	1000 * time.Millisecond,
	1000 * time.Millisecond,
	1000 * time.Millisecond,
	1000 * time.Millisecond,
	2500 * time.Millisecond,
	5000 * time.Millisecond,
}
