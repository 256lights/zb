// Copyright 2023 Roxy Light
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of
// this software and associated documentation files (the “Software”), to deal in
// the Software without restriction, including without limitation the rights to
// use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
// the Software, and to permit persons to whom the Software is furnished to do so,
// subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED “AS IS”, WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
// FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
// COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
// IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
// CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
//
// SPDX-License-Identifier: MIT

package lua

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
	"unsafe"

	"zb.256lights.llc/pkg/internal/lua54"
)

func TestLoad(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const source = "return 2 + 2"
		if err := state.Load(strings.NewReader(source), source, "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(0, 1, 0); err != nil {
			t.Fatal(err)
		}
		if !state.IsNumber(-1) {
			t.Fatalf("top of stack is %v; want number", state.Type(-1))
		}
		const want = int64(4)
		if got, ok := state.ToInteger(-1); got != want || !ok {
			t.Errorf("state.ToInteger(-1) = %d, %t; want %d, true", got, ok, want)
		}
	})

	t.Run("ReadError", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const message = "bork"
		r := io.MultiReader(strings.NewReader("return"), iotest.ErrReader(errors.New(message)))
		err := state.Load(r, "=(reader)", "t")
		if err == nil {
			t.Error("state.Load(...) = <nil>; want error")
		} else if got := err.Error(); !strings.Contains(got, message) {
			t.Errorf("state.Load(...) = %v; want to contain %q", got, message)
		}
		if got, ok := state.ToString(-1); !strings.Contains(got, message) || !ok {
			t.Errorf("state.ToString(-1) = %q, %t; want to contain %q", got, ok, message)
		}
	})
}

func TestLoadString(t *testing.T) {
	state := new(State)
	defer func() {
		if err := state.Close(); err != nil {
			t.Error("Close:", err)
		}
	}()

	const source = "return 2 + 2"
	if err := state.LoadString(source, source, "t"); err != nil {
		t.Fatal(err)
	}
	if err := state.Call(0, 1, 0); err != nil {
		t.Fatal(err)
	}
	if !state.IsNumber(-1) {
		t.Fatalf("top of stack is %v; want number", state.Type(-1))
	}
	const want = int64(4)
	if got, ok := state.ToInteger(-1); got != want || !ok {
		t.Errorf("state.ToInteger(-1) = %d, %t; want %d, true", got, ok, want)
	}
}

func TestDump(t *testing.T) {
	state := new(State)
	defer func() {
		if err := state.Close(); err != nil {
			t.Error("Close:", err)
		}
	}()

	const source = "return 2 + 2"
	if err := state.LoadString(source, source, "t"); err != nil {
		t.Fatal(err)
	}
	compiledChunk := new(strings.Builder)
	n, err := state.Dump(compiledChunk, false)
	if wantN := int64(compiledChunk.Len()); n != wantN || err != nil {
		t.Errorf("state.Dump(...) = %d, %v; want %d, <nil>", n, err, wantN)
	}
	state.Pop(1)

	if err := state.LoadString(compiledChunk.String(), "=(load)", "b"); err != nil {
		t.Fatal(err)
	}
	if err := state.Call(0, 1, 0); err != nil {
		t.Fatal(err)
	}
	if !state.IsNumber(-1) {
		t.Fatalf("top of stack is %v; want number", state.Type(-1))
	}
	const want = int64(4)
	if got, ok := state.ToInteger(-1); got != want || !ok {
		t.Errorf("state.ToInteger(-1) = %d, %t; want %d, true", got, ok, want)
	}
}

func TestFullUserdata(t *testing.T) {
	state := new(State)
	defer func() {
		if err := state.Close(); err != nil {
			t.Error("Close:", err)
		}
	}()

	state.NewUserdataUV(4, 1)
	if got, want := state.RawLen(-1), uint64(4); got != want {
		t.Errorf("state.RawLen(-1) = %d; want %d", got, want)
	}
	var gotBlock [4]byte
	if got, want := state.CopyUserdata(gotBlock[:], -1, 0), 4; got != want {
		t.Errorf("CopyUserdata(...) = %d; want %d", got, want)
	} else if want := ([4]byte{}); gotBlock != want {
		t.Errorf("after init, block = %v; want %v", gotBlock, want)
	}
	state.SetUserdata(-1, 0, []byte{0xde, 0xad, 0xbe, 0xef})
	if got, want := state.CopyUserdata(gotBlock[:], -1, 0), 4; got != want {
		t.Errorf("CopyUserdata(...) = %d; want %d", got, want)
	} else if want := ([4]byte{0xde, 0xad, 0xbe, 0xef}); gotBlock != want {
		t.Errorf("after init, block = %v; want %v", gotBlock, want)
	}

	const wantUserValue = 42
	state.PushInteger(wantUserValue)
	if !state.SetUserValue(-2, 1) {
		t.Error("Userdata does not have value 1")
	}
	if got, want := state.UserValue(-1, 1), TypeNumber; got != want {
		t.Errorf("user value 1 type = %v; want %v", got, want)
	}
	if got, ok := state.ToInteger(-1); got != wantUserValue || !ok {
		value, err := ToString(state, -1)
		if err != nil {
			value = "<unknown value>"
		}
		t.Errorf("user value 1 = %s; want %d", value, wantUserValue)
	}
	state.Pop(1)

	if got, want := state.UserValue(-1, 2), TypeNone; got != want {
		t.Errorf("user value 2 type = %v; want %v", got, want)
	}
	if got, want := state.Top(), 2; got != want {
		t.Errorf("after state.UserValue(-1, 2), state.Top() = %d; want %d", got, want)
	}
	if !state.IsNil(-1) {
		value, err := ToString(state, -1)
		if err != nil {
			value = "<unknown value>"
		}
		t.Errorf("user value 2 = %s; want nil", value)
	}
}

func TestLightUserdata(t *testing.T) {
	state := new(State)
	defer func() {
		if err := state.Close(); err != nil {
			t.Error("Close:", err)
		}
	}()

	vals := []uintptr{0, 42}
	for _, p := range vals {
		state.PushLightUserdata(p)
	}

	if got, want := state.Top(), len(vals); got != want {
		t.Fatalf("state.Top() = %d; want %d", got, want)
	}
	for i := 1; i <= len(vals); i++ {
		if got, want := state.Type(i), TypeLightUserdata; got != want {
			t.Errorf("state.Type(%d) = %v; want %v", i, got, want)
		}
		if !state.IsUserdata(i) {
			t.Errorf("state.IsUserdata(%d) = false; want true", i)
		}
		if got, want := state.ToPointer(i), vals[i-1]; got != want {
			t.Errorf("state.ToPointer(%d) = %#x; want %#x", i, got, want)
		}
	}
}

func TestPushClosure(t *testing.T) {
	t.Run("NoUpvalues", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const want = 42
		state.PushClosure(0, func(l *State) (int, error) {
			l.PushInteger(want)
			return 1, nil
		})
		if err := state.Call(0, 1, 0); err != nil {
			t.Fatal(err)
		}
		if got, ok := state.ToInteger(-1); got != want || !ok {
			value, err := ToString(state, -1)
			if err != nil {
				value = "<unknown value>"
			}
			t.Errorf("function returned %s; want %d", value, want)
		}
	})

	t.Run("Upvalues", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const want = 42
		state.PushInteger(want)
		state.PushClosure(1, func(l *State) (int, error) {
			l.PushValue(UpvalueIndex(1))
			return 1, nil
		})
		if err := state.Call(0, 1, 0); err != nil {
			t.Fatal(err)
		}
		if got, ok := state.ToInteger(-1); got != want || !ok {
			value, err := ToString(state, -1)
			if err != nil {
				value = "<unknown value>"
			}
			t.Errorf("function returned %s; want %d", value, want)
		}
	})
}

// TestStateRepresentation ensures that State has the same memory representation
// as lua54.State.
// This is critical for the correct functioning of [State.PushClosure],
// which avoids allocating a new closure by using a func(*State) (int, error)
// as a func(*lua54.State) (int, error).
func TestStateRepresentation(t *testing.T) {
	if got, want := unsafe.Offsetof(State{}.state), uintptr(0); got != want {
		t.Errorf("unsafe.Offsetof(State{}.state) = %d; want %d", got, want)
	}
	if got, want := unsafe.Sizeof(State{}), unsafe.Sizeof(lua54.State{}); got != want {
		t.Errorf("unsafe.Sizeof(State{}) = %d; want %d", got, want)
	}
	if got, want := unsafe.Alignof(State{}), unsafe.Alignof(lua54.State{}); got%want != 0 {
		t.Errorf("unsafe.Alignof(State{}) = %d; want %d", got, want)
	}
}

func TestStringContext(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const s = "hello"
		want := []string{"bar", "foo"}
		state.PushStringContext(s, slices.Clone(want))
		if got := state.StringContext(-1); !slices.Equal(got, want) {
			t.Errorf("state.StringContext(-1) = %q; want %q", got, want)
		}
		if got, ok := state.ToString(-1); got != s || !ok {
			t.Errorf("state.ToString(-1) = %q, %t; want %q, true", got, ok, s)
		}
	})

	t.Run("Concat", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.PushStringContext("a", []string{"foo"})
		state.PushStringContext("b", []string{"bar"})
		if err := state.Concat(2, 0); err != nil {
			t.Fatal(err)
		}

		if got, want := state.StringContext(-1), ([]string{"foo", "bar"}); !slices.Equal(got, want) {
			t.Errorf("state.StringContext(-1) = %q; want %q", got, want)
		}
		const want = "ab"
		if got, ok := state.ToString(-1); got != want || !ok {
			t.Errorf("state.ToString(-1) = %q, %t; want %q, true", got, ok, want)
		}
	})
}

func BenchmarkExec(b *testing.B) {
	state := new(State)
	defer func() {
		if err := state.Close(); err != nil {
			b.Error("Close:", err)
		}
	}()

	const source = "return 2 + 2"
	for i := 0; i < b.N; i++ {
		if err := state.LoadString(source, source, "t"); err != nil {
			b.Fatal(err)
		}
		if err := state.Call(0, 1, 0); err != nil {
			b.Fatal(err)
		}
		state.Pop(1)
	}
}

func BenchmarkPushClosure(b *testing.B) {
	b.ReportAllocs()

	state := new(State)
	defer func() {
		if err := state.Close(); err != nil {
			b.Error("Close:", err)
		}
	}()

	f := Function(func(l *State) (int, error) { return 0, nil })
	for i := 0; i < b.N; i++ {
		state.PushClosure(0, f)
		state.Pop(1)
	}
}

func BenchmarkOpenLibraries(b *testing.B) {
	b.ReportAllocs()

	state := new(State)
	defer func() {
		if err := state.Close(); err != nil {
			b.Error("Close:", err)
		}
	}()

	for i := 0; i < b.N; i++ {
		if err := OpenLibraries(state); err != nil {
			b.Fatal(err)
		}
	}
}
