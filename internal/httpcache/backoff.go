// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package httpcache

import (
	"context"
	"math/rand/v2"
	"time"

	"zb.256lights.llc/pkg/internal/xtime"
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
	return xtime.Sleep(ctx, duration)
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
