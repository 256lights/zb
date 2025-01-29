// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"context"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zb.256lights.llc/pkg/internal/lualex"
	"zb.256lights.llc/pkg/sets"
)

type patternTest struct {
	pattern              string
	wantPositionCaptures *sets.Bit
	tests                []submatchIndexTest
}

type submatchIndexTest struct {
	s    string
	want []int
}

var patternTests []patternTest = []patternTest{
	{
		pattern:              "()aa()",
		wantPositionCaptures: sets.NewBit(0, 1),
		tests: []submatchIndexTest{
			{"flaaap", []int{2, 4, 2, 2, 4, 4}},
		},
	},
	{
		pattern:              "()a*()",
		wantPositionCaptures: sets.NewBit(0, 1),
		tests: []submatchIndexTest{
			{"abc", []int{0, 1, 0, 0, 1, 1}},
		},
	},
	{
		pattern: "(a*(.)%w(%s*))",
		tests: []submatchIndexTest{
			{"", []int{}},
			{"abc", []int{0, 3, 0, 3, 1, 2, 3, 3}},
			{"aabc", []int{0, 4, 0, 4, 2, 3, 4, 4}},
			{"abc  def", []int{0, 5, 0, 5, 1, 2, 3, 5}},
		},
	},
	{
		pattern: "[0-7%l%-]",
		tests: []submatchIndexTest{
			{"0", []int{0, 1}},
			{"7", []int{0, 1}},
			{"8", []int{}},
			{"a", []int{0, 1}},
			{"-", []int{0, 1}},
		},
	},
	{
		pattern: "[]-c]",
		tests: []submatchIndexTest{
			{"a", []int{0, 1}},
			{"-", []int{}},
		},
	},
	{
		pattern: "[a-]",
		tests: []submatchIndexTest{
			{"a", []int{0, 1}},
			{"b", []int{}},
			{"-", []int{0, 1}},
		},
	},
	{
		pattern: "[a-c-]",
		tests: []submatchIndexTest{
			{"a", []int{0, 1}},
			{"b", []int{0, 1}},
			{"c", []int{0, 1}},
			{"d", []int{}},
			{"-", []int{0, 1}},
		},
	},
	{
		pattern: "[!--]",
		tests: []submatchIndexTest{
			{"!", []int{0, 1}},
			{"#", []int{0, 1}},
			{"-", []int{0, 1}},
			{".", []int{}},
		},
	},
	{
		pattern: "^%-?0x[1-9a-f]%.?[0-9a-f]*p[-+]?%d+$",
		tests: []submatchIndexTest{
			{"0x1.999999999999ap-04", []int{0, 21}},
		},
	},
	{
		pattern: "%d+",
		tests: []submatchIndexTest{
			{"1 2 3 4 5", []int{0, 1}},
		},
	},
}

var badPatternTests = []string{
	"x%",
	"(fo.*)%1",
	"%b()",
	"[%]-c]",
	"[C-%]]",
	"[%a-z]",
	"[a-%%]",
}

func TestPatternToRegexp(t *testing.T) {
	for _, test := range patternTests {
		re, gotPositionCaptures, err := patternToRegexp(test.pattern)
		if err != nil {
			t.Errorf("patternToRegexp(%q): %v", test.pattern, err)
			continue
		}
		if !test.wantPositionCaptures.Equal(gotPositionCaptures) {
			t.Errorf("patternToRegexp(%q) = %v, %v, <nil>; want _, %v, <nil>",
				test.pattern, re, gotPositionCaptures, test.wantPositionCaptures)
		} else {
			t.Logf("patternToRegexp(%q) = %v, %v, <nil>", test.pattern, re, gotPositionCaptures)
		}

		for _, submatchIndexTest := range test.tests {
			gotSubmatches := re.FindStringSubmatchIndex(submatchIndexTest.s)
			if !slices.Equal(submatchIndexTest.want, gotSubmatches) {
				t.Errorf("patternToRegexp(%q).FindStringSubmatchIndex(%q) = %v; want %v",
					test.pattern, submatchIndexTest.s, gotSubmatches, submatchIndexTest.want)
			}
		}
	}

	for _, pattern := range badPatternTests {
		got, _, err := patternToRegexp(pattern)
		if err == nil {
			t.Errorf("patternToRegexp(%q) = %v, _, <nil>; want error", pattern, got)
		} else {
			t.Logf("patternToRegexp(%q) returned error as expected: %v", pattern, err)
		}
	}
}

func FuzzPatternToRegexp(f *testing.F) {
	for _, test := range patternTests {
		f.Add(test.pattern)
	}
	for _, pattern := range badPatternTests {
		f.Add(pattern)
	}

	f.Fuzz(func(t *testing.T, pattern string) {
		// Primarily checking for panics or infinite loops.
		got, gotPositionCaptures, err := patternToRegexp(pattern)
		if err != nil {
			return
		}
		if got == nil {
			t.Fatalf("patternToRegexp(%q) did not return a regexp", pattern)
		}
		if n, nonEmpty := gotPositionCaptures.Max(); nonEmpty && n > uint(got.NumSubexp()) {
			t.Errorf("patternToRegexp(%q) = %v, %v, <nil>; regexp only has %d captures",
				pattern, got, gotPositionCaptures, got.NumSubexp())
		}
	})
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

func TestStringGSub(t *testing.T) {
	tests := []struct {
		name string
		push func(l *State)

		want             string
		wantContext      sets.Set[string]
		wantReplacements int64
	}{
		{
			name: "SubmatchStringReplacement",
			push: func(l *State) {
				l.PushString("hello world")
				l.PushString("(%w+)")
				l.PushString("%1 %1")
			},
			want:             "hello hello world world",
			wantReplacements: 2,
		},
		{
			name: "MaxReplacements",
			push: func(l *State) {
				l.PushString("hello world")
				l.PushString("%w+")
				l.PushString("%0 %0")
				l.PushInteger(1)
			},
			want:             "hello hello world",
			wantReplacements: 1,
		},
		{
			name: "SwapWords",
			push: func(l *State) {
				l.PushString("hello world from Lua")
				l.PushString("(%w+)%s*(%w+)")
				l.PushString("%2 %1")
			},
			want:             "world hello Lua from",
			wantReplacements: 2,
		},
		{
			name: "Function",
			push: func(l *State) {
				l.PushString("home = $HOME, user = $USER")
				l.PushString("%$(%w+)")
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
			name: "Table",
			push: func(l *State) {
				l.PushString("$name-$version.tar.gz")
				l.PushString("%$(%w+)")

				l.CreateTable(0, 2)
				l.PushString("lua")
				l.RawSetField(-2, "name")
				l.PushString("5.4")
				l.RawSetField(-2, "version")
			},
			want:             "lua-5.4.tar.gz",
			wantReplacements: 2,
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
			if err := Require(ctx, state, StringLibraryName, true, OpenString); err != nil {
				t.Fatal(err)
			}
			if _, err := state.Global(ctx, StringLibraryName); err != nil {
				t.Fatal(err)
			}
			if _, err := state.Field(ctx, -1, "gsub"); err != nil {
				t.Fatal(err)
			}
			funcIndex := state.Top()

			test.push(state)
			if err := state.Call(ctx, state.Top()-funcIndex, 2); err != nil {
				t.Fatal("gsub:", err)
			}

			if got, want := state.Type(-2), TypeString; got != want {
				t.Errorf("type(s) = %v; want %v", got, want)
			} else {
				if got, _ := state.ToString(-2); got != test.want {
					t.Errorf("s = %s; want %s", lualex.Quote(got), lualex.Quote(test.want))
				}
				if diff := cmp.Diff(test.wantContext, state.StringContext(-2), cmpopts.EquateEmpty()); diff != "" {
					t.Errorf("string context (-want +got):\n%s", diff)
				}
			}

			if got, want := state.Type(-1), TypeNumber; got != want {
				t.Errorf("type(n) = %v; want %v", got, want)
			} else if got, _ := state.ToInteger(-1); got != test.wantReplacements {
				t.Errorf("n = %d; want %d", got, test.wantReplacements)
			}
		})
	}
}
