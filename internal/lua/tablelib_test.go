// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zb.256lights.llc/pkg/internal/luacode"
	"zb.256lights.llc/pkg/internal/lualex"
)

func TestTablePack(t *testing.T) {
	tests := [][]any{
		{},
		{int64(42)},
		{int64(123), int64(456), int64(789)},
		{int64(123), nil, int64(789)},
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

			state.PushClosure(0, OpenTable)
			if err := state.Call(ctx, 0, 1); err != nil {
				t.Error(err)
				return
			}
			if _, err := state.Field(ctx, -1, "pack"); err != nil {
				t.Error(err)
				return
			}
			funcIndex := state.Top()

			testName := "table.pack("
			for i, x := range test {
				if i != 0 {
					testName += ", "
				}
				pushValue(state, x)
				switch x := x.(type) {
				case nil:
					testName += "nil"
				case int64:
					testName = fmt.Sprintf("%s%d", testName, x)
				case float64:
					testName += luacode.FloatValue(x).String()
				case string:
					testName += lualex.Quote(x)
				}
			}
			testName += ")"

			if err := state.Call(ctx, state.Top()-funcIndex, 1); err != nil {
				t.Errorf("%s: %v", testName, err)
				return
			}
			if got, want := state.Type(-1), TypeTable; got != want {
				t.Errorf("type(%s) = %v; want %v", testName, got, want)
				return
			}
			nType := state.RawField(-1, "n")
			if nType != TypeNumber || !state.IsInteger(-1) {
				t.Errorf("type(%s.n) = %v; want integer", testName, nType)
			} else if got, _ := state.ToInteger(-1); got != int64(len(test)) {
				t.Errorf("%s.n = %d; want %d", testName, got, len(test))
			}
			state.Pop(1)

			var got []any
			for i := range test {
				state.RawIndex(-1, int64(i+1))
				x, err := valueToGo(state, -1)
				if err != nil {
					t.Errorf("%s[%d]: %v", testName, i+1, err)
				}
				got = append(got, x)
				state.Pop(1)
			}
			if diff := cmp.Diff(test, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("%s (-want +got):\n%s", testName, diff)
			}
		}()
	}
}

func TestTableUnpack(t *testing.T) {
	tests := []struct {
		list []int64
		i    int64
		j    int64
		want []int64
	}{
		{
			list: []int64{},
			i:    1,
			j:    0,
			want: []int64{},
		},
		{
			list: []int64{42},
			i:    1,
			j:    1,
			want: []int64{42},
		},
		{
			list: []int64{123, 456, 789},
			i:    1,
			j:    3,
			want: []int64{123, 456, 789},
		},
		{
			list: []int64{123, 456, 789},
			i:    2,
			j:    3,
			want: []int64{456, 789},
		},
		{
			list: []int64{123, 456, 789},
			i:    1,
			j:    2,
			want: []int64{123, 456},
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

			state.PushClosure(0, OpenTable)
			if err := state.Call(ctx, 0, 1); err != nil {
				t.Error(err)
				return
			}
			if _, err := state.Field(ctx, -1, "unpack"); err != nil {
				t.Error(err)
				return
			}
			funcIndex := state.Top()

			testName := "table.unpack({"
			state.CreateTable(len(test.list), 0)
			for i, x := range test.list {
				state.PushInteger(x)
				if err := state.RawSetIndex(-2, int64(1+i)); err != nil {
					t.Error(err)
					return
				}
				if i == 0 {
					testName = fmt.Sprintf("%s%d", testName, x)
				} else {
					testName = fmt.Sprintf("%s, %d", testName, x)
				}
			}
			testName += "}"
			if hasJ := test.j != int64(len(test.list)); test.i != 1 || hasJ {
				state.PushInteger(test.i)
				testName = fmt.Sprintf("%s, %d", testName, test.i)

				if hasJ {
					state.PushInteger(test.j)
					testName = fmt.Sprintf("%s, %d", testName, test.j)
				}
			}
			testName += ")"

			if err := state.Call(ctx, state.Top()-funcIndex, MultipleReturns); err != nil {
				t.Errorf("%s: %v", testName, err)
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
