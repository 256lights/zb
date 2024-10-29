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
	"bytes"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func TestBasicLibrary(t *testing.T) {
	state := new(State)
	defer func() {
		if err := state.Close(); err != nil {
			t.Error("Close:", err)
		}
	}()

	out := new(bytes.Buffer)
	if err := Require(state, GName, true, NewOpenBase(out, nil)); err != nil {
		t.Error(err)
	}
	if _, err := state.Global("print", 0); err != nil {
		t.Fatal(err)
	}
	state.PushString("Hello, World!")
	state.PushInteger(42)
	if err := state.Call(2, 0, 0); err != nil {
		t.Fatal(err)
	}

	if got, want := out.String(), "Hello, World!\t42\n"; got != want {
		t.Errorf("output = %q; want %q", got, want)
	}
}

func TestMathLibrary(t *testing.T) {
	newState := func(t *testing.T, seed int64) *State {
		t.Helper()
		state := new(State)
		t.Cleanup(func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		})
		if err := Require(state, MathLibraryName, true, NewOpenMath(rand.NewSource(seed))); err != nil {
			t.Fatal(err)
		}
		return state
	}

	t.Run("Random", func(t *testing.T) {
		t.Run("Float", func(t *testing.T) {
			for seed := int64(0); seed < 100; seed++ {
				r := rand.New(rand.NewSource(seed))
				want := r.Float64()

				state := newState(t, seed)
				state.RawField(1, "random")
				if err := state.Call(0, 1, 0); err != nil {
					t.Fatal(err)
				}
				got, _ := state.ToNumber(-1)
				if got != want {
					t.Errorf("seed = %d: math.random() = %g; want %g", seed, got, want)
				}
				state.Pop(1)
			}
		})

		tests := []struct {
			name string
			args []int64
			want func(r *rand.Rand) int64
		}{
			{
				name: "SingleArg",
				args: []int64{10},
				want: func(r *rand.Rand) int64 { return 1 + r.Int63n(10) },
			},
			{
				name: "Range",
				args: []int64{5, 10},
				want: func(r *rand.Rand) int64 { return 5 + r.Int63n(6) },
			},
			{
				name: "Zero",
				args: []int64{0},
				want: func(r *rand.Rand) int64 { return int64(r.Uint64()) },
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				for seed := int64(0); seed < 100; seed++ {
					r := rand.New(rand.NewSource(seed))
					want := test.want(r)

					state := newState(t, seed)
					state.RawField(1, "random")
					for _, arg := range test.args {
						state.PushInteger(arg)
					}
					if err := state.Call(len(test.args), 1, 0); err != nil {
						t.Fatal(err)
					}
					got, _ := state.ToInteger(-1)
					if got != want {
						argstr := new(strings.Builder)
						for i, arg := range test.args {
							if i > 0 {
								argstr.WriteString(", ")
							}
							fmt.Fprint(argstr, arg)
						}
						t.Errorf("seed = %d: math.random(%v) = %d; want %d", seed, argstr, got, want)
					}
					state.Pop(1)
				}
			})
		}
	})
}
