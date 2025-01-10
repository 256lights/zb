// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zb.256lights.llc/pkg/sets"
)

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
	tests := []struct {
		name    string
		luaCode string
	}{
		{
			name: "Call",
			// Extra parentheses to prevent tail call.
			luaCode: "\nreturn (identify())\n",
		},
		{
			name:    "TailCall",
			luaCode: "\nreturn identify()\n",
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

			state.PushClosure(0, func(l *State) (int, error) {
				l.PushString(Where(l, 1))
				return 1, nil
			})
			if err := state.SetGlobal("identify", 0); err != nil {
				t.Fatal(err)
			}
			const chunkName Source = "=(load)"
			if err := state.Load(strings.NewReader(test.luaCode), chunkName, "t"); err != nil {
				t.Fatal(err)
			}
			if err := state.Call(0, 1, 0); err != nil {
				t.Fatal(err)
			}

			got, ok := state.ToString(-1)
			if want := "(load):2: "; got != want || !ok {
				t.Errorf("result = %q; want %q", got, want)
			}
		})
	}
}

func TestToString(t *testing.T) {
	tests := []struct {
		name        string
		push        func(l *State)
		want        string
		wantContext sets.Set[string]
	}{
		{
			name: "Integer",
			push: func(l *State) {
				l.PushInteger(42)
			},
			want: "42",
		},
		{
			name: "Float",
			push: func(l *State) {
				l.PushNumber(3.14)
			},
			want: "3.14",
		},
		{
			name: "IntegralFloat",
			push: func(l *State) {
				l.PushNumber(42)
			},
			want: "42.0",
		},
		{
			name: "String",
			push: func(l *State) {
				l.PushString("abc")
			},
			want: "abc",
		},
		{
			name: "StringWithContext",
			push: func(l *State) {
				l.PushStringContext("abc", sets.New("def", "ghi"))
			},
			want:        "abc",
			wantContext: sets.New("def", "ghi"),
		},
		{
			name: "False",
			push: func(l *State) {
				l.PushBoolean(false)
			},
			want: "false",
		},
		{
			name: "True",
			push: func(l *State) {
				l.PushBoolean(true)
			},
			want: "true",
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
			got, gotContext, err := ToString(state, -1)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want || !cmp.Equal(test.wantContext, gotContext, cmpopts.EquateEmpty()) {
				t.Errorf("ToString(l, -1) = %q, %q; want %q, %q", got, gotContext, test.want, test.wantContext)
			}
		})
	}
}
