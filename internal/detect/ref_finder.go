// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package detect

import (
	"cmp"
	"iter"
	"slices"
	"unicode"
	"unicode/utf16"

	"zb.256lights.llc/pkg/sets"
)

// A RefFinder records which elements in a set of search strings
// occur in a byte stream.
type RefFinder struct {
	root    *refFinderNode
	threads []*refFinderNode
	found   sets.Sorted[string]
}

// NewRefFinder returns a new [RefFinder] that searches for strings from the given sequence.
func NewRefFinder(search iter.Seq[string]) *RefFinder {
	rf := new(RefFinder)
	var hasEmpty bool
	rf.root, hasEmpty = buildRefFinderTree(search)
	if hasEmpty {
		rf.found.Add("")
	}
	return rf
}

func buildRefFinderTree(search iter.Seq[string]) (root *refFinderNode, hasEmpty bool) {
	root = new(refFinderNode)
	for s := range search {
		if s == "" {
			hasEmpty = true
			continue
		}
		root.add(s, encodeUTF8)
		root.add(s, encodeUTF16LE)
	}
	return root, hasEmpty
}

// Found returns the set of references found in the written content so far.
func (rf *RefFinder) Found() *sets.Sorted[string] {
	return rf.found.Clone()
}

// Write implements [io.Writer]
// by recording any occurrences of the strings the [RefFinder] is searching for
// that are found in p.
// The bytes written to the [RefFinder] are considered a contiguous stream:
// occurrences may span multiple calls to Write or [RefFinder.WriteString].
func (rf *RefFinder) Write(p []byte) (int, error) {
	for _, b := range p {
		rf.write(b)
	}
	return len(p), nil
}

// WriteString implements [io.StringWriter]
// by recording any occurrences of the strings the [RefFinder] is searching for
// that are found in s.
// The bytes written to the [RefFinder] are considered a contiguous stream:
// occurrences may span multiple calls to WriteString or [RefFinder.Write].
func (rf *RefFinder) WriteString(s string) (int, error) {
	for _, b := range []byte(s) { // Go compiler elides allocation.
		rf.write(b)
	}
	return len(s), nil
}

// write evaluates the next byte of the stream.
// A RefFinder maintains a set of "threads",
// which are pointers in the tree structure created in [buildRefFinderTree].
// write advances each of these threads.
// In addition, write may spawn a new thread
// if b matches any children of rf.root.
func (rf *RefFinder) write(b byte) {
	rf.threads = append(rf.threads, rf.root)

	n := 0
	for _, curr := range rf.threads {
		i, ok := curr.find(b)
		if !ok {
			continue
		}
		next := curr.children[i]
		if next.match != "" {
			rf.found.Add(next.match)
		}
		if len(next.children) > 0 {
			rf.threads[n] = next
			n++
		}
	}
	clear(rf.threads[n:])
	rf.threads = rf.threads[:n]
}

type refFinderNode struct {
	b        byte
	match    string
	children []*refFinderNode
}

func (node *refFinderNode) find(b byte) (i int, ok bool) {
	return slices.BinarySearchFunc(node.children, b, func(child *refFinderNode, b byte) int {
		return cmp.Compare(child.b, b)
	})
}

func (node *refFinderNode) add(s string, encode func(string) iter.Seq[byte]) {
	for b := range encode(s) {
		if i, ok := node.find(b); ok {
			node = node.children[i]
		} else {
			newNode := &refFinderNode{b: b}
			node.children = slices.Insert(node.children, i, newNode)
			node = newNode
		}
	}
	node.match = s
}

func encodeUTF8(s string) iter.Seq[byte] {
	return func(yield func(byte) bool) {
		for _, b := range []byte(s) {
			if !yield(b) {
				return
			}
		}
	}
}

func encodeUTF16LE(s string) iter.Seq[byte] {
	return func(yield func(byte) bool) {
		for _, r := range s {
			switch utf16.RuneLen(r) {
			case 1:
				if !yield(byte(r)) {
					return
				}
				if !yield(byte(r >> 8)) {
					return
				}
			case 2:
				r1, r2 := utf16.EncodeRune(r)
				if !yield(byte(r1)) {
					return
				}
				if !yield(byte(r1 >> 8)) {
					return
				}
				if !yield(byte(r2)) {
					return
				}
				if !yield(byte(r2 >> 8)) {
					return
				}
			default:
				if !yield(byte(unicode.ReplacementChar & 0xff)) {
					return
				}
				if !yield(byte(unicode.ReplacementChar >> 8)) {
					return
				}
			}
		}
	}
}
