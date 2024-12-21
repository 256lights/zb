// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package sets

import (
	"fmt"
	"iter"
	"math/bits"
	"slices"
)

const bitWordSize = 64

// Bit is a bitmap with O(1) lookup, insertion, and deletion.
// The zero value is an empty set.
type Bit struct {
	words []uint64
}

// New returns a new set that contains the arguments passed to it.
func NewBit(elem ...uint) *Bit {
	s := new(Bit)
	s.Add(elem...)
	return s
}

// Collect returns a new set that contains the elements of the given iterator.
func CollectBit(seq iter.Seq[uint]) *Bit {
	s := new(Bit)
	s.AddSeq(seq)
	return s
}

// Add adds the arguments to the set.
func (s *Bit) Add(elem ...uint) {
	for _, x := range elem {
		s.add(x)
	}
}

// AddSeq adds the values from seq to the set.
func (s *Bit) AddSeq(seq iter.Seq[uint]) {
	for x := range seq {
		s.add(x)
	}
}

func (s *Bit) add(x uint) {
	i := x / bitWordSize
	if i >= uint(len(s.words)) {
		n := int(i - uint(len(s.words)) + 1)
		s.words = slices.Grow(s.words, n)
		s.words = s.words[:cap(s.words)]
	}
	s.words[i] |= 1 << (x % bitWordSize)
}

// Has reports whether the set contains x.
func (s *Bit) Has(x uint) bool {
	if s == nil {
		return false
	}
	i := x / bitWordSize
	if i >= uint(len(s.words)) {
		return false
	}
	return s.words[i]&(1<<(x%bitWordSize)) != 0
}

// Clone returns a new set that contains the same elements as s.
func (s *Bit) Clone() *Bit {
	if s == nil {
		return new(Bit)
	}
	return &Bit{slices.Clone(s.words)}
}

// Len returns the number of elements in the set.
func (s *Bit) Len() int {
	if s == nil {
		return 0
	}
	total := 0
	for _, word := range s.words {
		total += bits.OnesCount64(word)
	}
	return total
}

// Min returns the smallest value in the set.
func (s *Bit) Min() (_ uint, nonEmpty bool) {
	if s == nil {
		return 0, false
	}
	for x := range s.All() {
		return x, true
	}
	return 0, false
}

// Max returns the largest value in the set.
func (s *Bit) Max() (_ uint, nonEmpty bool) {
	if s == nil {
		return 0, false
	}
	for x := range s.Reversed() {
		return x, true
	}
	return 0, false
}

// All returns an iterator of the elements of s.
// Elements are in ascending order.
func (s *Bit) All() iter.Seq[uint] {
	if s == nil {
		return func(yield func(uint) bool) {}
	}
	return func(yield func(uint) bool) {
		curr := uint(0)
		for i := 0; i < len(s.words); i++ {
			if s.words[i] == 0 {
				curr += bitWordSize
				continue
			}
			for j := 0; j < bitWordSize; j++ {
				if s.words[i]&(1<<j) != 0 {
					if !yield(curr) {
						return
					}
				}
				curr++
			}
		}
	}
}

// Reversed returns an iterator of the elements of s
// in descending order.
func (s *Bit) Reversed() iter.Seq[uint] {
	if s == nil {
		return func(yield func(uint) bool) {}
	}
	return func(yield func(uint) bool) {
		curr := uint(len(s.words) * bitWordSize)
		for i := len(s.words) - 1; i >= 0; i-- {
			if s.words[i] == 0 {
				curr -= bitWordSize
				continue
			}
			for j := bitWordSize - 1; j >= 0; j-- {
				curr--
				if s.words[i]&(1<<j) != 0 {
					if !yield(curr) {
						return
					}
				}
			}
		}
	}
}

// Delete removes x from the set if present.
func (s *Bit) Delete(x uint) {
	if s == nil {
		return
	}
	i := x / bitWordSize
	if i >= uint(len(s.words)) {
		return
	}
	s.words[i] &^= 1 << (x % bitWordSize)
}

// Clear removes all elements from the set,
// but retains the space allocated for the set.
func (s *Bit) Clear() {
	if s != nil {
		clear(s.words)
	}
}

// Format implements [fmt.Formatter]
// by formatting its elements according to the printer state and verb
// surrounded by braces.
func (s *Bit) Format(f fmt.State, verb rune) {
	format(f, verb, s.All())
}
