// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"bufio"
	"bytes"
	"errors"
	"io"
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
	if err := state.SetGlobal("x", 0); err != nil {
		t.Error(err)
	}
	if tp, err := state.Global("x", 0); err != nil {
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
	if tp, err := state.Global("x", 0); err != nil {
		t.Error(err)
	} else if tp != TypeNil {
		t.Errorf("type(_G.x) = %v; want %v", tp, TypeNil)
	}
	state.Pop(1)
}

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

	t.Run("Binary", func(t *testing.T) {
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
				state := new(State)
				defer func() {
					if err := state.Close(); err != nil {
						t.Error("Close:", err)
					}
				}()

				if err := state.Load(bytes.NewReader(test.data), source, "bt"); err != nil {
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
					got, err := state.Compare(-2, -1, op, 0)
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

					if err := state.Call(2, 1, 0); err != nil {
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
			state := new(State)
			defer func() {
				if err := state.Close(); err != nil {
					t.Error("Close:", err)
				}
			}()
			test.push(state)

			n := state.Top()
			if err := state.Concat(n, 0); err != nil {
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
	if !state.SetUserValue(-2, 1) {
		t.Error("Userdata does not have value 1")
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

func BenchmarkExec(b *testing.B) {
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
		if err := state.Call(0, 1, 0); err != nil {
			b.Fatal(err)
		}
		state.Pop(1)
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
