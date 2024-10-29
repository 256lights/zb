// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package backend

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"zb.256lights.llc/pkg/sets"
)

// userSet acts as a semaphore for build users.
// Methods on userSet are safe to call concurrently from multiple goroutines.
type userSet struct {
	users       []BuildUser
	releaseFull chan struct{}

	mu    sync.Mutex
	inUse sets.Bit
}

func newUserSet(users []BuildUser) (*userSet, error) {
	for i, u1 := range users {
		for _, u2 := range users[i+1:] {
			if u1.UID == u2.UID {
				return nil, fmt.Errorf("uid %d used multiple times", u1.UID)
			}
		}
	}
	return &userSet{
		users:       slices.Clone(users),
		releaseFull: make(chan struct{}, 1),
	}, nil
}

func (users *userSet) acquire(ctx context.Context) (*BuildUser, error) {
	if len(users.users) == 0 {
		return nil, nil
	}

	for {
		users.mu.Lock()
		if users.inUse.Len() < len(users.users) {
			for i := range users.users {
				if !users.inUse.Has(uint(i)) {
					users.inUse.Add(uint(i))
					users.mu.Unlock()
					u := users.users[i]
					return &u, nil
				}
			}
		}
		users.mu.Unlock()

		select {
		case <-users.releaseFull:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (users *userSet) release(user *BuildUser) {
	if user == nil {
		if len(users.users) > 0 {
			panic("userSet.release(nil)")
		}
		return
	}

	i := slices.Index(users.users, *user)
	if i < 0 {
		panic("userSet.release on unknown user")
	}

	users.mu.Lock()
	shouldNotify := users.inUse.Len() == len(users.users)
	users.inUse.Delete(uint(i))
	users.mu.Unlock()

	if shouldNotify {
		select {
		case users.releaseFull <- struct{}{}:
		default:
			// If we had no one blocking, the channel may be full.
			// Don't block on release for this,
			// just let the acquirer re-lock and figure it out.
		}
	}
}
