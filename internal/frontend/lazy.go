// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package frontend

import (
	"context"
	"fmt"
	"math"
	"sync"

	"zb.256lights.llc/pkg/internal/lua"
)

const lazyTypeName = "lazy"

const (
	lazySentinelRegistryKey = "zb.256lights.llc/pkg/internal/frontend lazy sentinel"

	lazyErrorTypeName    = "zb.256lights.llc/pkg/internal/frontend lazy error"
	lazyProgressTypeName = "zb.256lights.llc/pkg/internal/frontend lazy progress"
)

type lazyTable struct {
	mu      sync.Mutex
	storage lua.State
}

func (lt *lazyTable) Freeze() error {
	return nil
}

func registerLazyMetatable(ctx context.Context, l *lua.State) error {
	lua.NewMetatable(l, lazyTypeName)
	err := lua.SetPureFunctions(ctx, l, 0, map[string]lua.Function{
		"__index":     indexLazy,
		"__metatable": nil, // prevent Lua access to metatable
	})
	if err != nil {
		return err
	}
	l.Pop(1)
	return nil
}

func lazyFunction(ctx context.Context, l *lua.State) (int, error) {
	lt := new(lazyTable)
	lt.storage.NewUserdata(nil, 0)
	if err := lt.storage.RawSetField(lua.RegistryIndex, lazySentinelRegistryKey); err != nil {
		return 0, err
	}
	lua.NewMetatable(&lt.storage, lazyErrorTypeName)
	lua.NewMetatable(&lt.storage, lazyProgressTypeName)
	lt.storage.Pop(2)
	lt.storage.CreateTable(0, 0)

	l.SetTop(2)
	l.NewUserdata(lt, 1)
	l.PushValue(1)
	if err := l.SetUserValue(-2, 1); err != nil {
		return 0, err
	}

	// The second argument, if provided, must be a simple table
	// that we immediately initialize with values.
	if tp := l.Type(2); tp != lua.TypeNil {
		if tp != lua.TypeTable {
			return 0, lua.NewTypeError(l, 2, lua.TypeTable.String())
		}
		l.PushNil()
		for l.Next(2) {
			if !isLazyKey(l, -2) {
				l.Pop(1)
				continue
			}
			if err := l.Freeze(-1); err != nil {
				keyString, _, _ := lua.ToString(ctx, l, -1)
				return 0, fmt.Errorf("%scannot freeze value for %s", lua.Where(l, 1), keyString)
			}

			l.PushValue(-2) // key
			l.Insert(-2)
			if err := lt.storage.XMove(l, 2); err != nil {
				return 0, err
			}
			if err := lt.storage.RawSet(1); err != nil {
				return 0, err
			}
		}
	}

	if err := lua.SetMetatable(l, lazyTypeName); err != nil {
		return 0, err
	}

	return 1, nil
}

func toLazy(l *lua.State) (*lazyTable, error) {
	const idx = 1
	if _, err := lua.CheckUserdata(l, idx, lazyTypeName); err != nil {
		return nil, err
	}
	lt := testLazy(l, idx)
	if lt == nil {
		return nil, lua.NewArgError(l, idx, "could not extract lazy")
	}
	return lt, nil
}

func testLazy(l *lua.State, idx int) *lazyTable {
	x, _ := lua.TestUserdata(l, idx, lazyTypeName)
	lt, _ := x.(*lazyTable)
	return lt
}

func indexLazy(ctx context.Context, l *lua.State) (int, error) {
	lt, err := toLazy(l)
	if err != nil {
		return 0, err
	}
	if !isLazyKey(l, 2) {
		l.PushNil()
		return 1, nil
	}

	// Check in storage.
	l.PushValue(2)
	lt.mu.Lock()
	if err := lt.storage.XMove(l, 1); err != nil {
		lt.mu.Unlock()
		return 0, err
	}
	lt.storage.PushValue(-1) // Save copy for placeholder set down below.
	if cacheHit, err := lt.lockedCheckCache(ctx); err != nil {
		lt.storage.SetTop(1)
		lt.mu.Unlock()
		keyString, _, _ := lua.ToString(ctx, l, 2)
		return 0, fmt.Errorf("%sindex %s: %w", lua.Where(l, 1), keyString, err)
	} else if cacheHit {
		err := l.XMove(&lt.storage, 1)
		lt.storage.SetTop(1)
		lt.mu.Unlock()
		if err != nil {
			return 0, err
		}
		return 1, nil
	}
	// First time seeing this index. Add a placeholder.
	done := make(chan struct{})
	lt.storage.NewUserdata((<-chan struct{})(done), 0)
	lua.SetMetatable(&lt.storage, lazyProgressTypeName)
	err = lt.storage.RawSet(1)
	lt.mu.Unlock()
	if err != nil {
		return 0, err
	}

	// Call the function.
	// TODO(someday): Preserve error object instead of just string.
	l.UserValue(1, 1) // stored function
	l.PushValue(1)    // lazy table
	l.PushValue(2)    // key
	callError := l.PCall(ctx, 2, 1, 0)
	if callError == nil {
		if err := l.Freeze(-1); err != nil {
			l.Pop(1)
			keyString, _, _ := lua.ToString(ctx, l, 2)
			callError = fmt.Errorf("%scannot freeze value for %s: %v", lua.Where(l, 1), keyString, err)
		}
	}

	// Store the result.
	// The error conditions in this critical section should only trigger if there's a bug.
	n := 1
	l.PushValue(2) // key
	if callError == nil {
		l.PushValue(-2) // value
		n++
	}
	lt.mu.Lock()
	defer lt.mu.Unlock()
	if err := lt.storage.XMove(l, n); err != nil {
		return 0, err
	}
	switch {
	case callError != nil:
		// Wrap with error object.
		lt.storage.NewUserdata(callError, 0)
		lua.SetMetatable(&lt.storage, lazyErrorTypeName)
	case lt.storage.IsNil(-1):
		// Use the sentinel value instead of an actual nil
		// to record that we've already called the function for this key.
		lt.storage.Pop(1)
		lt.storage.RawField(lua.RegistryIndex, lazySentinelRegistryKey)
	}
	if err := lt.storage.RawSet(1); err != nil {
		return 0, err
	}

	close(done)
	return 1, nil
}

// lockedCheckCache checks the lazy table's storage for the key on the top of the stack.
// The key will be popped, then, if lockedCheckCache returns true,
// the value will be pushed in its place.
// The caller must be holding onto lt.mu.
func (lt *lazyTable) lockedCheckCache(ctx context.Context) (bool, error) {
	lt.storage.PushValue(-1) // retain key so we can do another fetch for progress
	cachedType := lt.storage.RawGet(1)
	if cachedType == lua.TypeNil {
		lt.storage.Pop(2)
		return false, nil
	}
	if cachedType != lua.TypeUserdata {
		lt.storage.Remove(-2)
		return true, nil
	}

	if data, ok := lua.TestUserdata(&lt.storage, -1, lazyProgressTypeName); !ok {
		lt.storage.Remove(-2)
	} else {
		// Another thread is calling the callback function.
		lt.storage.Pop(1) // Pop value. Key will be on top.
		ready := data.(<-chan struct{})
		var temp lua.State
		if err := temp.XMove(&lt.storage, 1); err != nil {
			return false, err
		}

		lt.mu.Unlock()
		select {
		case <-ready:
		case <-ctx.Done():
			return false, ctx.Err()
		}
		lt.mu.Lock()

		// Push key back on and fetch.
		if err := lt.storage.XMove(&temp, 1); err != nil {
			return false, err
		}
		cachedType = lt.storage.RawGet(1)
		if cachedType != lua.TypeUserdata {
			return true, nil
		}
	}

	lt.storage.RawField(lua.RegistryIndex, lazySentinelRegistryKey)
	if lt.storage.RawEqual(-2, -1) {
		// We've already called the function for this key and it returned nil.
		lt.storage.Pop(1)
		lt.storage.PushNil()
		return true, nil
	}
	lt.storage.Pop(1) // sentinel

	if data, ok := lua.TestUserdata(&lt.storage, -1, lazyErrorTypeName); ok {
		// The callback function raised an error when called.
		lt.storage.Pop(1)
		return false, data.(error)
	}

	return true, nil
}

func isLazyKey(l *lua.State, idx int) bool {
	switch l.Type(idx) {
	case lua.TypeString, lua.TypeBoolean:
		return true
	case lua.TypeNumber:
		n, _ := l.ToNumber(idx)
		return !math.IsNaN(n)
	default:
		return false
	}
}
