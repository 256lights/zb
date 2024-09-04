// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package deque

import (
	"slices"
	"testing"
)

func TestDeque(t *testing.T) {
	tests := []struct {
		name  string
		setup func() *Deque[int]
		want  []int
	}{
		{
			name: "Nil",
			setup: func() *Deque[int] {
				return nil
			},
			want: []int{},
		},
		{
			name: "Empty",
			setup: func() *Deque[int] {
				return new(Deque[int])
			},
			want: []int{},
		},
		{
			name: "PushFront1",
			setup: func() *Deque[int] {
				d := new(Deque[int])
				d.PushFront(42)
				return d
			},
			want: []int{42},
		},
		{
			name: "PushFront3",
			setup: func() *Deque[int] {
				d := new(Deque[int])
				d.PushFront(1, 2, 3)
				return d
			},
			want: []int{1, 2, 3},
		},
		{
			name: "PushBack1",
			setup: func() *Deque[int] {
				d := new(Deque[int])
				d.PushBack(42)
				return d
			},
			want: []int{42},
		},
		{
			name: "PushBack3",
			setup: func() *Deque[int] {
				d := new(Deque[int])
				d.PushBack(1, 2, 3)
				return d
			},
			want: []int{1, 2, 3},
		},
		{
			name: "PushFrontAndBack",
			setup: func() *Deque[int] {
				d := new(Deque[int])
				d.PushBack(2, 3)
				d.PushFront(1)
				d.PushBack(4)
				return d
			},
			want: []int{1, 2, 3, 4},
		},
		{
			name: "AtArrayEdge",
			setup: func() *Deque[int] {
				d := new(Deque[int])
				d.PushFront(1, 2, 10, 10)
				d.PopFront(2)
				for end := 4; end < d.Cap(); end++ {
					d.PushBack(10)
					d.PopFront(10)
				}
				d.PushBack(20, 20)
				return d
			},
			want: []int{10, 10, 20, 20},
		},
		{
			name: "PopBack1",
			setup: func() *Deque[int] {
				d := new(Deque[int])
				d.PushBack(1, 2, 3)
				d.PopBack(1)
				return d
			},
			want: []int{1, 2},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := test.setup()

			if got, want := d.Len(), len(test.want); got != want {
				t.Errorf("new(Deque[int]).Len() = %d; want %d", got, want)
			}
			if got, want := d.Cap(), len(test.want); got < want {
				t.Errorf("new(Deque[int]).Cap() = %d; want >=%d", got, want)
			}
			if got := slices.Collect(d.Values()); !slices.Equal(got, test.want) {
				t.Errorf("slices.Collect(d.Values()) = %v; want %v", got, test.want)
			}
			{
				var got []int
				for i, x := range d.All() {
					if want := len(got); i != want {
						t.Errorf("d.All()[%d] i = %d", want, i)
					}
					got = append(got, x)
				}
				if !slices.Equal(got, test.want) {
					t.Errorf("d.All() = %v; want %v", got, test.want)
				}
			}

			if len(test.want) == 0 {
				if got, ok := d.Front(); got != 0 || ok {
					t.Errorf("new(Deque[int]).Front() = %d, %t; want 0, false", got, ok)
				}
				if got, ok := d.Back(); got != 0 || ok {
					t.Errorf("new(Deque[int]).Back() = %d, %t; want 0, false", got, ok)
				}
			} else {
				if got, ok := d.Front(); got != test.want[0] || !ok {
					t.Errorf("new(Deque[int]).Front() = %d, %t; want %d, true", got, ok, test.want[0])
				}
				if got, ok := d.Back(); got != test.want[len(test.want)-1] || !ok {
					t.Errorf("new(Deque[int]).Back() = %d, %t; want %d, true", got, ok, test.want[len(test.want)-1])
				}
			}
		})
	}
}
