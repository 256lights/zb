// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package detect

import (
	"cmp"
	"slices"

	"zombiezen.com/go/zb/sortedset"
)

// A RefFinder records which elements in a set of search strings
// occur in a byte stream.
type RefFinder struct {
	root    *refFinderNode
	threads []*refFinderNode
	found   sortedset.Set[string]
}

// NewRefFinder returns a new [RefFinder] that searches for the given strings.
func NewRefFinder(search []string) *RefFinder {
	rf := &RefFinder{
		root: buildRefFinderTree(search),
	}
	if slices.Contains(search, "") {
		rf.found.Add("")
	}
	return rf
}

func buildRefFinderTree(search []string) *refFinderNode {
	root := new(refFinderNode)
	for _, s := range search {
		curr := root
		for _, b := range []byte(s) {
			if i, ok := curr.find(b); ok {
				curr = curr.children[i]
			} else {
				newNode := &refFinderNode{b: b}
				curr.children = slices.Insert(curr.children, i, newNode)
				curr = newNode
			}
		}
		curr.match = s
	}

	return root
}

// Found returns the set of references found in the written content so far.
func (rf *RefFinder) Found() *sortedset.Set[string] {
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
