// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"zb.256lights.llc/pkg/internal/luacode"
	"zb.256lights.llc/pkg/internal/lualex"
)

func TestVM(t *testing.T) {
	t.Run("AddImmediate", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.PushInteger(5)
		if err := state.SetGlobal(ctx, "x"); err != nil {
			t.Fatal(err)
		}
		const source = "return x + 2"
		if err := state.Load(strings.NewReader(source), source, "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
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
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.PushInteger(5)
		if err := state.SetGlobal(ctx, "x"); err != nil {
			t.Fatal(err)
		}
		const source = "local y = 2\nreturn x + y"
		if err := state.Load(strings.NewReader(source), source, "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
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
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.PushInteger(2)
		if err := state.SetGlobal(ctx, "x"); err != nil {
			t.Fatal(err)
		}
		const source = "return x + 129"
		if err := state.Load(strings.NewReader(source), source, "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
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
		ctx := context.Background()
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
		if err := state.Call(ctx, 0, 1); err != nil {
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
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const wantLength = 256
		source := "return {42" + strings.Repeat(",42", wantLength-1) + "}"
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
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
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const source = `return #"abc"`
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
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
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const source = `return #{123, 456}`
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
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
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.PushString("World")
		if err := state.SetGlobal(ctx, "x"); err != nil {
			t.Fatal(err)
		}

		const source = `return "Hello, "..x`
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
			t.Fatal(err)
		}
		const want = "Hello, World"
		if got, ok := state.ToString(-1); got != want || !ok {
			t.Errorf("state.ToString(-1) = %q, %t; want %q, true", got, ok, want)
		}
	})

	t.Run("Concat3", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.PushString("World")
		if err := state.SetGlobal(ctx, "x"); err != nil {
			t.Fatal(err)
		}

		const source = `return "Hello, "..x.."!"`
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
			t.Fatal(err)
		}
		const want = "Hello, World!"
		if got, ok := state.ToString(-1); got != want || !ok {
			t.Errorf("state.ToString(-1) = %q, %t; want %q, true", got, ok, want)
		}
	})

	t.Run("Vararg", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		var got []int64
		state.PushClosure(0, func(ctx context.Context, state *State) (int, error) {
			for i := range state.Top() {
				n, ok := state.ToInteger(1 + i)
				if !ok {
					t.Errorf("emit arg %d is a %v", len(got)+1, state.Type(1))
				}
				got = append(got, n)
			}
			return 0, nil
		})
		if err := state.SetGlobal(ctx, "emit"); err != nil {
			t.Fatal(err)
		}

		const source = `local function passthru(...)` + "\n" +
			`return ...` + "\n" +
			`end` + "\n" +
			// Not returning, because that would be a tail call.
			`emit(passthru(123, 456, 789))` + "\n"
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 0); err != nil {
			t.Fatal(err)
		}

		want := []int64{123, 456, 789}
		if !slices.Equal(want, got) {
			t.Errorf("emit arguments = %v; want %v", got, want)
		}
	})

	t.Run("TBC", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.PushClosure(0, func(ctx context.Context, state *State) (int, error) {
			state.SetTop(2)
			if err := state.SetMetatable(1); err != nil {
				return 0, err
			}
			return 1, nil
		})
		if err := state.SetGlobal(ctx, "setmetatable"); err != nil {
			t.Fatal(err)
		}

		var got []int64
		state.PushClosure(0, func(ctx context.Context, state *State) (int, error) {
			state.SetTop(1)
			i, _ := state.ToInteger(1)
			got = append(got, i)
			return 0, nil
		})
		if err := state.SetGlobal(ctx, "emit"); err != nil {
			t.Fatal(err)
		}

		const source = `local meta = {` + "\n" +
			`__close = function (tab, e)` + "\n" +
			`emit(tab.x)` + "\n" +
			`end,` + "\n" +
			`}` + "\n" +
			`local function newThing(x)` + "\n" +
			// Avoid testing tail calls in this function
			// by using parentheses to adjust results to 1.
			`return (setmetatable({x = x}, meta))` + "\n" +
			`end` + "\n" +
			`local v1 <close> = newThing(1)` + "\n" +
			`local v2 <close> = newThing(2)` + "\n" +
			`local v3 <close> = newThing(3)` + "\n"
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 0); err != nil {
			t.Fatal(err)
		}

		want := []int64{3, 2, 1}
		if !slices.Equal(want, got) {
			t.Errorf("emit sequence = %v; want %v", got, want)
		}
	})

	t.Run("Upvalues", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		var got []int64
		state.PushClosure(0, func(ctx context.Context, state *State) (int, error) {
			state.SetTop(1)
			i, ok := state.ToInteger(1)
			if !ok {
				t.Errorf("on call %d, emit received %v", len(got)+1, state.Type(1))
			}
			got = append(got, i)
			return 0, nil
		})
		if err := state.SetGlobal(ctx, "emit"); err != nil {
			t.Fatal(err)
		}

		const source = `local function counter()` + "\n" +
			`local x = 1` + "\n" +
			`return function() x = x + 1; return x - 1 end` + "\n" +
			`end` + "\n" +
			`local c = counter()` + "\n" +
			`emit(c())` + "\n" +
			`emit(c())` + "\n" +
			`emit(c())` + "\n"
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 0); err != nil {
			t.Fatal(err)
		}

		want := []int64{1, 2, 3}
		if !slices.Equal(want, got) {
			t.Errorf("emit sequence = %v; want %v", got, want)
		}
	})

	t.Run("NumericForLoop", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		var got []int64
		state.PushClosure(0, func(ctx context.Context, state *State) (int, error) {
			state.SetTop(1)
			i, ok := state.ToInteger(1)
			if !ok {
				t.Errorf("on call %d, emit received %v", len(got)+1, state.Type(1))
			}
			got = append(got, i)
			return 0, nil
		})
		if err := state.SetGlobal(ctx, "emit"); err != nil {
			t.Fatal(err)
		}

		const source = `for i = 1, 3 do` + "\n" +
			`emit(i)` + "\n" +
			`end` + "\n"
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 0); err != nil {
			t.Fatal(err)
		}

		want := []int64{1, 2, 3}
		if !slices.Equal(want, got) {
			t.Errorf("emit sequence = %v; want %v", got, want)
		}
	})

	t.Run("NumericForLoopWithExplicitStep", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		var got []int64
		state.PushClosure(0, func(ctx context.Context, state *State) (int, error) {
			state.SetTop(1)
			i, ok := state.ToInteger(1)
			if !ok {
				t.Errorf("on call %d, emit received %v", len(got)+1, state.Type(1))
			}
			got = append(got, i)
			return 0, nil
		})
		if err := state.SetGlobal(ctx, "emit"); err != nil {
			t.Fatal(err)
		}

		const source = `for i = 10, 1, -1 do` + "\n" +
			`emit(i)` + "\n" +
			`end` + "\n"
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 0); err != nil {
			t.Fatal(err)
		}

		want := []int64{10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
		if !slices.Equal(want, got) {
			t.Errorf("emit sequence = %v; want %v", got, want)
		}
	})

	t.Run("GenericForLoop", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		var got []float64
		state.PushClosure(0, func(ctx context.Context, state *State) (int, error) {
			state.SetTop(2)
			i, ok := state.ToInteger(1)
			if !ok {
				t.Errorf("on call %d, emit arg #1 is %v", len(got)+1, state.Type(1))
			} else if want := int64(len(got) + 1); i != want {
				t.Errorf("on call %d, emit arg #1 = %d (want %d)", len(got)+1, i, want)
			}
			v, ok := state.ToNumber(2)
			if !ok {
				t.Errorf("on call %d, emit arg #2 is %v", len(got)+1, state.Type(2))
			}
			got = append(got, v)
			return 0, nil
		})
		if err := state.SetGlobal(ctx, "emit"); err != nil {
			t.Fatal(err)
		}

		// Very light-weight copy of ipairs.
		state.PushClosure(0, func(ctx context.Context, state *State) (int, error) {
			state.SetTop(1)

			f := Function(func(ctx context.Context, state *State) (int, error) {
				i, ok := state.ToInteger(2)
				if !ok {
					return 0, fmt.Errorf("ipairs iterator function arg #2 not an integer (got %v)", state.Type(2))
				}
				i++
				state.PushInteger(i)
				tp, err := state.Index(ctx, 1, i)
				if err != nil {
					return 0, err
				}
				if tp == TypeNil {
					return 1, nil
				}
				return 2, nil
			})

			state.PushClosure(0, f)
			state.PushValue(1)
			state.PushInteger(0)
			return 3, nil
		})
		if err := state.SetGlobal(ctx, "ipairs"); err != nil {
			t.Fatal(err)
		}

		const source = `a = {42, 3.14}` + "\n" +
			`for i, v in ipairs(a) do` + "\n" +
			`emit(i, v)` + "\n" +
			`end` + "\n"
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 0); err != nil {
			t.Fatal(err)
		}

		want := []float64{42, 3.14}
		if !slices.Equal(want, got) {
			t.Errorf("emit sequence = %v; want %v", got, want)
		}
	})

	t.Run("TailCall", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const source = `local function factorial(n, acc)` + "\n" +
			`acc = acc or 1` + "\n" +
			`if n == 0 then return acc end` + "\n" +
			`return factorial(n - 1, acc * n)` + "\n" +
			`end` + "\n" +
			`return factorial(3)` + "\n"
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
			t.Fatal(err)
		}
		if !state.IsNumber(-1) {
			t.Fatalf("top of stack is %v; want number", state.Type(-1))
		}
		const want = 6.0
		if got, ok := state.ToNumber(-1); got != want || !ok {
			t.Errorf("(return value, is number) = (%g, %t); want (%g, true)", got, ok, want)
		}
	})

	t.Run("TailCallGoFunction", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const wantResult = "hello world"
		state.PushClosure(0, func(ctx context.Context, l *State) (int, error) {
			l.PushString(wantResult)
			return 1, nil
		})
		if err := state.SetGlobal(ctx, "foo"); err != nil {
			t.Fatal(err)
		}

		const source = `return foo()` + "\n"
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
			t.Fatal(err)
		}
		if got, want := state.Type(-1), TypeString; got != want {
			t.Fatalf("state.Type(-1) = %v; want %v", got, want)
		} else if got, ok := state.ToString(-1); got != wantResult || !ok {
			t.Errorf("state.ToString(-1) = %q, %t; want %q, true", got, ok, wantResult)
		}
	})

	// Regression test for tail calls with Go functions.
	// VM would keep the old function cached,
	// so despite the call stack being correct,
	// the VM would claim that resuming execution of the top-level script
	// was an out-of-bounds instruction.
	t.Run("TailCallGoOutOfBounds", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.PushClosure(0, func(ctx context.Context, l *State) (int, error) {
			l.SetTop(1)
			return 1, nil
		})
		if err := state.SetGlobal(ctx, "foo"); err != nil {
			t.Fatal(err)
		}

		const source = `local function wrapper(x)` + "\n" +
			`return foo(x)` + "\n" +
			`end` + "\n" +
			// Waste a couple instructions here so our top-level PC is outside the length of wrapper.
			`local y = 42` + "\n" +
			`foo(0)` + "\n" +
			// Now perform the tail call.
			`local z = wrapper(y)` + "\n" +
			`y = y + 1` + "\n" +
			`return y + z` + "\n"
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
			t.Fatal(err)
		}
		const wantResult = 85
		if got, want := state.Type(-1), TypeNumber; got != want {
			t.Fatalf("state.Type(-1) = %v; want %v", got, want)
		} else if got, ok := state.ToInteger(-1); got != wantResult || !ok {
			t.Errorf("state.ToInteger(-1) = %d, %t; want %d, true", got, ok, wantResult)
		}
	})

	t.Run("SetTable", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const source = `local a = {}` + "\n" +
			// The index cannot be a constant.
			`for i = 1,1 do a[i] = "xuxu" end` + "\n" +
			"return a\n"
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
			t.Fatal(err)
		}

		if got, err := Len(ctx, state, -1); got != 1 || err != nil {
			t.Errorf("Len(state, -1) = %d, %v; want 1, <nil>", got, err)
		}
		if got, want := state.RawIndex(-1, 1), TypeString; got != want {
			t.Errorf("type(a[1]) = %v; want %v", got, want)
		} else if got, _ := state.ToString(-1); got != "xuxu" {
			t.Errorf("a[1] = %s; want %s", lualex.Quote(got), lualex.Quote("xuxu"))
		}
		state.Pop(1)
	})

	t.Run("LoadNil", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const source = `local x = 1` + "\n" +
			`x = nil` + "\n" +
			"return x\n"
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
			t.Fatal(err)
		}

		if got, want := state.Type(-1), TypeNil; got != want {
			t.Errorf("type(a[1]) = %v; want %v", got, want)
		}
		state.Pop(1)
	})

	t.Run("AllowNegatedMetamethod", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		// Set a metatable for nil which treats the nil like a zero when subtracting.
		state.PushNil()
		NewPureLib(state, map[string]Function{
			luacode.TagMethodSub.String(): func(ctx context.Context, l *State) (int, error) {
				for i := range 2 {
					if l.IsNil(1 + i) {
						l.PushInteger(0)
						l.Replace(1 + i)
					}
				}
				if err := l.Arithmetic(ctx, luacode.Subtract); err != nil {
					return 0, err
				}
				return 1, nil
			},
		})
		if err := state.SetMetatable(-2); err != nil {
			t.Fatal(err)
		}

		const source = `return nil - 1`
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
			t.Fatal(err)
		}

		const wantResult = -1
		if got, want := state.Type(-1), TypeNumber; got != want {
			t.Fatalf("state.Type(-1) = %v; want %v", got, want)
		} else if got, ok := state.ToInteger(-1); got != wantResult || !ok {
			t.Errorf("state.ToInteger(-1) = %d, %t; want %d, true", got, ok, wantResult)
		}
	})

	t.Run("FlippedAddImmediateMetamethod", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		// Set a metatable for nil which treats the nil like 0 when adding.
		state.PushNil()
		var isNil [2]bool
		NewPureLib(state, map[string]Function{
			luacode.TagMethodAdd.String(): func(ctx context.Context, l *State) (int, error) {
				for i := range 2 {
					isNil[i] = l.IsNil(1 + i)
					if isNil[i] {
						l.PushInteger(0)
						l.Replace(1 + i)
					}
				}
				if err := l.Arithmetic(ctx, luacode.Add); err != nil {
					return 0, err
				}
				return 1, nil
			},
		})
		if err := state.SetMetatable(-2); err != nil {
			t.Fatal(err)
		}

		const source = `return 1 + nil`
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
			t.Fatal(err)
		}

		if isNil[0] || !isNil[1] {
			t.Errorf("when calling __add, (x == nil), (y == nil) = %t, %t; want false, true", isNil[0], isNil[1])
		}

		const wantResult = 1
		if got, want := state.Type(-1), TypeNumber; got != want {
			t.Fatalf("state.Type(-1) = %v; want %v", got, want)
		} else if got, ok := state.ToInteger(-1); got != wantResult || !ok {
			t.Errorf("state.ToInteger(-1) = %d, %t; want %d, true", got, ok, wantResult)
		}
	})

	t.Run("FlippedBOrImmediateMetamethod", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		// Set a metatable for nil which treats the nil like 0 when adding.
		state.PushNil()
		var isNil [2]bool
		NewPureLib(state, map[string]Function{
			luacode.TagMethodBOr.String(): func(ctx context.Context, l *State) (int, error) {
				for i := range 2 {
					isNil[i] = l.IsNil(1 + i)
					if isNil[i] {
						l.PushInteger(0)
						l.Replace(1 + i)
					}
				}
				if err := l.Arithmetic(ctx, luacode.BitwiseOr); err != nil {
					return 0, err
				}
				return 1, nil
			},
		})
		if err := state.SetMetatable(-2); err != nil {
			t.Fatal(err)
		}

		const source = `return 1 | nil`
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
			t.Fatal(err)
		}

		if isNil[0] || !isNil[1] {
			t.Errorf("when calling __bor, (x == nil), (y == nil) = %t, %t; want false, true", isNil[0], isNil[1])
		}

		const wantResult = 1
		if got, want := state.Type(-1), TypeNumber; got != want {
			t.Fatalf("state.Type(-1) = %v; want %v", got, want)
		} else if got, ok := state.ToInteger(-1); got != wantResult || !ok {
			t.Errorf("state.ToInteger(-1) = %d, %t; want %d, true", got, ok, wantResult)
		}
	})

	t.Run("FlippedAddConstantMetamethod", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		// Set a metatable for nil which treats the nil like 0 when adding.
		state.PushNil()
		var isNil [2]bool
		NewPureLib(state, map[string]Function{
			luacode.TagMethodAdd.String(): func(ctx context.Context, l *State) (int, error) {
				for i := range 2 {
					isNil[i] = l.IsNil(1 + i)
					if isNil[i] {
						l.PushInteger(0)
						l.Replace(1 + i)
					}
				}
				if err := l.Arithmetic(ctx, luacode.Add); err != nil {
					return 0, err
				}
				return 1, nil
			},
		})
		if err := state.SetMetatable(-2); err != nil {
			t.Fatal(err)
		}

		const source = `return 3.14 + nil`
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
			t.Fatal(err)
		}

		if isNil[0] || !isNil[1] {
			t.Errorf("when calling __add, (x == nil), (y == nil) = %t, %t; want false, true", isNil[0], isNil[1])
		}

		const wantResult = 3.14
		if got, want := state.Type(-1), TypeNumber; got != want {
			t.Fatalf("state.Type(-1) = %v; want %v", got, want)
		} else if got, ok := state.ToNumber(-1); got != wantResult || !ok {
			t.Errorf("state.ToNumber(-1) = %g, %t; want %g, true", got, ok, wantResult)
		}
	})
}
