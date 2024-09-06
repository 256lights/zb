// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package backend

import (
	"context"
	"sync"
)

// A mutexMap is a map of mutexes.
// The zero value is an empty map.
type mutexMap[T comparable] struct {
	mu sync.Mutex
	m  map[T]<-chan struct{}
}

// lock waits until it can either acquire the mutex for k
// or ctx.Done is closed.
// If lock acquires the mutex, it returns a function that will unlock the mutex and a nil error.
// Otherwise, lock returns a nil unlock function and the result of ctx.Err().
// Until unlock is called, all calls to mm.lock(k) for the same k will block.
// Multiple goroutines can call lock simultaneously.
func (mm *mutexMap[T]) lock(ctx context.Context, k T) (unlock func(), err error) {
	for {
		mm.mu.Lock()
		workDone := mm.m[k]
		if workDone == nil {
			c := make(chan struct{})
			if mm.m == nil {
				mm.m = make(map[T]<-chan struct{})
			}
			mm.m[k] = c
			mm.mu.Unlock()
			return func() {
				mm.mu.Lock()
				delete(mm.m, k)
				close(c)
				mm.mu.Unlock()
			}, nil
		}
		mm.mu.Unlock()

		select {
		case <-workDone:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
