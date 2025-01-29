// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"context"
	"testing"
)

func TestTableSort(t *testing.T) {
	tests := []struct {
		name     string
		pushInit func(l *State)
		compare  Function
		pushWant func(l *State)
	}{
		{
			name:     "Empty",
			pushInit: func(l *State) {},
			pushWant: func(l *State) {},
		},
		{
			name: "AlreadySorted",
			pushInit: func(l *State) {
				l.PushInteger(123)
				l.PushInteger(456)
				l.PushInteger(789)
			},
			pushWant: func(l *State) {
				l.PushInteger(123)
				l.PushInteger(456)
				l.PushInteger(789)
			},
		},
		{
			name: "ReverseSorted",
			pushInit: func(l *State) {
				l.PushInteger(789)
				l.PushInteger(456)
				l.PushInteger(123)
			},
			pushWant: func(l *State) {
				l.PushInteger(123)
				l.PushInteger(456)
				l.PushInteger(789)
			},
		},
		{
			name: "AnyOrder",
			pushInit: func(l *State) {
				l.PushInteger(123)
				l.PushInteger(789)
				l.PushInteger(456)
			},
			pushWant: func(l *State) {
				l.PushInteger(123)
				l.PushInteger(456)
				l.PushInteger(789)
			},
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
			if err := Require(ctx, state, TableLibraryName, false, OpenTable); err != nil {
				t.Fatal(err)
			}
			if _, err := state.Field(ctx, -1, "sort"); err != nil {
				t.Fatal(err)
			}
			state.CreateTable(0, 0)
			state.PushValue(-1)
			state.Insert(-3) // Keep another reference to table underneath function reference.
			tableArgIndex := state.Top()
			funcIndex := tableArgIndex - 1
			tableIndex := funcIndex - 1
			test.pushInit(state)
			for state.Top() > tableArgIndex {
				i := int64(state.Top() - tableArgIndex)
				if err := state.SetIndex(ctx, tableIndex, i); err != nil {
					t.Fatalf("list[%d]: %v", i, err)
				}
			}
			if test.compare != nil {
				state.PushClosure(0, test.compare)
			}

			if err := state.Call(ctx, state.Top()-funcIndex, 0); err != nil {
				t.Error("table.sort:", err)
			}

			test.pushWant(state)
			wantLen := state.Top() - tableIndex
			if got, err := Len(ctx, state, tableIndex); err != nil {
				t.Fatal(err)
			} else if got != int64(wantLen) {
				t.Errorf("#list = %d; want %d", got, wantLen)
			}

			for i := range wantLen {
				state.Rotate(tableIndex+1, -1)
				if _, err := state.Index(ctx, tableIndex, int64(i+1)); err != nil {
					t.Errorf("list[%d]: %v", i+1, err)
				}
				if !state.RawEqual(-2, -1) {
					t.Errorf("list[%d] = %v; want %v", i+1, describeValue(state, -1), describeValue(state, -2))
				}
				state.Pop(2)
			}
		})
	}
}
