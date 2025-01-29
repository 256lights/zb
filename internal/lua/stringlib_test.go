// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zb.256lights.llc/pkg/internal/lualex"
	"zb.256lights.llc/pkg/sets"
)

func TestStringFind(t *testing.T) {
	t.Skip("TODO(soon)")

	tests := []struct {
		s       string
		pattern string
		init    int64
		plain   bool

		want []any
	}{
		{
			s:       "",
			pattern: "",
			init:    1,
			want:    []any{int64(1), int64(0)},
		},
		{
			s:       "alo",
			pattern: "",
			init:    1,
			want:    []any{int64(1), int64(0)},
		},
		{
			s:       "a\x00o a\x00o a\x00o",
			pattern: "a",
			init:    1,
			want:    []any{int64(1), int64(1)},
		},
		{
			s:       "a\x00o a\x00o a\x00o",
			pattern: "a\x00o",
			init:    2,
			want:    []any{int64(5), int64(7)},
		},
		{
			s:       "a\x00o a\x00o a\x00o",
			pattern: "a\x00o",
			init:    9,
			want:    []any{int64(9), int64(11)},
		},
		{
			s:       "a\x00a\x00a\x00a\x00\x00ab",
			pattern: "\x00ab",
			init:    2,
			want:    []any{int64(9), int64(11)},
		},
		{
			s:       "a\x00a\x00a\x00a\x00\x00ab",
			pattern: "b",
			init:    1,
			want:    []any{int64(11), int64(11)},
		},
		{
			s:       "a\x00a\x00a\x00a\x00\x00ab",
			pattern: "b\x00",
			init:    1,
			want:    []any{nil},
		},
		{
			s:       "",
			pattern: "\x00",
			init:    1,
			want:    []any{nil},
		},
		{
			s:       "alo123alo",
			pattern: "12",
			init:    1,
			want:    []any{int64(4), int64(5)},
		},
		{
			s:       "alo123alo",
			pattern: "^12",
			init:    1,
			want:    []any{nil},
		},
		{
			s:       "aloALO",
			pattern: "%l*",
			init:    1,
			want:    []any{int64(1), int64(3)},
		},
		{
			s:       "aLo_ALO",
			pattern: "%a*",
			init:    1,
			want:    []any{int64(1), int64(3)},
		},
		{
			s:       "  \n\r*&\n\r   xuxu  \n\n",
			pattern: "%g%g%g+",
			init:    1,
			want:    []any{int64(12), int64(15)},
		},
		{
			s:       "aaab",
			pattern: "a*",
			init:    1,
			want:    []any{int64(1), int64(3)},
		},
		{
			s:       "aaa",
			pattern: "^.*$",
			init:    1,
			want:    []any{int64(1), int64(3)},
		},
		{
			s:       "aaa",
			pattern: "b*",
			init:    1,
			want:    []any{int64(1), int64(0)},
		},
		{
			s:       "aaa",
			pattern: "ab*a",
			init:    1,
			want:    []any{int64(1), int64(2)},
		},
		{
			s:       "aba",
			pattern: "ab*a",
			init:    1,
			want:    []any{int64(1), int64(3)},
		},
		{
			s:       "aaab",
			pattern: "a+",
			init:    1,
			want:    []any{int64(1), int64(3)},
		},
		{
			s:       "aaa",
			pattern: "^.+$",
			init:    1,
			want:    []any{int64(1), int64(3)},
		},
		{
			s:       "aaa",
			pattern: "b+",
			init:    1,
			want:    []any{nil},
		},
		{
			s:       "aaa",
			pattern: "ab+a",
			init:    1,
			want:    []any{nil},
		},
		{
			s:       "aba",
			pattern: "ab+a",
			init:    1,
			want:    []any{int64(1), int64(3)},
		},
		{
			s:       "a$a",
			pattern: ".$",
			init:    1,
			want:    []any{int64(3), int64(3)},
		},
		{
			s:       "a$a",
			pattern: ".%$",
			init:    1,
			want:    []any{int64(1), int64(2)},
		},
		{
			s:       "a$a",
			pattern: ".$.",
			init:    1,
			want:    []any{int64(1), int64(3)},
		},
		{
			s:       "a$a",
			pattern: "$$",
			init:    1,
			want:    []any{nil},
		},
		{
			s:       "a$b",
			pattern: "a$",
			init:    1,
			want:    []any{nil},
		},
		{
			s:       "a$a",
			pattern: "$",
			init:    1,
			want:    []any{int64(4), int64(3)},
		},
		{
			s:       "",
			pattern: "b*",
			init:    1,
			want:    []any{int64(1), int64(0)},
		},
		{
			s:       "aaa",
			pattern: "bb*",
			init:    1,
			want:    []any{nil},
		},
		{
			s:       "aaab",
			pattern: "a-",
			init:    1,
			want:    []any{int64(1), int64(0)},
		},
		{
			s:       "aaa",
			pattern: "^.-$",
			init:    1,
			want:    []any{int64(1), int64(3)},
		},
		{
			s:       "aabaaabaaabaaaba",
			pattern: "b.*b",
			init:    1,
			want:    []any{int64(3), int64(15)},
		},
		{
			s:       "aabaaabaaabaaaba",
			pattern: "b.-b",
			init:    1,
			want:    []any{int64(3), int64(7)},
		},
		{
			s:       "alo xo",
			pattern: ".o$",
			init:    1,
			want:    []any{int64(5), int64(6)},
		},
		{
			s:       " \n isto é assim",
			pattern: "%S%S*",
			init:    1,
			want:    []any{int64(4), int64(7)},
		},
		{
			s:       " \n isto é assim",
			pattern: "%S*$",
			init:    1,
			want:    []any{int64(12), int64(16)},
		},
		{
			s:       " \n isto é assim",
			pattern: "[a-z]*$",
			init:    1,
			want:    []any{int64(12), int64(16)},
		},
		{
			s:       "um caracter ? extra",
			pattern: "[^%sa-z]",
			init:    1,
			want:    []any{int64(13), int64(13)},
		},
		{
			s:       "",
			pattern: "a?",
			init:    1,
			want:    []any{int64(1), int64(0)},
		},
		{
			s:       "á",
			pattern: "\xc3?\xa1?",
			init:    1,
			want:    []any{int64(1), int64(2)},
		},
		{
			s:       "ábl",
			pattern: "\xc3?\xa1?b?l?",
			init:    1,
			want:    []any{int64(1), int64(4)},
		},
		{
			s:       "  ábl",
			pattern: "\xc3?\xa1?b?l?",
			init:    1,
			want:    []any{int64(1), int64(0)},
		},
		{
			s:       "aa",
			pattern: "^aa?a?a",
			init:    1,
			want:    []any{int64(1), int64(2)},
		},
		{
			s:       "]]]áb",
			pattern: "[^]]+",
			init:    1,
			want:    []any{int64(1), int64(3)},
		},
		{
			s:       "0alo alo",
			pattern: "%x*",
			init:    1,
			want:    []any{int64(1), int64(2)},
		},
		{
			s:       "alo alo",
			pattern: "%C+",
			init:    1,
			want:    []any{int64(1), int64(7)},
		},
		{
			s:       "(álo)",
			pattern: "%(á",
			init:    1,
			want:    []any{int64(1), int64(3)},
		},
		{
			s:       "a",
			pattern: "%f[a]",
			init:    1,
			want:    []any{int64(1), int64(1)},
		},
		{
			s:       "a",
			pattern: "%f[^%z]",
			init:    1,
			want:    []any{int64(1), int64(1)},
		},
		{
			s:       "a",
			pattern: "%f[^%l]",
			init:    1,
			want:    []any{int64(2), int64(1)},
		},
		{
			s:       "aba",
			pattern: "%f[a%z]",
			init:    1,
			want:    []any{int64(3), int64(3)},
		},
		{
			s:       "aba",
			pattern: "%f[%z]",
			init:    1,
			want:    []any{int64(4), int64(3)},
		},
		{
			s:       "aba",
			pattern: "%f[%l%z]",
			init:    1,
			want:    []any{nil},
		},
		{
			s:       "aba",
			pattern: "%f[^%l%z]",
			init:    1,
			want:    []any{nil},
		},
		{
			s:       " alo aalo allo",
			pattern: "%f[%S].-%f[%s].-%f[%S]",
			init:    1,
			want:    []any{int64(2), int64(5)},
		},
		{
			s:       "b$a",
			pattern: "$\x00?",
			init:    1,
			want:    []any{int64(2), int64(2)},
		},
		{
			s:       "abc\x00efg",
			pattern: "%\x00",
			init:    1,
			want:    []any{int64(4), int64(4)},
		},
		{
			s:       "abc\x00\x00",
			pattern: "\x00.",
			init:    1,
			want:    []any{int64(4), int64(5)},
		},
		{
			s:       "abcx\x00\x00abc\x00abc",
			pattern: "x\x00\x00abc\x00a.",
			init:    1,
			want:    []any{int64(4), int64(12)},
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

			state.PushClosure(0, OpenString)
			if err := state.Call(ctx, 0, 1); err != nil {
				t.Error(err)
				return
			}
			if _, err := state.Field(ctx, -1, "find"); err != nil {
				t.Error(err)
				return
			}

			testName := fmt.Sprintf("string.find(%s, %s", lualex.Quote(test.s), lualex.Quote(test.pattern))
			top := state.Top()
			state.PushString(test.s)
			state.PushString(test.pattern)
			if test.init != 1 || test.plain {
				state.PushInteger(test.init)
				testName = fmt.Sprintf("%s, %d", testName, test.init)
				if test.plain {
					state.PushBoolean(test.plain)
					testName += ", true"
				}
			}
			testName += ")"

			if err := state.Call(ctx, state.Top()-top, MultipleReturns); err != nil {
				t.Errorf("%s: %v", testName, err)
				return
			}

			var got []any
			for i, n := top, state.Top(); i <= n; i++ {
				switch tp := state.Type(i); tp {
				case TypeNil:
					got = append(got, nil)
				case TypeNumber:
					if x, ok := state.ToInteger(i); ok {
						got = append(got, x)
					} else {
						t.Errorf("%s return %d is not an integer", testName, i-top)
					}
				case TypeString:
					x, _ := state.ToString(i)
					got = append(got, x)
				default:
					t.Errorf("%s return %d is a %v", testName, i-top, tp)
					got = append(got, nil)
				}
			}

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("%s (-want +got):\n%s", testName, diff)
			}
		}()
	}
}

func TestStringMatch(t *testing.T) {
	t.Skip("TODO(soon)")

	tests := []struct {
		s       string
		pattern string
		init    int64

		want []any
	}{
		{
			s:       "aaab",
			pattern: ".*b",
			init:    1,
			want:    []any{"aaab"},
		},
		{
			s:       "aaa",
			pattern: ".*a",
			init:    1,
			want:    []any{"aaa"},
		},
		{
			s:       "b",
			pattern: ".*b",
			init:    1,
			want:    []any{"b"},
		},
		{
			s:       "aaab",
			pattern: ".+b",
			init:    1,
			want:    []any{"aaab"},
		},
		{
			s:       "aaa",
			pattern: ".+a",
			init:    1,
			want:    []any{"aaa"},
		},
		{
			s:       "b",
			pattern: ".+b",
			init:    1,
			want:    []any{nil},
		},
		{
			s:       "aaab",
			pattern: ".?b",
			init:    1,
			want:    []any{"ab"},
		},
		{
			s:       "aaa",
			pattern: ".?a",
			init:    1,
			want:    []any{"aa"},
		},
		{
			s:       "b",
			pattern: ".?b",
			init:    1,
			want:    []any{"b"},
		},
		{
			s:       "alo xyzK",
			pattern: "(%w+)K",
			init:    1,
			want:    []any{"xyz"},
		},
		{
			s:       "254 K",
			pattern: "(%d*)K",
			init:    1,
			want:    []any{""},
		},
		{
			s:       "alo ",
			pattern: "(%w*)$",
			init:    1,
			want:    []any{""},
		},
		{
			s:       "alo ",
			pattern: "(%w+)$",
			init:    1,
			want:    []any{nil},
		},
		{
			s:       "âlo alo",
			pattern: "^((([\x00-\x7F\xC2-\xFD][\x80-\xBF]*)[\x00-\x7F\xC2-\xFD][\x80-\xBF]*)[\x00-\x7F\xC2-\xFD][\x80-\xBF]* (%w*))$",
			init:    1,
			want: []any{
				"âlo alo",
				"âl",
				"â",
				"alo",
			},
		},
		{
			s:       "0123456789",
			pattern: "(.+(.?)())",
			init:    1,
			want: []any{
				"0123456789",
				"",
				int64(11),
			},
		},
		{
			s:       " alo aalo allo",
			pattern: "%f[%S](.-%f[%s].-%f[%S])",
			init:    1,
			want:    []any{"alo "},
		},
		{
			s:       "ab\x00\x01\x02c",
			pattern: "[\x00-\x02]+",
			init:    1,
			want:    []any{"\x00\x01\x02"},
		},
		{
			s:       "ab\x00\x01\x02c",
			pattern: "[\x00-\x00]+",
			init:    1,
			want:    []any{"\x00"},
		},
		{
			s:       "abc\x00efg\x00\x01e\x01g",
			pattern: "%b\x00\x01",
			init:    1,
			want:    []any{"\x00efg\x00\x01e\x01"},
		},
		{
			s:       "abc\x00\x00\x00",
			pattern: "%\x00+",
			init:    1,
			want:    []any{"\x00\x00\x00"},
		},
		{
			s:       "abc\x00\x00\x00",
			pattern: "%\x00%\x00?",
			init:    1,
			want:    []any{"\x00\x00"},
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

			state.PushClosure(0, OpenString)
			if err := state.Call(ctx, 0, 1); err != nil {
				t.Error(err)
				return
			}
			if _, err := state.Field(ctx, -1, "match"); err != nil {
				t.Error(err)
				return
			}

			testName := fmt.Sprintf("string.match(%s, %s", lualex.Quote(test.s), lualex.Quote(test.pattern))
			top := state.Top()
			state.PushString(test.s)
			state.PushString(test.pattern)
			if test.init != 1 {
				state.PushInteger(test.init)
				testName = fmt.Sprintf("%s, %d", testName, test.init)
			}
			testName += ")"

			if err := state.Call(ctx, state.Top()-top, MultipleReturns); err != nil {
				t.Errorf("%s: %v", testName, err)
				return
			}

			var got []any
			for i, n := top, state.Top(); i <= n; i++ {
				switch tp := state.Type(i); tp {
				case TypeNil:
					got = append(got, nil)
				case TypeNumber:
					if x, ok := state.ToInteger(i); ok {
						got = append(got, x)
					} else {
						t.Errorf("%s return %d is not an integer", testName, i-top)
					}
				case TypeString:
					x, _ := state.ToString(i)
					got = append(got, x)
				default:
					t.Errorf("%s return %d is a %v", testName, i-top, tp)
					got = append(got, nil)
				}
			}

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("%s (-want +got):\n%s", testName, diff)
			}
		}()
	}
}

func TestStringGSub(t *testing.T) {
	t.Skip("TODO(soon)")

	tests := []struct {
		s               string
		sContext        sets.Set[string]
		pattern         string
		patternContext  sets.Set[string]
		pushReplacement func(l *State)
		n               int64
		zeroN           bool

		want             string
		wantContext      sets.Set[string]
		wantReplacements int64
	}{
		{
			s:       "hello world",
			pattern: "(%w+)",
			pushReplacement: func(l *State) {
				l.PushString("%1 %1")
			},
			want:             "hello hello world world",
			wantReplacements: 2,
		},
		{
			s:       "hello world",
			pattern: "%w+",
			pushReplacement: func(l *State) {
				l.PushString("%0 %0")
				l.PushInteger(1)
			},
			n:                1,
			want:             "hello hello world",
			wantReplacements: 1,
		},
		{
			s:       "hello world from Lua",
			pattern: "(%w+)%s*(%w+)",
			pushReplacement: func(l *State) {
				l.PushString("%2 %1")
			},
			want:             "world hello Lua from",
			wantReplacements: 2,
		},
		{
			s:       "home = $HOME, user = $USER",
			pattern: "%$(%w+)",
			pushReplacement: func(l *State) {
				l.PushClosure(0, func(ctx context.Context, l *State) (int, error) {
					switch name, _ := l.ToString(1); name {
					case "HOME":
						l.PushString("/home/roberto")
					case "USER":
						l.PushString("roberto")
					default:
						l.PushNil()
					}
					return 1, nil
				})
			},
			want:             "home = /home/roberto, user = roberto",
			wantReplacements: 2,
		},
		{
			s:       "$name-$version.tar.gz",
			pattern: "%$(%w+)",
			pushReplacement: func(l *State) {
				l.CreateTable(0, 2)
				l.PushString("lua")
				l.RawSetField(-2, "name")
				l.PushString("5.4")
				l.RawSetField(-2, "version")
			},
			want:             "lua-5.4.tar.gz",
			wantReplacements: 2,
		},
		{
			s:       "ülo ülo",
			pattern: "ü",
			pushReplacement: func(l *State) {
				l.PushString("x")
			},
			want:             "xlo xlo",
			wantReplacements: 2,
		},
		{
			s:       "alo úlo  ",
			pattern: " +$",
			pushReplacement: func(l *State) {
				l.PushString("")
			},
			want:             "alo úlo",
			wantReplacements: 1,
		},
		{
			s:       "  alo alo  ",
			pattern: "^%s*(.-)%s*$",
			pushReplacement: func(l *State) {
				l.PushString("%1")
			},
			want:             "alo alo",
			wantReplacements: 1,
		},
		{
			s:       "alo  alo  \n 123\n ",
			pattern: "%s+",
			pushReplacement: func(l *State) {
				l.PushString(" ")
			},
			want:             "alo alo 123 ",
			wantReplacements: 3,
		},
		{
			s:       "abç d",
			pattern: "([\x00-\x7F\xC2-\xFD][\x80-\xBF]*)",
			pushReplacement: func(l *State) {
				l.PushString("%1@")
			},
			want:             "a@b@ç@ @d@",
			wantReplacements: 5,
		},
		{
			s:       "abçd",
			pattern: "([\x00-\x7F\xC2-\xFD][\x80-\xBF]*)",
			pushReplacement: func(l *State) {
				l.PushString("%0@")
			},
			n:                2,
			want:             "a@b@çd",
			wantReplacements: 2,
		},
		{
			s:       "alo alo",
			pattern: "()[al]",
			pushReplacement: func(l *State) {
				l.PushString("%1")
			},
			want:             "12o 56o",
			wantReplacements: 4,
		},
		{
			s:       "abc=xyz",
			pattern: "(%w*)(%p)(%w+)",
			pushReplacement: func(l *State) {
				l.PushString("%3%2%1-%0")
			},
			want:             "xyz=abc-abc=xyz",
			wantReplacements: 1,
		},
		{
			s:       "abc",
			pattern: "%w",
			pushReplacement: func(l *State) {
				l.PushString("%1%0")
			},
			want:             "aabbcc",
			wantReplacements: 3,
		},
		{
			s:       "abc",
			pattern: "%w+",
			pushReplacement: func(l *State) {
				l.PushString("%0%1")
			},
			want:             "abcabc",
			wantReplacements: 3,
		},
		{
			s:       "áéí",
			pattern: "$",
			pushReplacement: func(l *State) {
				l.PushString("\x00óú")
			},
			want:             "áéí\x00óú",
			wantReplacements: 1,
		},
		{
			s:       "",
			pattern: "^",
			pushReplacement: func(l *State) {
				l.PushString("r")
			},
			want:             "r",
			wantReplacements: 1,
		},
		{
			s:       "",
			pattern: "$",
			pushReplacement: func(l *State) {
				l.PushString("r")
			},
			want:             "r",
			wantReplacements: 1,
		},
		{
			s:       "a b cd",
			pattern: " *",
			pushReplacement: func(l *State) {
				l.PushString("-")
			},
			want:             "-a-b-c-d-",
			wantReplacements: 5,
		},
		{
			s:       "um (dois) tres (quatro)",
			pattern: "(%(%w+%))",
			pushReplacement: func(l *State) {
				l.PushClosure(0, stringUpper)
			},
			want:             "um (DOIS) tres (QUATRO)",
			wantReplacements: 5,
		},
		{
			s:       "aaa aa a aaa a",
			pattern: "%f[%w]a",
			pushReplacement: func(l *State) {
				l.PushString("x")
			},
			want:             "xaa xa x xaa x",
			wantReplacements: 5,
		},
		{
			s:       "[[]] [][] [[[[",
			pattern: "%f[[].",
			pushReplacement: func(l *State) {
				l.PushString("x")
			},
			want:             "x[]] x]x] x[[[",
			wantReplacements: 4,
		},
		{
			s:       "01abc45de3",
			pattern: "%f[%d]",
			pushReplacement: func(l *State) {
				l.PushString(".")
			},
			want:             ".01abc.45de.3",
			wantReplacements: 3,
		},
		{
			s:       "01abc45 de3x",
			pattern: "%f[%D]%w",
			pushReplacement: func(l *State) {
				l.PushString(".")
			},
			want:             "01.bc45 de3.",
			wantReplacements: 2,
		},
		{
			s:       "function",
			pattern: "%f[\x01-\xff]%w",
			pushReplacement: func(l *State) {
				l.PushString(".")
			},
			want:             ".unction",
			wantReplacements: 1,
		},
		{
			s:       "function",
			pattern: "%f[^\x01-\xff]",
			pushReplacement: func(l *State) {
				l.PushString(".")
			},
			want:             "function.",
			wantReplacements: 1,
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

			state.PushClosure(0, OpenString)
			if err := state.Call(ctx, 0, 1); err != nil {
				t.Error(err)
				return
			}
			if _, err := state.Field(ctx, -1, "gsub"); err != nil {
				t.Error(err)
				return
			}
			funcIndex := state.Top()

			testName := fmt.Sprintf("string.gsub(%s, %s, ", lualex.Quote(test.s), lualex.Quote(test.pattern))
			state.PushStringContext(test.s, test.sContext)
			state.PushStringContext(test.pattern, test.patternContext)
			test.pushReplacement(state)
			if tp := state.Type(-1); tp == TypeString {
				repl, _ := state.ToString(-1)
				testName += lualex.Quote(repl)
			} else {
				testName = fmt.Sprintf("%s(%v)", testName, tp)
			}
			if test.n != 0 || test.zeroN {
				state.PushInteger(test.n)
				testName += fmt.Sprintf("%s, %d", testName, test.n)
			}
			testName += ")"

			if err := state.Call(ctx, state.Top()-funcIndex, 2); err != nil {
				t.Errorf("%s: %v", testName, err)
				return
			}

			if got, want := state.Type(-2), TypeString; got != want {
				t.Errorf("type(%s) = %v; want %v", testName, got, want)
			} else {
				if got, _ := state.ToString(-2); got != test.want {
					t.Errorf("%s = %s; want %s", testName, lualex.Quote(got), lualex.Quote(test.want))
				}
				if diff := cmp.Diff(test.wantContext, state.StringContext(-2), cmpopts.EquateEmpty()); diff != "" {
					t.Errorf("%s string context (-want +got):\n%s", testName, diff)
				}
			}

			if got, want := state.Type(-1), TypeNumber; got != want {
				t.Errorf("type(select(2, %s)) = %v; want %v", testName, got, want)
			} else if got, _ := state.ToInteger(-1); got != test.wantReplacements {
				t.Errorf("(select(2, %s)) = %d; want %d", testName, got, test.wantReplacements)
			}
		}()
	}
}

func TestCutFormatSpecifier(t *testing.T) {
	tests := []struct {
		s    string
		spec string
		err  bool
	}{
		{s: "", spec: ""},
		{s: "abc", spec: "a"},
		{s: "%s", spec: "%s"},
		{s: "%sabc", spec: "%s"},
		{s: "%sabc", spec: "%s"},
		{s: "%%", spec: "%%"},
		{s: "%%123s", spec: "%%"},
		{s: "%1%", spec: "%1%", err: true},
		{s: "%", spec: "%", err: true},
		{s: "%y", spec: "%y", err: true},
		{s: "%yabc", spec: "%y", err: true},
		{s: "%42dabc", spec: "%42d"},
		{s: "%42.0dabc", spec: "%42.0d"},
		{s: "%42.0fabc", spec: "%42.0f"},
		{s: "%.20s.20s", spec: "%.20s"},
		{s: "%42cabc", spec: "%42c"},
		{s: "%42.0cabc", spec: "%42.0c", err: true},
	}

	for _, test := range tests {
		spec, tail, err := cutFormatSpecifier(test.s)
		wantTail := test.s[len(test.spec):]
		if spec != test.spec || tail != wantTail || (err != nil) != test.err {
			errString := "<nil>"
			if test.err {
				errString = "<error>"
			}
			t.Errorf("cutFormatSpecifier(%q) = %q, %q, %v; want %q, %q, %s", test.s, spec, tail, err, test.spec, wantTail, errString)
		}
	}
}
