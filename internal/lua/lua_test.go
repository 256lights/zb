// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zb.256lights.llc/pkg/internal/luacode"
	"zb.256lights.llc/pkg/internal/lualex"
	"zb.256lights.llc/pkg/sets"
)

func TestClose(t *testing.T) {
	ctx := context.Background()
	state := new(State)
	defer func() {
		if err := state.Close(); err != nil {
			t.Error("Close:", err)
		}
	}()

	state.PushBoolean(true)
	state.PushInteger(42)
	state.PushString("hello")
	state.PushValue(-1)
	if err := state.SetGlobal(ctx, "x"); err != nil {
		t.Error(err)
	}
	if tp, err := state.Global(ctx, "x"); err != nil {
		t.Error(err)
	} else if tp != TypeString {
		t.Errorf("type(_G.x) = %v; want %v", tp, TypeString)
	} else if got, _ := state.ToString(-1); got != "hello" {
		t.Errorf("_G.x = %q; want %q", got, "hello")
	}
	state.Pop(1)
	if got, want := state.Top(), 3; got != want {
		t.Errorf("before close, state.Top() = %d; want %d", got, want)
	}

	if err := state.Close(); err != nil {
		t.Error("Close:", err)
	}
	if got, want := state.Top(), 0; got != want {
		t.Errorf("after close, state.Top() = %d; want %d", got, want)
	}
	if tp, err := state.Global(ctx, "x"); err != nil {
		t.Error(err)
	} else if tp != TypeNil {
		t.Errorf("type(_G.x) = %v; want %v", tp, TypeNil)
	}
	state.Pop(1)
}

func TestLoad(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		ctx := context.Background()
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
		if err := state.Call(ctx, 0, 1); err != nil {
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

	t.Run("Binary", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const source = "return 2 + 2"
		proto, err := luacode.Parse(source, strings.NewReader(source))
		if err != nil {
			t.Fatal(err)
		}
		chunk, err := proto.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}

		if err := state.Load(bytes.NewReader(chunk), "", "b"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
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

	t.Run("Autodetect", func(t *testing.T) {
		const source = "return 2 + 2"
		proto, err := luacode.Parse(source, strings.NewReader(source))
		if err != nil {
			t.Fatal(err)
		}
		chunk, err := proto.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		tests := []struct {
			name string
			data []byte
		}{
			{name: "Text", data: []byte(source)},
			{name: "Binary", data: chunk},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				ctx := context.Background()
				state := new(State)
				defer func() {
					if err := state.Close(); err != nil {
						t.Error("Close:", err)
					}
				}()

				if err := state.Load(bytes.NewReader(test.data), source, "bt"); err != nil {
					t.Fatal(err)
				}
				if err := state.Call(ctx, 0, 1); err != nil {
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
		err := state.Load(bufio.NewReader(r), "=(reader)", "t")
		if err == nil {
			t.Error("state.Load(...) = <nil>; want error")
		} else if got := err.Error(); !strings.Contains(got, message) {
			t.Errorf("state.Load(...) = %v; want to contain %q", got, message)
		}
		if got, want := state.Top(), 0; got != want {
			t.Errorf("state.Top() = %d; want %d", got, want)
		}
	})
}

func TestCompare(t *testing.T) {
	type compareTable [3]int8
	const bad int8 = -1

	tests := []struct {
		name string
		push func(l *State)
		want compareTable
	}{
		{
			name: "StringNumber",
			push: func(l *State) {
				l.PushString("0")
				l.PushInteger(0)
			},
			want: compareTable{
				Equal:       0,
				Less:        bad,
				LessOrEqual: bad,
			},
		},
		{
			name: "NumberString",
			push: func(l *State) {
				l.PushInteger(0)
				l.PushString("0")
			},
			want: compareTable{
				Equal:       0,
				Less:        bad,
				LessOrEqual: bad,
			},
		},
		{
			name: "IntegerFloat",
			push: func(l *State) {
				l.PushInteger(42)
				l.PushNumber(42)
			},
			want: compareTable{
				Equal:       1,
				Less:        0,
				LessOrEqual: 1,
			},
		},
		{
			name: "MaxIntNegativeMinInt",
			push: func(l *State) {
				l.PushInteger(math.MaxInt)
				l.PushNumber(float64(-math.MinInt))
			},
			want: compareTable{
				Equal:       0,
				Less:        1,
				LessOrEqual: 1,
			},
		},
		{
			name: "NaNZero",
			push: func(l *State) {
				l.PushNumber(math.NaN())
				l.PushNumber(0)
			},
			want: compareTable{
				Equal:       0,
				Less:        0,
				LessOrEqual: 0,
			},
		},
		{
			name: "NaNIntegerZero",
			push: func(l *State) {
				l.PushNumber(math.NaN())
				l.PushInteger(0)
			},
			want: compareTable{
				Equal:       0,
				Less:        0,
				LessOrEqual: 0,
			},
		},
		{
			name: "TwoNaNs",
			push: func(l *State) {
				l.PushNumber(math.NaN())
				l.PushNumber(math.NaN())
			},
			want: compareTable{
				Equal:       0,
				Less:        0,
				LessOrEqual: 0,
			},
		},
		{
			name: "FloatInteger",
			push: func(l *State) {
				l.PushNumber(42)
				l.PushInteger(42)
			},
			want: compareTable{
				Equal:       1,
				Less:        0,
				LessOrEqual: 1,
			},
		},
		{
			name: "EqualIntegers",
			push: func(l *State) {
				l.PushInteger(42)
				l.PushInteger(42)
			},
			want: compareTable{
				Equal:       1,
				Less:        0,
				LessOrEqual: 1,
			},
		},
		{
			name: "AscendingIntegers",
			push: func(l *State) {
				l.PushInteger(42)
				l.PushInteger(100)
			},
			want: compareTable{
				Equal:       0,
				Less:        1,
				LessOrEqual: 1,
			},
		},
		{
			name: "DescendingIntegers",
			push: func(l *State) {
				l.PushInteger(100)
				l.PushInteger(42)
			},
			want: compareTable{
				Equal:       0,
				Less:        0,
				LessOrEqual: 0,
			},
		},
		{
			name: "EmptyStrings",
			push: func(l *State) {
				l.PushString("")
				l.PushString("")
			},
			want: compareTable{
				Equal:       1,
				Less:        0,
				LessOrEqual: 1,
			},
		},
		{
			name: "EmptyNonEmptyString",
			push: func(l *State) {
				l.PushString("")
				l.PushString("abc")
			},
			want: compareTable{
				Equal:       0,
				Less:        1,
				LessOrEqual: 1,
			},
		},
		{
			name: "NonEmptyEmptyString",
			push: func(l *State) {
				l.PushString("abc")
				l.PushString("")
			},
			want: compareTable{
				Equal:       0,
				Less:        0,
				LessOrEqual: 0,
			},
		},
		{
			name: "FalseTrue",
			push: func(l *State) {
				l.PushBoolean(false)
				l.PushBoolean(true)
			},
			want: compareTable{
				Equal:       0,
				Less:        bad,
				LessOrEqual: bad,
			},
		},
		{
			name: "TrueFalse",
			push: func(l *State) {
				l.PushBoolean(true)
				l.PushBoolean(false)
			},
			want: compareTable{
				Equal:       0,
				Less:        bad,
				LessOrEqual: bad,
			},
		},
		{
			name: "TwoFalse",
			push: func(l *State) {
				l.PushBoolean(false)
				l.PushBoolean(false)
			},
			want: compareTable{
				Equal:       1,
				Less:        bad,
				LessOrEqual: bad,
			},
		},
		{
			name: "TwoTrue",
			push: func(l *State) {
				l.PushBoolean(true)
				l.PushBoolean(true)
			},
			want: compareTable{
				Equal:       1,
				Less:        bad,
				LessOrEqual: bad,
			},
		},
	}

	t.Run("StateMethod", func(t *testing.T) {
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				ctx := context.Background()
				state := new(State)
				defer func() {
					if err := state.Close(); err != nil {
						t.Error("Close:", err)
					}
				}()

				test.push(state)
				s1 := describeValue(state, -2)
				s2 := describeValue(state, -1)

				for opIndex, want := range test.want {
					op := ComparisonOperator(opIndex)
					got, err := state.Compare(ctx, -2, -1, op)
					if got != (want == 1) || (err != nil) != (want == bad) {
						wantError := "<nil>"
						if want == bad {
							wantError = "<error>"
						}
						t.Errorf("(%s %v %s) = %t, %v; want %t, %s",
							s1, op, s2, got, err, (want == 1), wantError)
					}
				}
			})
		}
	})

	t.Run("Load", func(t *testing.T) {
		// Parse scripts for comparing two arguments.
		scripts := [len(compareTable{})][]byte{}
		for i := range scripts {
			op := ComparisonOperator(i)
			source := "local x, y = ...\nreturn x " + op.String() + " y\n"
			proto, err := luacode.Parse(Source(source), strings.NewReader(source))
			if err != nil {
				t.Fatal(err)
			}
			scripts[i], err = proto.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				ctx := context.Background()
				state := new(State)
				defer func() {
					if err := state.Close(); err != nil {
						t.Error("Close:", err)
					}
				}()

				test.push(state)
				s1 := describeValue(state, -2)
				s2 := describeValue(state, -1)

				for i, want := range test.want {
					op := ComparisonOperator(i)
					if err := state.Load(bytes.NewReader(scripts[i]), "", "b"); err != nil {
						t.Error("Load:", err)
						continue
					}

					// Copy pushed values on top of function pushed.
					state.PushValue(-3)
					state.PushValue(-3)

					if err := state.Call(ctx, 2, 1); err != nil {
						t.Logf("(%s %v %s): %v", s1, op, s2, err)
						if want != bad {
							t.Fail()
						}
						continue
					}
					if want == bad {
						t.Fatalf("Comparison did not throw an error")
					}

					if got, want := state.Type(-1), TypeBoolean; got != want {
						t.Errorf("(%s %v %s) returned %v; want %v",
							s1, op, s2, got, want)
					}
					got := state.ToBoolean(-1)
					if got != (want == 1) {
						t.Errorf("(%s %v %s) = %t, <nil>; want %t, <nil>",
							s1, op, s2, got, (want == 1))
					}
					state.Pop(1)
				}
			})
		}
	})
}

func TestConcat(t *testing.T) {
	tests := []struct {
		name        string
		push        func(l *State)
		want        string
		wantContext sets.Set[string]
	}{
		{
			name: "Empty",
			push: func(l *State) {},
			want: "",
		},
		{
			name: "SingleString",
			push: func(l *State) {
				l.PushString("abc")
			},
			want: "abc",
		},
		{
			name: "SingleStringWithContext",
			push: func(l *State) {
				l.PushStringContext("abc", sets.New("1", "2", "3"))
			},
			want:        "abc",
			wantContext: sets.New("1", "2", "3"),
		},
		{
			name: "TwoStrings",
			push: func(l *State) {
				l.PushString("abc")
				l.PushString("def")
			},
			want: "abcdef",
		},
		{
			name: "TwoStringsWithContext",
			push: func(l *State) {
				l.PushStringContext("abc", sets.New("1", "2", "3"))
				l.PushStringContext("def", sets.New("4", "5", "6"))
			},
			want:        "abcdef",
			wantContext: sets.New("1", "2", "3", "4", "5", "6"),
		},
		{
			name: "ThreeStrings",
			push: func(l *State) {
				l.PushString("abc")
				l.PushString("def")
				l.PushString("ghi")
			},
			want: "abcdefghi",
		},
		{
			name: "ThreeStringsWithContext",
			push: func(l *State) {
				l.PushStringContext("abc", sets.New("1", "2", "3"))
				l.PushStringContext("def", sets.New("4", "5", "6"))
				l.PushStringContext("ghi", sets.New("7", "8", "9"))
			},
			want:        "abcdefghi",
			wantContext: sets.New("1", "2", "3", "4", "5", "6", "7", "8", "9"),
		},
		{
			name: "StringWithInteger",
			push: func(l *State) {
				l.PushString("abc")
				l.PushInteger(12)
			},
			want: "abc12",
		},
		{
			name: "EmptyStringWithInteger",
			push: func(l *State) {
				l.PushString("")
				l.PushInteger(12)
			},
			want: "12",
		},
		{
			name: "FloatWithString",
			push: func(l *State) {
				l.PushNumber(12)
				l.PushString("xyz")
			},
			want: "12.0xyz",
		},
		{
			name: "FloatWithEmptyString",
			push: func(l *State) {
				l.PushNumber(12)
				l.PushString("")
			},
			want: "12.0",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			state := new(State)
			defer func() {
				if err := state.Close(); err != nil {
					t.Error("Close:", err)
				}
			}()
			test.push(state)

			n := state.Top()
			if err := state.Concat(ctx, n); err != nil {
				t.Errorf("state.Concat(%d, 0): %v", n, err)
			}

			if got, want := state.Top(), 1; got != want {
				t.Errorf("after Concat(%d, 0), state.Top() = %d; want %d", n, got, want)
			}
			if got, want := state.Type(1), TypeString; got != want {
				t.Errorf("state.Type(1) = %v; want %v", got, want)
			}
			if got, _ := state.ToString(1); got != test.want {
				t.Errorf("result = %q; want %q", got, test.want)
			}
			gotContext := state.StringContext(1)
			if diff := cmp.Diff(test.wantContext, gotContext, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("context (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFullUserdata(t *testing.T) {
	state := new(State)
	defer func() {
		if err := state.Close(); err != nil {
			t.Error("Close:", err)
		}
	}()

	initValue := [4]byte{}
	state.NewUserdata(initValue, 1)
	if got, want := state.RawLen(-1), uint64(0); got != want {
		t.Errorf("state.RawLen(-1) = %d; want %d", got, want)
	}
	if got, ok := state.ToUserdata(-1); got != initValue || !ok {
		t.Errorf("state.ToUserdata(...) = %#v, %t; want %#v, true", got, ok, initValue)
	}

	const wantUserValue = 42
	state.PushInteger(wantUserValue)
	if err := state.SetUserValue(-2, 1); err != nil {
		t.Error("state.SetUserValue(-2, 1):", err)
	}
	if got, want := state.UserValue(-1, 1), TypeNumber; got != want {
		t.Errorf("user value 1 type = %v; want %v", got, want)
	}
	if got, ok := state.ToInteger(-1); got != wantUserValue || !ok {
		t.Errorf("user value 1 = %s; want %d", describeValue(state, -1), wantUserValue)
	}
	state.Pop(1)

	if got, want := state.UserValue(-1, 2), TypeNone; got != want {
		t.Errorf("user value 2 type = %v; want %v", got, want)
	}
	if got, want := state.Top(), 2; got != want {
		t.Errorf("after state.UserValue(-1, 2), state.Top() = %d; want %d", got, want)
	}
	if !state.IsNil(-1) {
		t.Errorf("user value 2 = %s; want nil", describeValue(state, -2))
	}
}

func TestMessageHandler(t *testing.T) {
	t.Run("DivideByZero", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		// Message handler.
		const handledMessage = "uwu"
		messageHandlerCallCount := 0
		state.PushClosure(0, func(ctx context.Context, l *State) (int, error) {
			messageHandlerCallCount++

			if got, want := l.Top(), 1; got != want {
				t.Errorf("l.Top() = %d; want %d", got, want)
			}
			wantMessage := luacode.ErrDivideByZero.Error()
			if got, want := l.Type(1), TypeString; got != want {
				t.Errorf("l.Type(1) = %v; want %v", got, want)
			} else if got, ok := l.ToString(1); !strings.Contains(got, wantMessage) || !ok {
				t.Errorf("l.ToString(1) = %q, %t; want to contain %q", got, ok, wantMessage)
			}

			scriptDebug := l.Info(1)
			if scriptDebug == nil {
				t.Error("l.Info(1) = nil")
			} else {
				if got, want := scriptDebug.What, "main"; got != want {
					t.Errorf("l.Info(1).What = %q; want %q", got, want)
				}
				if got, want := scriptDebug.CurrentLine, 2; got != want {
					t.Errorf("l.Info(1).CurrentLine = %d; want %d", got, want)
				}
			}

			l.PushString(handledMessage)
			return 1, nil
		})

		const source = `-- Comment here to advance line number.` + "\n" +
			`return 5 // 0` + "\n"
		if err := state.Load(strings.NewReader(source), LiteralSource(source), "t"); err != nil {
			t.Fatal(err)
		}

		if err := state.PCall(ctx, 0, 0, -2); err == nil {
			t.Error("Running script did not return an error")
		} else if got, want := err.Error(), handledMessage; got != want {
			t.Errorf("state.Call(...).Error() = %q; want %q", got, want)
		}
		if messageHandlerCallCount != 1 {
			t.Errorf("message handler called %d times; want 1", messageHandlerCallCount)
		}

		const errorObjectIndex = 2
		if got, want := state.Top(), errorObjectIndex; got != want {
			t.Errorf("after state.Call(...), state.Top() = %d; want %d", got, want)
		}
		if state.Top() >= errorObjectIndex {
			if got, want := state.Type(errorObjectIndex), TypeString; got != want {
				t.Errorf("after state.Call(...), state.Type(%d) = %v; want %v", errorObjectIndex, got, want)
			} else {
				got, ok := state.ToString(errorObjectIndex)
				want := handledMessage
				if !ok || got != want {
					t.Errorf("after state.Call(...), state.ToString(%d) = %q, %t; want %q, true", errorObjectIndex, got, ok, want)
				}
			}
		}
	})

	t.Run("FromGo", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		// Stripped down version of _G.error.
		state.PushClosure(0, func(ctx context.Context, l *State) (int, error) {
			msg, _ := l.ToString(1)
			return 0, errors.New(msg)
		})
		if err := state.SetGlobal(ctx, "error"); err != nil {
			t.Fatal(err)
		}

		// Message handler.
		const originalMessage = "bork"
		const handledMessage = "uwu"
		messageHandlerCallCount := 0
		state.PushClosure(0, func(ctx context.Context, l *State) (int, error) {
			messageHandlerCallCount++

			if got, want := l.Top(), 1; got != want {
				t.Errorf("l.Top() = %d; want %d", got, want)
			}
			if got, want := l.Type(1), TypeString; got != want {
				t.Errorf("l.Type(1) = %v; want %v", got, want)
			} else if got, ok := l.ToString(1); got != originalMessage || !ok {
				t.Errorf("l.ToString(1) = %q, %t; want %q, true", got, ok, originalMessage)
			}

			errFuncDebug := l.Info(1)
			if errFuncDebug == nil {
				t.Error("l.Info(1) = nil")
			} else {
				if got, want := errFuncDebug.What, "Go"; got != want {
					t.Errorf("l.Info(1).What = %q; want %q", got, want)
				}
			}

			scriptDebug := l.Info(2)
			if scriptDebug == nil {
				t.Error("l.Info(2) = nil")
			} else {
				if got, want := scriptDebug.What, "main"; got != want {
					t.Errorf("l.Info(2).What = %q; want %q", got, want)
				}
				if got, want := scriptDebug.CurrentLine, 2; got != want {
					t.Errorf("l.Info(2).CurrentLine = %d; want %d", got, want)
				}
			}

			l.PushString(handledMessage)
			return 1, nil
		})

		const source = `-- Comment here to advance line number.` + "\n" +
			`error("bork")` + "\n"
		if err := state.Load(strings.NewReader(source), LiteralSource(source), "t"); err != nil {
			t.Fatal(err)
		}

		if err := state.PCall(ctx, 0, 0, -2); err == nil {
			t.Error("Running script did not return an error")
		} else if got, want := err.Error(), handledMessage; got != want {
			t.Errorf("state.Call(...).Error() = %q; want %q", got, want)
		}
		if messageHandlerCallCount != 1 {
			t.Errorf("message handler called %d times; want 1", messageHandlerCallCount)
		}

		const errorObjectIndex = 2
		if got, want := state.Top(), errorObjectIndex; got != want {
			t.Errorf("after state.Call(...), state.Top() = %d; want %d", got, want)
		}
		if state.Top() >= errorObjectIndex {
			if got, want := state.Type(errorObjectIndex), TypeString; got != want {
				t.Errorf("after state.Call(...), state.Type(%d) = %v; want %v", errorObjectIndex, got, want)
			} else {
				got, ok := state.ToString(errorObjectIndex)
				want := handledMessage
				if !ok || got != want {
					t.Errorf("after state.Call(...), state.ToString(%d) = %q, %t; want %q, true", errorObjectIndex, got, ok, want)
				}
			}
		}
	})

	t.Run("ErrorDuringMessageHandler", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		// Message handler.
		const firstHandleErrorMessage = "O_O"
		const handledMessage = "uwu"
		messageHandlerCallCount := 0
		state.PushClosure(0, func(ctx context.Context, l *State) (int, error) {
			messageHandlerCallCount++

			if got, want := l.Top(), 1; got != want {
				t.Errorf("l.Top() = %d; want %d", got, want)
			}

			if messageHandlerCallCount == 1 {
				scriptDebug := l.Info(1)
				if scriptDebug == nil {
					t.Error("l.Info(1) = nil")
				} else {
					if got, want := scriptDebug.What, "main"; got != want {
						t.Errorf("l.Info(1).What = %q; want %q", got, want)
					}
					if got, want := scriptDebug.CurrentLine, 2; got != want {
						t.Errorf("l.Info(1).CurrentLine = %d; want %d", got, want)
					}
				}
				return 0, errors.New(firstHandleErrorMessage)
			}

			// On subsequent calls, handle the error.
			if got, want := l.Type(1), TypeString; got != want {
				t.Errorf("l.Type(1) = %v; want %v", got, want)
			} else if got, ok := l.ToString(1); got != firstHandleErrorMessage || !ok {
				t.Errorf("l.ToString(1) = %q, %t; want %q, true", got, ok, firstHandleErrorMessage)
			}

			errFuncDebug := l.Info(1)
			if errFuncDebug == nil {
				t.Error("l.Info(1) = nil")
			} else {
				if got, want := errFuncDebug.What, "Go"; got != want {
					t.Errorf("l.Info(1).What = %q; want %q", got, want)
				}
			}

			scriptDebug := l.Info(2)
			if scriptDebug == nil {
				t.Error("l.Info(2) = nil")
			} else {
				if got, want := scriptDebug.What, "main"; got != want {
					t.Errorf("l.Info(2).What = %q; want %q", got, want)
				}
				if got, want := scriptDebug.CurrentLine, 2; got != want {
					t.Errorf("l.Info(2).CurrentLine = %d; want %d", got, want)
				}
			}

			l.PushString(handledMessage)
			return 1, nil
		})

		const source = `-- Comment here to advance line number.` + "\n" +
			`return 5 // 0` + "\n"
		if err := state.Load(strings.NewReader(source), LiteralSource(source), "t"); err != nil {
			t.Fatal(err)
		}

		if err := state.PCall(ctx, 0, 0, -2); err == nil {
			t.Error("Running script did not return an error")
		} else if got, want := err.Error(), handledMessage; got != want {
			t.Errorf("state.Call(...).Error() = %q; want %q", got, want)
		}
		if messageHandlerCallCount != 2 {
			t.Errorf("message handler called %d times; want 2", messageHandlerCallCount)
		}

		const errorObjectIndex = 2
		if got, want := state.Top(), errorObjectIndex; got != want {
			t.Errorf("after state.Call(...), state.Top() = %d; want %d", got, want)
		}
		if state.Top() >= errorObjectIndex {
			if got, want := state.Type(errorObjectIndex), TypeString; got != want {
				t.Errorf("after state.Call(...), state.Type(%d) = %v; want %v", errorObjectIndex, got, want)
			} else {
				got, ok := state.ToString(errorObjectIndex)
				want := handledMessage
				if !ok || got != want {
					t.Errorf("after state.Call(...), state.ToString(%d) = %q, %t; want %q, true", errorObjectIndex, got, ok, want)
				}
			}
		}
	})

	t.Run("CrossesCall", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const originalMessage = "bork"
		state.PushClosure(0, func(ctx context.Context, l *State) (int, error) {
			// Call a Go function using the Lua API that raises an error.
			l.PushClosure(0, func(ctx context.Context, l *State) (int, error) {
				return 0, errors.New(originalMessage)
			})
			if err := l.Call(ctx, 0, 0); err != nil {
				return 0, err
			}
			t.Error("Calling error-raising function did not raise an error.")
			return 0, nil
		})
		if err := state.SetGlobal(ctx, "foo"); err != nil {
			t.Fatal(err)
		}

		// Message handler.
		const handledMessage = "uwu"
		messageHandlerCallCount := 0
		state.PushClosure(0, func(ctx context.Context, l *State) (int, error) {
			messageHandlerCallCount++

			if got, want := l.Top(), 1; got != want {
				t.Errorf("l.Top() = %d; want %d", got, want)
			}
			if got, want := l.Type(1), TypeString; got != want {
				t.Errorf("l.Type(1) = %v; want %v", got, want)
			} else if got, ok := l.ToString(1); got != originalMessage || !ok {
				t.Errorf("l.ToString(1) = %q, %t; want %q, true", got, ok, originalMessage)
			}

			errFuncDebug := l.Info(1)
			if errFuncDebug == nil {
				t.Error("l.Info(1) = nil")
			} else {
				if got, want := errFuncDebug.What, "Go"; got != want {
					t.Errorf("l.Info(1).What = %q; want %q", got, want)
				}
			}

			goFuncDebug := l.Info(2)
			if goFuncDebug == nil {
				t.Error("l.Info(2) = nil")
			} else {
				if got, want := goFuncDebug.What, "Go"; got != want {
					t.Errorf("l.Info(2).What = %q; want %q", got, want)
				}
			}

			scriptDebug := l.Info(3)
			if scriptDebug == nil {
				t.Error("l.Info(3) = nil")
			} else {
				if got, want := scriptDebug.What, "main"; got != want {
					t.Errorf("l.Info(3).What = %q; want %q", got, want)
				}
				if got, want := scriptDebug.CurrentLine, 2; got != want {
					t.Errorf("l.Info(3).CurrentLine = %d; want %d", got, want)
				}
			}

			l.PushString(handledMessage)
			return 1, nil
		})

		const source = `-- Comment here to advance line number.` + "\n" +
			`foo()` + "\n"
		if err := state.Load(strings.NewReader(source), LiteralSource(source), "t"); err != nil {
			t.Fatal(err)
		}

		if err := state.PCall(ctx, 0, 0, -2); err == nil {
			t.Error("Running script did not return an error")
		} else if got, want := err.Error(), handledMessage; got != want {
			t.Errorf("state.Call(...).Error() = %q; want %q", got, want)
		}
		if messageHandlerCallCount != 1 {
			t.Errorf("message handler called %d times; want 1", messageHandlerCallCount)
		}

		const errorObjectIndex = 2
		if got, want := state.Top(), errorObjectIndex; got != want {
			t.Errorf("after state.Call(...), state.Top() = %d; want %d", got, want)
		}
		if state.Top() >= errorObjectIndex {
			if got, want := state.Type(errorObjectIndex), TypeString; got != want {
				t.Errorf("after state.Call(...), state.Type(%d) = %v; want %v", errorObjectIndex, got, want)
			} else {
				got, ok := state.ToString(errorObjectIndex)
				want := handledMessage
				if !ok || got != want {
					t.Errorf("after state.Call(...), state.ToString(%d) = %q, %t; want %q, true", errorObjectIndex, got, ok, want)
				}
			}
		}
	})

	t.Run("DoesNotCrossPCall", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const originalMessage = "bork"
		state.PushClosure(0, func(ctx context.Context, l *State) (int, error) {
			// Call a Go function using the Lua API that raises an error.
			l.PushClosure(0, func(ctx context.Context, l *State) (int, error) {
				return 0, errors.New(originalMessage)
			})
			if err := l.PCall(ctx, 0, 0, 0); err != nil {
				return 0, err
			}
			t.Error("Calling error-raising function did not raise an error.")
			return 0, nil
		})
		if err := state.SetGlobal(ctx, "foo"); err != nil {
			t.Fatal(err)
		}

		// Message handler.
		const handledMessage = "uwu"
		messageHandlerCallCount := 0
		state.PushClosure(0, func(ctx context.Context, l *State) (int, error) {
			messageHandlerCallCount++

			if got, want := l.Top(), 1; got != want {
				t.Errorf("l.Top() = %d; want %d", got, want)
			}
			if got, want := l.Type(1), TypeString; got != want {
				t.Errorf("l.Type(1) = %v; want %v", got, want)
			} else if got, ok := l.ToString(1); got != originalMessage || !ok {
				t.Errorf("l.ToString(1) = %q, %t; want %q, true", got, ok, originalMessage)
			}

			errFuncDebug := l.Info(1)
			if errFuncDebug == nil {
				t.Error("l.Info(1) = nil")
			} else {
				if got, want := errFuncDebug.What, "Go"; got != want {
					t.Errorf("l.Info(1).What = %q; want %q", got, want)
				}
			}

			scriptDebug := l.Info(2)
			if scriptDebug == nil {
				t.Error("l.Info(2) = nil")
			} else {
				if got, want := scriptDebug.What, "main"; got != want {
					t.Errorf("l.Info(2).What = %q; want %q", got, want)
				}
				if got, want := scriptDebug.CurrentLine, 2; got != want {
					t.Errorf("l.Info(2).CurrentLine = %d; want %d", got, want)
				}
			}

			l.PushString(handledMessage)
			return 1, nil
		})

		const source = `-- Comment here to advance line number.` + "\n" +
			`foo()` + "\n"
		if err := state.Load(strings.NewReader(source), LiteralSource(source), "t"); err != nil {
			t.Fatal(err)
		}

		if err := state.PCall(ctx, 0, 0, -2); err == nil {
			t.Error("Running script did not return an error")
		} else if got, want := err.Error(), handledMessage; got != want {
			t.Errorf("state.Call(...).Error() = %q; want %q", got, want)
		}
		if messageHandlerCallCount != 1 {
			t.Errorf("message handler called %d times; want 1", messageHandlerCallCount)
		}

		const errorObjectIndex = 2
		if got, want := state.Top(), errorObjectIndex; got != want {
			t.Errorf("after state.Call(...), state.Top() = %d; want %d", got, want)
		}
		if state.Top() >= errorObjectIndex {
			if got, want := state.Type(errorObjectIndex), TypeString; got != want {
				t.Errorf("after state.Call(...), state.Type(%d) = %v; want %v", errorObjectIndex, got, want)
			} else {
				got, ok := state.ToString(errorObjectIndex)
				want := handledMessage
				if !ok || got != want {
					t.Errorf("after state.Call(...), state.ToString(%d) = %q, %t; want %q, true", errorObjectIndex, got, ok, want)
				}
			}
		}
	})
}

func TestFreeze(t *testing.T) {
	t.Run("Nil", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.PushNil()
		if err := state.Freeze(-1); err != nil {
			t.Error("state.Freeze(-1):", err)
		}
		if got, want := state.Top(), 1; got != want {
			t.Errorf("after Freeze, state.Top() = %d; want %d", got, want)
		}
		if got, want := state.Type(1), TypeNil; got != want {
			t.Errorf("after Freeze, state.Type(1) = %v; want %v", got, want)
		}
	})

	t.Run("Number", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.PushInteger(42)
		if err := state.Freeze(-1); err != nil {
			t.Error("state.Freeze(-1):", err)
		}
		if got, want := state.Top(), 1; got != want {
			t.Errorf("after Freeze, state.Top() = %d; want %d", got, want)
		}
		if got, want := state.Type(1), TypeNumber; got != want {
			t.Errorf("after Freeze, state.Type(1) = %v; want %v", got, want)
		}
	})

	t.Run("TableSet", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.CreateTable(0, 0)
		tableIndex := state.Top()
		if err := state.Freeze(tableIndex); err != nil {
			t.Errorf("state.Freeze(%d): %v", tableIndex, err)
		}
		state.PushString("bar")
		if err := state.SetField(ctx, tableIndex, "foo"); err == nil {
			t.Error(`x = {}; freeze(x); x.foo = "bar" did not raise error`)
		} else if got, want := err.Error(), "frozen"; !strings.Contains(got, want) {
			t.Errorf("x = {}; freeze(x); x.foo = \"bar\" error = %q; want %q", got, want)
		} else {
			t.Logf("x = {}; freeze(x); x.foo = \"bar\" raised: %s", got)
		}

		if tp, err := state.Field(ctx, tableIndex, "foo"); err != nil {
			t.Error("x.foo:", err)
		} else if tp != TypeNil {
			t.Errorf("type(x.foo) = %v; want nil", tp)
		}
	})

	t.Run("NestedTable", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.CreateTable(1, 0)
		table1Index := state.Top()
		state.CreateTable(0, 1)
		table2Index := state.Top()
		state.PushValue(table2Index)
		if err := state.RawSetIndex(table1Index, 1); err != nil {
			t.Fatal("table1 = {}; table2 = {}; table1[1] = table2:", err)
		}

		if err := state.Freeze(table1Index); err != nil {
			t.Errorf("state.Freeze(%d): %v", table1Index, err)
		}
		state.PushString("bar")
		if err := state.SetField(ctx, table2Index, "foo"); err == nil {
			t.Error(`x = {}; freeze(x); x.foo = "bar" did not raise error`)
		} else if got, want := err.Error(), "frozen"; !strings.Contains(got, want) {
			t.Errorf("x = {}; freeze(x); x.foo = \"bar\" error = %q; want %q", got, want)
		} else {
			t.Logf("x = {}; freeze(x); x.foo = \"bar\" raised: %s", got)
		}

		if tp, err := state.Field(ctx, table2Index, "foo"); err != nil {
			t.Error("x.foo:", err)
		} else if tp != TypeNil {
			t.Errorf("type(x.foo) = %v; want nil", tp)
		}
	})

	t.Run("CyclicTable", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.CreateTable(1, 0)
		tableIndex := state.Top()
		state.PushValue(tableIndex)
		if err := state.RawSetIndex(tableIndex, 1); err != nil {
			t.Fatal("table = {}; table[1] = table:", err)
		}

		if err := state.Freeze(tableIndex); err != nil {
			t.Errorf("state.Freeze(%d): %v", tableIndex, err)
		}
		state.PushString("bar")
		if err := state.SetField(ctx, tableIndex, "foo"); err == nil {
			t.Error(`x = {}; freeze(x); x.foo = "bar" did not raise error`)
		} else if got, want := err.Error(), "frozen"; !strings.Contains(got, want) {
			t.Errorf("x = {}; freeze(x); x.foo = \"bar\" error = %q; want %q", got, want)
		} else {
			t.Logf("x = {}; freeze(x); x.foo = \"bar\" raised: %s", got)
		}

		if tp, err := state.Field(ctx, tableIndex, "foo"); err != nil {
			t.Error("x.foo:", err)
		} else if tp != TypeNil {
			t.Errorf("type(x.foo) = %v; want nil", tp)
		}
	})

	t.Run("TableRawSet", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.CreateTable(0, 0)
		tableIndex := state.Top()
		if err := state.Freeze(tableIndex); err != nil {
			t.Errorf("state.Freeze(%d): %v", tableIndex, err)
		}
		state.PushString("bar")
		if err := state.RawSetField(tableIndex, "foo"); err == nil {
			t.Error(`x = {}; freeze(x); rawset(x, "foo", "bar") did not raise error`)
		} else if got, want := err.Error(), "frozen"; !strings.Contains(got, want) {
			t.Errorf("x = {}; freeze(x); rawset(x, \"foo\", \"bar\") error = %q; want %q", got, want)
		} else {
			t.Logf("x = {}; freeze(x); rawset(x, \"foo\", \"bar\") raised: %s", got)
		}

		if tp, err := state.Field(ctx, tableIndex, "foo"); err != nil {
			t.Error("x.foo:", err)
		} else if tp != TypeNil {
			t.Errorf("type(x.foo) = %v; want nil", tp)
		}
	})

	t.Run("GoClosure", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.PushClosure(0, func(ctx context.Context, l *State) (int, error) { return 0, nil })
		if err := state.Freeze(-1); err == nil {
			t.Error("Freeze on impure Go function did not return error")
		}
	})

	t.Run("PureFunction", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.PushInteger(42)
		callCount := 0
		state.PushPureFunction(1, func(ctx context.Context, l *State) (int, error) {
			callCount++

			state.PushInteger(-100)
			if err := state.Replace(UpvalueIndex(1)); err == nil {
				t.Error("state.Replace(UpvalueIndex(1)) did not return an error")
			}

			state.PushValue(UpvalueIndex(1))
			return 1, nil
		})

		if err := state.Freeze(-1); err != nil {
			t.Error("Freeze on pure Go function:", err)
		}

		if err := state.Call(ctx, 0, 1); err != nil {
			t.Error("Call(...):", err)
		}
		if callCount != 1 {
			t.Errorf("Go callback called %d times; want 1", callCount)
		}
		if got, want := state.Type(-1), TypeNumber; got != want {
			t.Errorf("type(f()) = %v; want %v", got, want)
		} else if got, ok := state.ToNumber(-1); !ok || got != 42 {
			t.Errorf("f() = %g; want 42", got)
		}
	})

	t.Run("UserdataWithoutFreezer", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		state.NewUserdata(struct{}{}, 1)

		if err := state.Freeze(-1); err == nil {
			t.Error("state.Freeze(-1) did not return an error")
		}

		state.PushBoolean(true)
		if err := state.SetUserValue(-2, 1); err != nil {
			t.Error("state.SetUserValue(-2, 1):", err)
		}
		if got, want := state.UserValue(-1, 1), TypeBoolean; got != want {
			t.Errorf("state.UserValue(-1, 1) = %v; want %v", got, want)
		} else if got := state.ToBoolean(-1); !got {
			t.Error("user value 1 = false; want true")
		}
	})

	t.Run("UserdataWithFreezer", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		freezer := &freezeSpy{}
		state.NewUserdata(freezer, 1)

		if err := state.Freeze(-1); err != nil {
			t.Error("state.Freeze(-1):", err)
		}
		if freezer.callCount != 1 {
			t.Errorf("Freeze() called %d times; want 1", freezer.callCount)
		}

		state.PushBoolean(true)
		if err := state.SetUserValue(-2, 1); err == nil {
			t.Error("state.SetUserValue(-2, 1) did not return an error")
		} else if got, want := err.Error(), "frozen"; !strings.Contains(got, want) {
			t.Errorf("state.SetUserValue(-2, 1) = %q; want to contain %q", got, want)
		}
		if got, want := state.UserValue(-1, 1), TypeNil; got != want {
			t.Errorf("state.UserValue(-1, 1) = %v; want %v", got, want)
		}
	})

	t.Run("UserdataWithFailingFreezer", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		freezer := &freezeSpy{err: errors.New("cannot freeze")}
		state.NewUserdata(freezer, 1)

		if err := state.Freeze(-1); err == nil {
			t.Error("state.Freeze(-1) did not return an error")
		}
		if freezer.callCount != 1 {
			t.Errorf("Freeze() called %d times; want 1", freezer.callCount)
		}

		state.PushBoolean(true)
		if err := state.SetUserValue(-2, 1); err != nil {
			t.Error("state.SetUserValue(-2, 1):", err)
		}
		if got, want := state.UserValue(-1, 1), TypeBoolean; got != want {
			t.Errorf("state.UserValue(-1, 1) = %v; want %v", got, want)
		} else if got := state.ToBoolean(-1); !got {
			t.Error("user value 1 = false; want true")
		}
	})

	t.Run("LuaFunction", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const source = `local x = 1` + "\n" +
			`return function()` + "\n" +
			`local y = x` + "\n" +
			`x = y + 1` + "\n" +
			`return y` + "\n" +
			"end\n"
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
			t.Fatal(err)
		}

		if err := state.Freeze(-1); err != nil {
			t.Error("state.Freeze(-1):", err)
		}
		if err := state.Call(ctx, 0, 0); err == nil {
			t.Error("f = load(...)(); freeze(f); f() did not return an error")
		} else if got, want := err.Error(), "frozen"; !strings.Contains(got, want) {
			t.Errorf("f = load(...)(); freeze(f); f() raised %q; want to contain %q", got, want)
		} else {
			t.Logf("f = load(...)(); freeze(f); f(): %s", got)
		}
	})

	t.Run("SetFrozenUpvalueFromOtherFunction", func(t *testing.T) {
		ctx := context.Background()
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const source = `local x = 1` + "\n" +
			`return function()` + "\n" +
			`local y = x` + "\n" +
			`x = y + 1` + "\n" +
			`return y` + "\n" +
			`end, function()` + "\n" +
			`return x` + "\n" +
			"end\n"
		if err := state.Load(strings.NewReader(source), Source(source), "t"); err != nil {
			t.Fatal(err)
		}
		if err := state.Call(ctx, 0, 2); err != nil {
			t.Fatal(err)
		}

		// Freeze the second function.
		if err := state.Freeze(-1); err != nil {
			t.Error("state.Freeze(-1):", err)
		}
		state.Pop(1)

		// Call the first function.
		if err := state.Call(ctx, 0, 0); err == nil {
			t.Error("f1, f2 = load(...)(); freeze(f2); f1() did not return an error")
		} else if got, want := err.Error(), "frozen"; !strings.Contains(got, want) {
			t.Errorf("f1, f2 = load(...)(); freeze(f2); f1() raised %q; want to contain %q", got, want)
		} else {
			t.Logf("f1, f2 = load(...)(); freeze(f2); f1(): %s", got)
		}
	})
}

func TestRotate(t *testing.T) {
	tests := []struct {
		s    []int
		n    int
		want []int
	}{
		{[]int{}, 0, []int{}},
		{[]int{1, 2, 3}, 0, []int{1, 2, 3}},
		{[]int{1, 2, 3}, 1, []int{3, 1, 2}},
		{[]int{1, 2, 3}, 2, []int{2, 3, 1}},
		{[]int{1, 2, 3}, 3, []int{1, 2, 3}},
		{[]int{1, 2, 3}, -1, []int{2, 3, 1}},
		{[]int{1, 2, 3}, -2, []int{3, 1, 2}},
	}
	for _, test := range tests {
		got := slices.Clone(test.s)
		rotate(got, test.n)
		if diff := cmp.Diff(test.want, got); diff != "" {
			t.Errorf("rotate(%v, %d) (-want +got):\n%s", test.s, test.n, diff)
		}
	}
}

func TestSuite(t *testing.T) {
	names := []string{
		"math",
		"pm",
		"strings",
		"utf8",
	}

	for _, name := range names {
		t.Run(strings.ToUpper(name[:1])+name[1:], func(t *testing.T) {
			ctx := context.Background()
			l := new(State)
			defer func() {
				if err := l.Close(); err != nil {
					t.Error("Close:", err)
				}
			}()
			if err := OpenLibraries(ctx, l); err != nil {
				t.Fatal(err)
			}
			l.PushBoolean(true)
			if err := l.SetGlobal(ctx, "_port"); err != nil {
				t.Fatal(err)
			}

			// Message handler.
			l.PushClosure(0, func(ctx context.Context, l *State) (int, error) {
				msg, _ := l.ToString(1)
				l.PushStringContext(Traceback(l, msg, 1), l.StringContext(1))
				return 1, nil
			})

			sourcePath := filepath.Join("testdata", "testsuite", name+".lua")
			sourceData, err := os.ReadFile(sourcePath)
			if err != nil {
				t.Fatal(err)
			}
			err = l.Load(bytes.NewReader(sourceData), FilenameSource(sourcePath), "t")
			if err != nil {
				t.Fatal(err)
			}
			if err := l.PCall(ctx, 0, 0, -2); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func BenchmarkExec(b *testing.B) {
	ctx := context.Background()
	state := new(State)
	defer func() {
		if err := state.Close(); err != nil {
			b.Error("Close:", err)
		}
	}()

	const source = "return 2 + 2"
	for i := 0; i < b.N; i++ {
		if err := state.Load(strings.NewReader(source), source, "t"); err != nil {
			b.Fatal(err)
		}
		if err := state.Call(ctx, 0, 1); err != nil {
			b.Fatal(err)
		}
		state.Pop(1)
	}
}

func pushValue(l *State, x any) {
	switch x := x.(type) {
	case nil:
		l.PushNil()
	case bool:
		l.PushBoolean(x)
	case string:
		l.PushString(x)
	case int64:
		l.PushInteger(x)
	case float64:
		l.PushNumber(x)
	default:
		panic(fmt.Errorf("unsupported value type %T", x))
	}
}

func valueToGo(l *State, idx int) (any, error) {
	switch tp := l.Type(idx); tp {
	case TypeNil:
		return nil, nil
	case TypeBoolean:
		return l.ToBoolean(idx), nil
	case TypeString:
		x, _ := l.ToString(idx)
		return x, nil
	case TypeNumber:
		if x, ok := l.ToInteger(idx); ok {
			return x, nil
		}
		x, _ := l.ToNumber(idx)
		return x, nil
	default:
		return nil, fmt.Errorf("value is a %v", tp)
	}
}

func describeValue(l *State, idx int) string {
	switch l.Type(idx) {
	case TypeNone:
		return "<none>"
	case TypeNil:
		return "nil"
	case TypeBoolean:
		return strconv.FormatBool(l.ToBoolean(idx))
	case TypeString:
		s, _ := l.ToString(idx)
		return lualex.Quote(s)
	case TypeNumber:
		if l.IsInteger(idx) {
			i, _ := l.ToInteger(idx)
			return strconv.FormatInt(i, 10)
		}
		f, _ := l.ToNumber(idx)
		return strconv.FormatFloat(f, 'g', 0, 64)
	case TypeTable:
		if l.RawLen(idx) == 0 {
			return "{}"
		}
		return "{...}"
	case TypeFunction:
		return "<function>"
	case TypeLightUserdata, TypeUserdata:
		return "<userdata>"
	case TypeThread:
		return "<thread>"
	default:
		return "<unknown>"
	}
}

// freezeSpy is a test double for the [Freezer] interface.
type freezeSpy struct {
	callCount int
	err       error
}

func (spy *freezeSpy) Freeze() error {
	spy.callCount++
	return spy.err
}
