// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zb.256lights.llc/pkg/internal/lualex"
)

func TestUTF8Char(t *testing.T) {
	tests := []struct {
		args []int64
		want string
	}{
		{
			args: []int64{},
			want: "",
		},
		{
			args: []int64{'a'},
			want: "a",
		},
		{
			args: []int64{'h', 'e', 'l', 'l', 'o', ' ', 'W', 'o', 'r', 'l', 'd'},
			want: "hello World",
		},
		{
			args: []int64{0x6c49},
			want: "\u6c49",
		},
		{
			args: []int64{0x7fffffff},
			want: "\xfd\xbf\xbf\xbf\xbf\xbf",
		},
	}

	ctx := context.Background()
	for _, test := range tests {
		func() {
			state := new(State)
			defer func() {
				if err := state.Close(); err != nil {
					t.Error("Close:", err)
				}
			}()

			state.PushClosure(0, OpenUTF8)
			if err := state.Call(ctx, 0, 1); err != nil {
				t.Error(err)
				return
			}
			if _, err := state.Field(ctx, -1, "char"); err != nil {
				t.Error(err)
				return
			}

			testName := "utf8.char("
			for i, arg := range test.args {
				state.PushInteger(arg)
				if i == 0 {
					testName = fmt.Sprintf("%s%d", testName, arg)
				} else {
					testName = fmt.Sprintf("%s, %d", testName, arg)
				}
			}
			testName += ")"

			if err := state.Call(ctx, len(test.args), 1); err != nil {
				t.Errorf("%s: %v", testName, err)
				return
			}

			if got, want := state.Type(-1), TypeString; got != want {
				t.Errorf("type(%s) = %v; want %v", testName, got, want)
			} else if got, ok := state.ToString(-1); got != test.want || !ok {
				t.Errorf("%s = %q; want %q", testName, got, test.want)
			}
		}()
	}
}

func TestUTF8Codepoint(t *testing.T) {
	tests := []struct {
		s   string
		i   int64
		j   int64
		lax bool

		want      []int64
		wantError string
	}{
		{
			s:         "",
			i:         1,
			j:         1,
			wantError: "out of bounds",
		},
		{
			s:    "\x00",
			i:    1,
			j:    1,
			want: []int64{0},
		},
		{
			s:    "a",
			i:    1,
			j:    1,
			want: []int64{'a'},
		},
		{
			s:    "hello World",
			i:    1,
			j:    -1,
			want: []int64{'h', 'e', 'l', 'l', 'o', ' ', 'W', 'o', 'r', 'l', 'd'},
		},
		{
			s:    "\u6c49\u5b57/\u6f22\u5b57",
			i:    1,
			j:    -1,
			want: []int64{0x6c49, 0x5b57, '/', 0x6f22, 0x5b57},
		},
		{
			s:    "\u6c49\u5b57/\u6f22\u5b57",
			i:    1,
			j:    1,
			want: []int64{0x6c49},
		},
		{
			s:         "áéí\x80",
			i:         -8,
			j:         1,
			wantError: "out of bounds",
		},
		{
			s:    "\xed\xa0\x80",
			i:    1,
			j:    1,
			lax:  true,
			want: []int64{0xd800},
		},
		{
			s:    "\xed\xbf\xbf",
			i:    1,
			j:    1,
			lax:  true,
			want: []int64{0xdfff},
		},
		{
			s:    "\xfd\xbf\xbf\xbf\xbf\xbf",
			i:    1,
			j:    1,
			lax:  true,
			want: []int64{0x7fffffff},
		},
	}

	ctx := context.Background()
	for _, test := range tests {
		func() {
			state := new(State)
			defer func() {
				if err := state.Close(); err != nil {
					t.Error("Close:", err)
				}
			}()

			state.PushClosure(0, OpenUTF8)
			if err := state.Call(ctx, 0, 1); err != nil {
				t.Error(err)
				return
			}
			if _, err := state.Field(ctx, -1, "codepoint"); err != nil {
				t.Error(err)
				return
			}

			testName := fmt.Sprintf("utf8.codepoint(%s", lualex.Quote(test.s))
			funcIndex := state.Top()
			state.PushString(test.s)
			if test.i != 1 || test.i != test.j || test.lax {
				state.PushInteger(test.i)
				testName = fmt.Sprintf("%s, %d", testName, test.i)
				if test.i != test.j || test.lax {
					state.PushInteger(test.j)
					testName = fmt.Sprintf("%s, %d", testName, test.j)
					if test.lax {
						state.PushBoolean(true)
						testName += ", true"
					}
				}
			}
			testName += ")"

			if err := state.Call(ctx, state.Top()-funcIndex, MultipleReturns); err != nil {
				if test.wantError == "" {
					t.Errorf("%s: %v", testName, err)
				} else if got := err.Error(); !strings.Contains(got, test.wantError) {
					t.Errorf("%s raised: %s; want message to contain %q", testName, got, test.wantError)
				}
				return
			}
			if test.wantError != "" {
				t.Errorf("%s did not raise an error (expected %q)", testName, test.wantError)
				return
			}

			var got []int64
			for i, n := funcIndex, state.Top(); i <= n; i++ {
				if got, want := state.Type(i), TypeNumber; got != want || !state.IsInteger(i) {
					t.Errorf("type(select(%d, %s)) = %v; want integer", i-funcIndex+1, testName, got)
				}
				n, _ := state.ToInteger(i)
				got = append(got, n)
			}
			if diff := cmp.Diff(test.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("%s (-want +got):\n%s", testName, diff)
			}
		}()
	}
}

func TestUTF8Len(t *testing.T) {
	tests := []struct {
		s   string
		i   int64
		j   int64
		lax bool

		want      int64
		fail      bool
		wantError string
	}{
		{s: "", i: 1, j: -1, want: 0},
		{s: "abc", i: 0, j: 2, wantError: "out of bounds"},
		{s: "abc", i: 1, j: 4, wantError: "out of bounds"},
		{s: "hello World", i: 1, j: -1, want: 11},
		{s: "hello World", i: 12, j: -1, want: 0},
		{s: "\u6c49\u5b57/\u6f22\u5b57", i: 1, j: 1, want: 1},
	}

	ctx := context.Background()
	for _, test := range tests {
		func() {
			state := new(State)
			defer func() {
				if err := state.Close(); err != nil {
					t.Error("Close:", err)
				}
			}()

			state.PushClosure(0, OpenUTF8)
			if err := state.Call(ctx, 0, 1); err != nil {
				t.Error(err)
				return
			}
			if _, err := state.Field(ctx, -1, "len"); err != nil {
				t.Error(err)
				return
			}

			testName := fmt.Sprintf("utf8.len(%s", lualex.Quote(test.s))
			funcIndex := state.Top()
			state.PushString(test.s)
			if test.i != 1 || test.j != -1 || test.lax {
				state.PushInteger(test.i)
				testName = fmt.Sprintf("%s, %d", testName, test.i)
				if test.j != -1 || test.lax {
					state.PushInteger(test.j)
					testName = fmt.Sprintf("%s, %d", testName, test.j)
					if test.lax {
						state.PushBoolean(true)
						testName += ", true"
					}
				}
			}
			testName += ")"

			if err := state.Call(ctx, state.Top()-funcIndex, 2); err != nil {
				if test.wantError == "" {
					t.Errorf("%s: %v", testName, err)
				} else if got := err.Error(); !strings.Contains(got, test.wantError) {
					t.Errorf("%s raised: %s; want message to contain %q", testName, got, test.wantError)
				}
				return
			}
			if test.wantError != "" {
				t.Errorf("%s did not raise an error (expected %q)", testName, test.wantError)
				return
			}

			switch tp := state.Type(-2); tp {
			case TypeNumber:
				if test.fail {
					n, _ := state.ToNumber(-2)
					t.Errorf("%s = %g; want nil", testName, n)
				} else if got, ok := state.ToInteger(-2); !ok {
					n, _ := state.ToNumber(-2)
					t.Errorf("%s = %g; want %d", testName, n, test.want)
				} else if got != test.want {
					t.Errorf("%s = %d; want %d", testName, got, test.want)
				}
				if got, want := state.Type(-1), TypeNil; got != want {
					t.Errorf("type(select(2, %s)) = %v; want %v", testName, got, want)
				}
			case TypeNil:
				if !test.fail {
					t.Errorf("%s = nil; want %d", testName, test.want)
				} else if got, want := state.Type(-1), TypeNumber; got != want {
					t.Errorf("type(select(2, %s)) = %v; want %v", testName, got, want)
				} else if got, ok := state.ToInteger(-1); !ok {
					n, _ := state.ToNumber(-1)
					t.Errorf("%s = nil, %g; want nil, %d", testName, n, test.want)
				} else if got != test.want {
					t.Errorf("%s = nil, %d; want nil, %d", testName, got, test.want)
				}
			default:
				want := TypeNumber.String()
				if test.fail {
					want = TypeNil.String()
				}
				t.Errorf("type(%s) = %v; want %s", testName, tp, want)
			}
		}()
	}
}

func TestUTF8Offset(t *testing.T) {
	tests := []struct {
		s string
		n int64
		i int64

		want      int64
		fail      bool
		wantError string
	}{
		{s: "", n: 1, i: 1, want: 1},
		{s: "alo", n: 5, i: 1, fail: true},
		{s: "alo", n: -4, i: 4, fail: true},
		{s: "abc", n: 1, i: 5, wantError: "position out of bounds"},
		{s: "abc", n: 1, i: -4, wantError: "position out of bounds"},
		{s: "", n: 1, i: 2, wantError: "position out of bounds"},
		{s: "", n: 1, i: -1, wantError: "position out of bounds"},
		{s: "𦧺", n: 1, i: 2, wantError: "continuation byte"},
		{s: "𦧺", n: 1, i: 2, wantError: "continuation byte"},
		{s: "\x80", n: 1, i: 1, wantError: "continuation byte"},
		{s: "hello World", n: 0, i: 1, want: 1},
		{s: "hello World", n: 11, i: 1, want: 11},
		{s: "hello World", n: 2, i: 11, want: 12},
		{s: "\u6c49\u5b57/\u6f22\u5b57", n: 1, i: 1, want: 1},
		{s: "\u6c49\u5b57/\u6f22\u5b57", n: 2, i: 1, want: 4},
		{s: "\u6c49\u5b57/\u6f22\u5b57", n: 2, i: 4, want: 7},
		{s: "\u6c49\u5b57/\u6f22\u5b57", n: 2, i: 7, want: 8},
	}

	ctx := context.Background()
	for _, test := range tests {
		func() {
			state := new(State)
			defer func() {
				if err := state.Close(); err != nil {
					t.Error("Close:", err)
				}
			}()

			state.PushClosure(0, OpenUTF8)
			if err := state.Call(ctx, 0, 1); err != nil {
				t.Error(err)
				return
			}
			if _, err := state.Field(ctx, -1, "offset"); err != nil {
				t.Error(err)
				return
			}

			testName := fmt.Sprintf("utf8.offset(%s, %d", lualex.Quote(test.s), test.n)
			funcIndex := state.Top()
			state.PushString(test.s)
			state.PushInteger(test.n)
			if !(test.n >= 0 && test.i == 1) && !(test.n < 0 && test.i == int64(len(test.s))+1) {
				state.PushInteger(test.i)
				testName = fmt.Sprintf("%s, %d", testName, test.i)
			}
			testName += ")"

			if err := state.Call(ctx, state.Top()-funcIndex, 1); err != nil {
				if test.wantError == "" {
					t.Errorf("%s: %v", testName, err)
				} else if got := err.Error(); !strings.Contains(got, test.wantError) {
					t.Errorf("%s raised: %s; want message to contain %q", testName, got, test.wantError)
				}
				return
			}
			if test.wantError != "" {
				t.Errorf("%s did not raise an error (expected %q)", testName, test.wantError)
				return
			}

			switch tp := state.Type(-1); tp {
			case TypeNumber:
				if test.fail {
					n, _ := state.ToNumber(-1)
					t.Errorf("%s = %g; want nil", testName, n)
				} else if got, ok := state.ToInteger(-1); !ok {
					n, _ := state.ToNumber(-1)
					t.Errorf("%s = %g; want %d", testName, n, test.want)
				} else if got != test.want {
					t.Errorf("%s = %d; want %d", testName, got, test.want)
				}
			case TypeNil:
				if !test.fail {
					t.Errorf("%s = nil; want %d", testName, test.want)
				}
			default:
				want := TypeNumber.String()
				if test.fail {
					want = TypeNil.String()
				}
				t.Errorf("type(%s) = %v; want %s", testName, tp, want)
			}
		}()
	}
}
