// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

// Package rangeheader parses the [HTTP Range header].
//
// [HTTP Range header]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Range
package rangeheader

import (
	"fmt"
	"strconv"
	"strings"
)

// Spec is a single bytes range.
// The zero value of Spec is a range that covers the first byte of the representation data.
type Spec struct {
	start, end int64
}

// StartingAt returns a [Spec] that starts at the given byte.
// If start is negative, then it is interpreted as the number of bytes
// before the end of the representation data.
func StartingAt(start int64) Spec {
	return Spec{start: start, end: -1}
}

// IntRange returns a [Spec] that starts at the byte offset start
// and ends at the byte offset end, inclusive.
// IntRange panics if start or end is negative,
// or end is less than start.
func IntRange(start, end int64) Spec {
	if start < 0 || end < 0 {
		panic("IntRange with negative values")
	}
	if start > end {
		panic("IntRange start must be less than or equal to end")
	}
	return Spec{start: start, end: end}
}

// Start returns the first byte offset.
func (spec Spec) Start() int64 {
	return spec.start
}

// End returns the last byte offset and whether there is a specified end.
func (spec Spec) End() (_ int64, ok bool) {
	return max(spec.end, 0), spec.end >= 0
}

// Size returns the number of bytes the range represents.
func (spec Spec) Size() (size int64, hasEnd bool) {
	if spec.end < 0 {
		return 0, false
	}
	return spec.end - spec.start + 1, true
}

// String returns spec in the format of the HTTP Range header.
func (spec Spec) String() string {
	if spec.IsSuffix() {
		return strconv.FormatInt(spec.Start(), 10)
	}
	const maxPosLen = 19 // number of decimal digits in 2^63
	buf := make([]byte, 0, maxPosLen*2+len("-"))
	buf = strconv.AppendInt(buf, spec.Start(), 10)
	buf = append(buf, '-')
	if end, hasEnd := spec.End(); hasEnd {
		buf = strconv.AppendInt(buf, end, 10)
	}
	return string(buf)
}

func (spec Spec) Resolve(n int64) (_ Spec, ok bool) {
	if end, hasEnd := spec.End(); hasEnd {
		return spec, 0 <= spec.Start() && spec.Start() < n && end < n
	}
	newSpec := Spec{spec.start, max(n-1, 0)}
	if newSpec.start < 0 {
		newSpec.start += n
	}
	return newSpec, 0 <= newSpec.start && newSpec.start <= n
}

// IsSuffix reports whether the start byte offset is relative to the end of representation data.
func (spec Spec) IsSuffix() bool {
	return spec.start < 0
}

// Parse parses the content of an HTTP Range header into zero or more [Spec] values.
func Parse(rangeHeader string) ([]Spec, error) {
	if rangeHeader == "" {
		return nil, nil
	}
	rangeHeader, ok := strings.CutPrefix(rangeHeader, "bytes=")
	if !ok {
		unit, _, _ := strings.Cut(rangeHeader, "=")
		return nil, fmt.Errorf("parse range header: unsupported unit %q", unit)
	}
	var result []Spec
	for spec := range strings.SplitSeq(rangeHeader, ",") {
		spec = strings.Trim(spec, " \t")
		start, end, hasDash := strings.Cut(spec, "-")
		switch {
		case hasDash && start == "" && isDigits(end):
			i, err := strconv.ParseInt(spec, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse range header: suffix-range %q: %v", spec, err)
			}
			result = append(result, Spec{start: i, end: -1})
		case hasDash && isDigits(start) && end == "":
			i, err := strconv.ParseInt(start, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse range header: int-range %q: %v", spec, err)
			}
			result = append(result, Spec{start: i, end: -1})
		case hasDash && isDigits(start) && isDigits(end):
			i, err := strconv.ParseInt(start, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse range header: int-range %q: %v", spec, err)
			}
			j, err := strconv.ParseInt(end, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse range header: int-range %q: %v", spec, err)
			}
			if j < i {
				return nil, fmt.Errorf("parse range header: int-range %q: last position must be greater than first position", spec)
			}
			result = append(result, Spec{start: i, end: j})
		default:
			return nil, fmt.Errorf("parse range header: invalid spec %q", spec)
		}
	}
	return result, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range []byte(s) {
		if !('0' <= c && c <= '9') {
			return false
		}
	}
	return true
}
