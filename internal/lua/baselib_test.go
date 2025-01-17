// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"context"
	"testing"
)

func TestAssert(t *testing.T) {
	t.Run("True", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Fatal(err)
			}
		}()

		if err := Require(ctx, state, GName, true, NewOpenBase(nil)); err != nil {
			t.Fatal(err)
		}
		state.Pop(1)
		if got, err := state.Global(ctx, "assert"); got != TypeFunction || err != nil {
			t.Fatalf("state.Global(ctx, \"assert\") = %v, %v; want %v, <nil>", got, err, TypeFunction)
		}

		state.PushBoolean(true)
		if err := state.Call(ctx, 1, MultipleReturns, 0); err != nil {
			t.Fatal(err)
		}

		if got, want := state.Top(), 1; got != want {
			t.Errorf("state.Top() = %d; want %d", got, want)
		}
		if got, want := state.ToBoolean(1), true; got != want {
			t.Errorf("state.ToBoolean(1) = %t; want %t", got, want)
		}
	})

	t.Run("False", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Fatal(err)
			}
		}()

		if err := Require(ctx, state, GName, true, NewOpenBase(nil)); err != nil {
			t.Fatal(err)
		}
		state.Pop(1)
		if got, err := state.Global(ctx, "assert"); got != TypeFunction || err != nil {
			t.Fatalf("state.Global(ctx, \"assert\") = %v, %v; want %v, <nil>", got, err, TypeFunction)
		}

		state.PushBoolean(false)
		if err := state.Call(ctx, 1, MultipleReturns, 0); err == nil {
			t.Error("state.Call(ctx, 1, MultipleReturns, 0) did not return an error")
		} else if got, want := err.Error(), "assertion failed!"; got != want {
			t.Errorf("state.Call(ctx, 1, MultipleReturns, 0) error = %q; want %q", got, want)
		}
	})

	t.Run("Nil", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Fatal(err)
			}
		}()

		if err := Require(ctx, state, GName, true, NewOpenBase(nil)); err != nil {
			t.Fatal(err)
		}
		state.Pop(1)
		if got, err := state.Global(ctx, "assert"); got != TypeFunction || err != nil {
			t.Fatalf("state.Global(ctx, \"assert\") = %v, %v; want %v, <nil>", got, err, TypeFunction)
		}

		state.PushNil()
		if err := state.Call(ctx, 1, MultipleReturns, 0); err == nil {
			t.Error("state.Call(ctx, 1, MultipleReturns, 0) did not return an error")
		} else if got, want := err.Error(), "assertion failed!"; got != want {
			t.Errorf("state.Call(ctx, 1, MultipleReturns, 0) error = %q; want %q", got, want)
		}
	})

	t.Run("ErrorMessage", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Fatal(err)
			}
		}()

		if err := Require(ctx, state, GName, true, NewOpenBase(nil)); err != nil {
			t.Fatal(err)
		}
		state.Pop(1)
		if got, err := state.Global(ctx, "assert"); got != TypeFunction || err != nil {
			t.Fatalf("state.Global(ctx, \"assert\") = %v, %v; want %v, <nil>", got, err, TypeFunction)
		}

		state.PushBoolean(false)
		const msg = "bork bork bork"
		state.PushString(msg)
		if err := state.Call(ctx, 2, MultipleReturns, 0); err == nil {
			t.Error("state.Call(ctx, 1, MultipleReturns, 0) did not return an error")
		} else if got, want := err.Error(), msg; got != want {
			t.Errorf("state.Call(ctx, 1, MultipleReturns, 0) error = %q; want %q", got, want)
		}
	})
}
