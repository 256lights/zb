// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package mylua

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/google/go-cmp/cmp"
)

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

	t.Run("ReadError", func(t *testing.T) {
		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()

		const message = "bork"
		r := io.MultiReader(strings.NewReader("return"), iotest.ErrReader(errors.New(message)))
		err := state.Load(r, "=(reader)", "t")
		if err == nil {
			t.Error("state.Load(...) = <nil>; want error")
		} else if got := err.Error(); !strings.Contains(got, message) {
			t.Errorf("state.Load(...) = %v; want to contain %q", got, message)
		}
		if got, ok := state.ToString(-1); !strings.Contains(got, message) || !ok {
			t.Errorf("state.ToString(-1) = %q, %t; want to contain %q", got, ok, message)
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
