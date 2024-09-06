// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package backend

import (
	"context"
	"testing"
	"time"
)

func TestMutexMap(t *testing.T) {
	// Prevent this test from blocking for more than 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mm := new(mutexMap[int])
	unlock1, err := mm.lock(ctx, 1)
	if err != nil {
		t.Fatal("lock(ctx, 1) on new map failed:", err)
	}

	// Verify that we can acquire a lock on an independent key.
	unlock2, err := mm.lock(ctx, 2)
	if err != nil {
		t.Fatal("lock(ctx, 2) after lock(ctx, 1) failed:", err)
	}

	// Verify that attempting a lock on the same key blocks until Done.
	failFastCtx, cancelFailFast := context.WithTimeout(ctx, 100*time.Millisecond)
	unlock1b, err := mm.lock(failFastCtx, 1)
	cancelFailFast()
	if err == nil {
		t.Error("lock(ctx, 1) acquired without releasing unlock1")
		unlock1b()
	}

	// Verify that unlocking a key allows a subsequent lock to succeed.
	unlock1()
	unlock1, err = mm.lock(ctx, 1)
	if err != nil {
		t.Fatal("lock(ctx, 1) after unlock1 failed:", err)
	}

	// Verify that unlocking a key allows a concurrent lock to succeed.
	lock2Done := make(chan error)
	go func() {
		_, err := mm.lock(ctx, 2)
		lock2Done <- err
	}()
	// Wait for a little bit to make it more likely that the other goroutine hit lock(2).
	timer := time.NewTimer(10 * time.Millisecond)
	select {
	case <-timer.C:
	case <-ctx.Done():
		timer.Stop()
	}
	unlock2()
	if err := <-lock2Done; err != nil {
		t.Error("lock(ctx, 2) with concurrent unlock2 failed:", err)
	}
}
