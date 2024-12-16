// Copyright (C) 1994-2024 Lua.org, PUC-Rio.
// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import (
	"cmp"
	"fmt"
	"io"
	"iter"
	"slices"
)

const maxInstructionsWithoutAbsLineInfo = 128

const (
	// lineInfoRelativeLimit is the maximum value permitted
	// in elements of the rel slice of [LineInfo].
	lineInfoRelativeLimit = 1<<7 - 1

	// absMarker is the mark for entries in the rel slice of [LineInfo]
	// that have absolute information in the abs slice.
	absMarker int8 = -lineInfoRelativeLimit - 1
)

// LineInfo is a sequence of line numbers.
// The zero value is an empty sequence.
//
// The underlying data structure is optimized for a sequence of integers
// where the difference between adjacent values is relatively small (|Δ| < 128).
type LineInfo struct {
	rel []int8
	abs []absLineInfo
}

type absLineInfo struct {
	pc   int
	line int
}

// CollectLineInfo collects values from seq into a new [LineInfo] and returns it.
func CollectLineInfo(seq iter.Seq[int]) LineInfo {
	var info LineInfo
	var w lineInfoWriter
	for line := range seq {
		rel := w.next(line)
		info.rel = append(info.rel, rel)
		if rel == absMarker {
			info.abs = append(info.abs, absLineInfo{
				pc:   len(info.rel) - 1,
				line: line,
			})
		}
	}
	return info
}

// Len returns the number of line numbers in the sequence.
func (info LineInfo) Len() int {
	return len(info.rel)
}

// All returns an iterator over the sequence's line numbers.
// (The index is the instruction address.)
func (info LineInfo) All() iter.Seq2[int, int] {
	return func(yield func(int, int) bool) {
		absIndex := 0
		curr := 0
		for pc, delta := range info.rel {
			if delta != absMarker {
				curr += int(delta)
			} else {
				if info.abs[absIndex].pc != pc {
					panic("corrupted LineInfo")
				}
				curr = info.abs[absIndex].line
				absIndex++
			}

			if !yield(pc, curr) {
				return
			}
		}
	}
}

// At returns the i'th line number in the sequence.
// If i < 0 or i >= info.Len(), then Line panics.
func (info LineInfo) At(i int) int {
	// Equivalent to luaG_getfuncline in upstream Lua.

	if i < 0 || i >= info.Len() {
		panic("index out of range")
	}

	absIndex, ok := slices.BinarySearchFunc(info.abs, i, func(a absLineInfo, pc int) int {
		return cmp.Compare(a.pc, pc)
	})
	if !ok {
		// Binary search finds next largest, so go back one.
		absIndex--
	}

	currPC := 0
	lineno := 0
	if absIndex >= 0 {
		currPC = info.abs[absIndex].pc + 1 // Skip absMarker.
		lineno = info.abs[absIndex].line
	}

	for ; currPC <= i; currPC++ {
		delta := info.rel[currPC]
		if delta == absMarker {
			// Search through info.abs should have brought us to closest absMarker + 1.
			panic("corrupted LineInfo")
		}
		lineno += int(delta)
	}
	return lineno
}

func dumpLineInfo(buf []byte, base int, info LineInfo) []byte {
	if info.Len() == 0 {
		buf = dumpVarint(buf, 0)
		buf = dumpVarint(buf, 0)
		return buf
	}

	rel0, rel, abs := normalizeLineInfo(info, base)
	buf = dumpVarint(buf, 1+len(rel))
	buf = append(buf, byte(rel0))
	for _, i := range rel {
		buf = append(buf, byte(i))
	}
	buf = dumpVarint(buf, len(abs))
	for _, a := range abs {
		buf = dumpVarint(buf, a.pc)
		buf = dumpVarint(buf, a.line)
	}
	return buf
}

// normalizeLineInfo converts info to upstream Lua's algorithm.
// rel and abs may refer to info's underlying arrays.
//
// We store line info in-memory slightly differently from upstream Lua:
// rather than make Prototype.LineDefined be the base line number for the first offset,
// we have an implicit offset of zero to make [LineInfo] usable as a standalone data type.
// Frequently, the only difference between our representation and upstream Lua
// is info.rel[0] == absMarker instead of a relative offset,
// but that's easy to strip out without an allocation.
// We go through the whole exercise of verifying the entire array
// because [loadLineInfo] may import a well-formed but inefficient (or just different) packing.
func normalizeLineInfo(info LineInfo, base int) (rel0 int8, rel []int8, abs []absLineInfo) {
	w := lineInfoWriter{previousLine: base}
	relIdx := 0
	abs = info.abs
	absIdx := 0

	needsRewrite := false
	for i, line := range info.All() {
		if i == 0 {
			rel0 = w.next(line)
			isFirstAbsPC0 := len(info.abs) > 0 && info.abs[0].pc == 0
			if rel0 == absMarker && !isFirstAbsPC0 {
				needsRewrite = true
				break
			}
			if rel0 != absMarker && isFirstAbsPC0 {
				// In the common case where we transformed the first element
				// from an absolute line info to a line info relative to base,
				// only use the subsequent absolute line entries.
				abs = abs[1:]
			}
		} else {
			want := w.next(line)
			if info.rel[relIdx] != want {
				needsRewrite = true
				break
			}
			if want == absMarker {
				if abs[absIdx].pc != i {
					needsRewrite = true
					break
				}
				absIdx++
			}
		}
	}
	if !needsRewrite {
		return rel0, info.rel[1:], abs
	}

	// Reset writer and allocate new arrays.
	w = lineInfoWriter{previousLine: base}
	abs = nil
	for pc, line := range info.All() {
		delta := w.next(line)
		if pc == 0 {
			rel0 = delta
		} else {
			rel = append(rel, delta)
		}
		if delta == absMarker {
			abs = append(abs, absLineInfo{
				pc:   pc,
				line: line,
			})
		}
	}
	return rel0, rel, abs
}

func loadLineInfo(r *chunkReader, base int) (LineInfo, error) {
	n, err := r.readVarint()
	if err != nil {
		return LineInfo{}, fmt.Errorf("line info: %v", err)
	}
	info := LineInfo{
		rel: make([]int8, n),
	}
	nAbsolute := 0 // Counter for absMarker values read.
	for i := range info.rel {
		b, ok := r.readByte()
		if !ok {
			return LineInfo{}, fmt.Errorf("line info: %v", io.ErrUnexpectedEOF)
		}
		delta := int8(b)
		if delta == absMarker {
			info.rel[i] = absMarker
			nAbsolute++
		} else if i > 0 {
			info.rel[i] = delta
		} else {
			// Interpret the first element as relative to base,
			// inserting an absMarker if needed.
			rebased := base + int(delta)
			if newDelta, fitsRelative := lineInfoRelativeDelta(rebased); fitsRelative {
				info.rel[i] = newDelta
			} else {
				info.rel[i] = absMarker
				info.abs = append(info.abs, absLineInfo{
					pc:   0,
					line: rebased,
				})
			}
		}
	}

	if got, err := r.readVarint(); err != nil {
		return LineInfo{}, fmt.Errorf("line info: %v", err)
	} else if got != nAbsolute {
		return LineInfo{}, fmt.Errorf("line info: absolute line info count incorrect (%d vs. %d markers)", got, nAbsolute)
	}
	info.abs = slices.Grow(info.abs, nAbsolute)
	for i := range nAbsolute {
		var newAbsInfo absLineInfo
		newAbsInfo.pc, err = r.readVarint()
		if err != nil {
			return LineInfo{}, fmt.Errorf("line info: %v", err)
		}
		minPC := -1
		if len(info.abs) > 0 {
			minPC = info.abs[len(info.abs)-1].pc
		}
		if newAbsInfo.pc <= minPC {
			return LineInfo{}, fmt.Errorf("line info: absolute line info PCs not monotonically increasing")
		}
		if newAbsInfo.pc >= n {
			return LineInfo{}, fmt.Errorf("line info: absolute line info PC %d out of range", newAbsInfo.pc)
		}
		if info.rel[newAbsInfo.pc] != absMarker {
			return LineInfo{}, fmt.Errorf("line info: absolute line information not expected for pc %d", i)
		}

		newAbsInfo.line, err = r.readVarint()
		if err != nil {
			return LineInfo{}, fmt.Errorf("line info: %v", err)
		}

		info.abs = append(info.abs, newAbsInfo)
	}

	return info, nil
}

// A lineInfoWriter holds the state to construct a [LineInfo] a value at a time.
// This algorithm matches upstream Lua's.
type lineInfoWriter struct {
	// previousLine is the last line number passed to next.
	previousLine int
	// instructionsSinceLastAbsLineInfo is a counter
	// of instructions added since the last [absLineInfo].
	instructionsSinceLastAbsLineInfo uint8
}

// next returns the next value for the rel slice given the line.
// A new entry should be appended to LineInfo.abs
// if the returned value is [absMarker].
func (w *lineInfoWriter) next(line int) int8 {
	delta, fitsRelative := lineInfoRelativeDelta(line - w.previousLine)
	w.previousLine = line

	if !fitsRelative ||
		w.instructionsSinceLastAbsLineInfo >= maxInstructionsWithoutAbsLineInfo {
		w.instructionsSinceLastAbsLineInfo = 1
		return absMarker
	}

	w.instructionsSinceLastAbsLineInfo++
	return delta
}

// prev undoes the effects of a call to [*lineInfoWriter.next].
func (w *lineInfoWriter) prev(lastDelta int8) {
	if lastDelta == absMarker {
		// Force next line info to be absolute.
		w.instructionsSinceLastAbsLineInfo = maxInstructionsWithoutAbsLineInfo + 1
	} else {
		w.previousLine -= int(lastDelta)
		w.instructionsSinceLastAbsLineInfo--
	}
}

func lineInfoRelativeDelta(delta int) (_ int8, ok bool) {
	if delta > lineInfoRelativeLimit || delta < -lineInfoRelativeLimit {
		return absMarker, false
	}
	return int8(delta), true
}
