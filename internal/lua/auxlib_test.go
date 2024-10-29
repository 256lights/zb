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

import "testing"

func TestLen(t *testing.T) {
	state := new(State)
	defer func() {
		if err := state.Close(); err != nil {
			t.Error("Close:", err)
		}
	}()
	want := []float64{123, 456, 789}
	state.CreateTable(len(want), 0)
	for i, n := range want {
		state.PushNumber(n)
		state.RawSetIndex(-2, int64(1+i))
	}

	got, err := Len(state, -1)
	if got != int64(len(want)) || err != nil {
		t.Errorf("Len(...) = %d, %v; want %d, <nil>", got, err, len(want))
	}
	if got, want := state.Top(), 1; got != want {
		t.Errorf("Top() = %d; want %d", got, want)
	}
}

func TestWhere(t *testing.T) {
	state := new(State)
	defer func() {
		if err := state.Close(); err != nil {
			t.Error("Close:", err)
		}
	}()

	state.PushClosure(0, func(l *State) (int, error) {
		l.PushString(Where(l, 1))
		return 1, nil
	})
	if err := state.SetGlobal("identify", 0); err != nil {
		t.Fatal(err)
	}
	const luaCode = "\nreturn identify()\n"
	const chunkName = "=(load)"
	if err := state.LoadString(luaCode, chunkName, "t"); err != nil {
		t.Fatal(err)
	}
	if err := state.Call(0, 1, 0); err != nil {
		t.Fatal(err)
	}

	got, ok := state.ToString(-1)
	if want := "(load):2: "; got != want || !ok {
		t.Errorf("result = %q; want %q", got, want)
	}
}
