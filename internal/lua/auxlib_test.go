// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"strings"
	"testing"
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
