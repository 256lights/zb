// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package mylua

import (
	"strings"
	"testing"

	"zb.256lights.llc/pkg/internal/luacode"
)

func TestVM(t *testing.T) {
	t.Run("AddImmediate", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.PushInteger(5)
		if err := state.SetGlobal("x", 0); err != nil {
			t.Fatal(err)
		}
		const source = "return x + 2"
		if err := state.Load(strings.NewReader(source), source, "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(0, 1, 0); err != nil {
			t.Fatal(err)
		}
		if !state.IsNumber(-1) {
			t.Fatalf("top of stack is %v; want number", state.Type(-1))
		}
		const want = int64(7)
		if got, ok := state.ToInteger(-1); got != want || !ok {
			t.Errorf("state.ToInteger(-1) = %d, %t; want %d, true", got, ok, want)
		}
	})

	t.Run("AddRegisters", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.PushInteger(5)
		if err := state.SetGlobal("x", 0); err != nil {
			t.Fatal(err)
		}
		const source = "local y = 2\nreturn x + y"
		if err := state.Load(strings.NewReader(source), source, "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(0, 1, 0); err != nil {
			t.Fatal(err)
		}
		if !state.IsNumber(-1) {
			t.Fatalf("top of stack is %v; want number", state.Type(-1))
		}
		const want = int64(7)
		if got, ok := state.ToInteger(-1); got != want || !ok {
			t.Errorf("state.ToInteger(-1) = %d, %t; want %d, true", got, ok, want)
		}
	})

	t.Run("AddConstant", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.PushInteger(2)
		if err := state.SetGlobal("x", 0); err != nil {
			t.Fatal(err)
		}
		const source = "return x + 129"
		if err := state.Load(strings.NewReader(source), source, "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(0, 1, 0); err != nil {
			t.Fatal(err)
		}
		if !state.IsNumber(-1) {
			t.Fatalf("top of stack is %v; want number", state.Type(-1))
		}
		const want = int64(131)
		if got, ok := state.ToInteger(-1); got != want || !ok {
			t.Errorf("state.ToInteger(-1) = %d, %t; want %d, true", got, ok, want)
		}
	})

	t.Run("SetListSmall", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const source = `return {"abc", 42, 3.14}`
		if err := state.Load(strings.NewReader(source), source, "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(0, 1, 0); err != nil {
			t.Fatal(err)
		}
		if !state.IsTable(-1) {
			t.Fatalf("top of stack is %v; want table", state.Type(-1))
		}
		if got, want := state.RawLen(-1), 3; got != uint64(want) {
			t.Errorf("table size = %d; want %d", got, want)
		}

		state.RawIndex(-1, 1)
		if got, ok := state.ToString(-1); got != "abc" || !ok {
			t.Errorf("table[1] = %q, %t; want %q, true", got, ok, "abc")
		}
		state.Pop(1)

		state.RawIndex(-1, 2)
		if got, ok := state.ToInteger(-1); got != 42 || !ok {
			t.Errorf("table[2] = %d, %t; want %d, true", got, ok, 42)
		}
		state.Pop(1)

		state.RawIndex(-1, 3)
		if got, ok := state.ToNumber(-1); got != 3.14 || !ok {
			t.Errorf("table[3] = %g, %t; want %g, true", got, ok, 3.14)
		}
		state.Pop(1)
	})

	t.Run("SetListLarge", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const wantLength = 256
		source := "return {42" + strings.Repeat(",42", wantLength-1) + "}"
		if err := state.Load(strings.NewReader(source), luacode.Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(0, 1, 0); err != nil {
			t.Fatal(err)
		}
		if !state.IsTable(-1) {
			t.Fatalf("top of stack is %v; want table", state.Type(-1))
		}
		if got := state.RawLen(-1); got != uint64(wantLength) {
			t.Errorf("table size = %d; want %d", got, wantLength)
		}
		for i := int64(1); i <= wantLength; i++ {
			state.RawIndex(-1, i)
			if got, ok := state.ToInteger(-1); got != 42 || !ok {
				t.Errorf("table[%d] = %d, %t; want %d, true", i, got, ok, 42)
			}
			state.Pop(1)
		}
	})

	t.Run("LenString", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const source = `return #"abc"`
		if err := state.Load(strings.NewReader(source), luacode.Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(0, 1, 0); err != nil {
			t.Fatal(err)
		}
		if !state.IsNumber(-1) {
			t.Fatalf("top of stack is %v; want number", state.Type(-1))
		}
		const want = int64(3)
		if got, ok := state.ToInteger(-1); got != want || !ok {
			t.Errorf("state.ToInteger(-1) = %d, %t; want %d, true", got, ok, want)
		}
	})

	t.Run("LenTable", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const source = `return #{123, 456}`
		if err := state.Load(strings.NewReader(source), luacode.Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(0, 1, 0); err != nil {
			t.Fatal(err)
		}
		if !state.IsNumber(-1) {
			t.Fatalf("top of stack is %v; want number", state.Type(-1))
		}
		const want = int64(2)
		if got, ok := state.ToInteger(-1); got != want || !ok {
			t.Errorf("state.ToInteger(-1) = %d, %t; want %d, true", got, ok, want)
		}
	})

	t.Run("Concat2", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.PushString("World")
		if err := state.SetGlobal("x", 0); err != nil {
			t.Fatal(err)
		}

		const source = `return "Hello, "..x`
		if err := state.Load(strings.NewReader(source), luacode.Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(0, 1, 0); err != nil {
			t.Fatal(err)
		}
		const want = "Hello, World"
		if got, ok := state.ToString(-1); got != want || !ok {
			t.Errorf("state.ToString(-1) = %q, %t; want %q, true", got, ok, want)
		}
	})

	t.Run("Concat3", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.PushString("World")
		if err := state.SetGlobal("x", 0); err != nil {
			t.Fatal(err)
		}

		const source = `return "Hello, "..x.."!"`
		if err := state.Load(strings.NewReader(source), luacode.Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(0, 1, 0); err != nil {
			t.Fatal(err)
		}
		const want = "Hello, World!"
		if got, ok := state.ToString(-1); got != want || !ok {
			t.Errorf("state.ToString(-1) = %q, %t; want %q, true", got, ok, want)
		}
	})
}
