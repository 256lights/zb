// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package xhttp

import (
	"fmt"
	"strconv"
	"strings"
)

// RangeSpec is a single bytes range from an [HTTP Range header field].
// The zero value of RangeSpec is a range that covers the first byte of the representation data.
//
// [HTTP Range header field]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Range
type RangeSpec struct {
	start, end int64
}

// RangeStartingAt returns a [RangeSpec] that starts at the given byte.
// If start is negative, then it is interpreted as the number of bytes
// before the end of the representation data.
func RangeStartingAt(start int64) RangeSpec {
	return RangeSpec{start: start, end: -1}
}

// IntRange returns a [RangeSpec] that starts at the byte offset start
// and ends at the byte offset end, inclusive.
// IntRange panics if start or end is negative,
// or end is less than start.
func IntRange(start, end int64) RangeSpec {
	if start < 0 || end < 0 {
		panic("IntRange with negative values")
	}
	if start > end {
		panic("IntRange start must be less than or equal to end")
	}
	return RangeSpec{start: start, end: end}
}

// Start returns the first byte offset.
func (spec RangeSpec) Start() int64 {
	return spec.start
}

// End returns the last byte offset and whether there is a specified end.
func (spec RangeSpec) End() (_ int64, ok bool) {
	return max(spec.end, 0), spec.end >= 0
}

// Size returns the number of bytes the range represents.
func (spec RangeSpec) Size() (size int64, hasEnd bool) {
	if spec.end < 0 {
		return 0, false
	}
	return spec.end - spec.start + 1, true
}

// String returns spec in the format of the HTTP Range header.
func (spec RangeSpec) String() string {
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

func (spec RangeSpec) Resolve(n int64) (_ RangeSpec, ok bool) {
	if end, hasEnd := spec.End(); hasEnd {
		return spec, 0 <= spec.Start() && spec.Start() < n && end < n
	}
	newSpec := RangeSpec{spec.start, max(n-1, 0)}
	if newSpec.start < 0 {
		newSpec.start += n
	}
	return newSpec, 0 <= newSpec.start && newSpec.start <= n
}

// IsSuffix reports whether the start byte offset is relative to the end of representation data.
func (spec RangeSpec) IsSuffix() bool {
	return spec.start < 0
}

// ParseRange parses the content of an [HTTP Range header field] into zero or more [RangeSpec] values.
//
// [HTTP Range header field]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Range
func ParseRange(rangeHeader string) ([]RangeSpec, error) {
	if rangeHeader == "" {
		return nil, nil
	}
	rangeHeader, ok := strings.CutPrefix(rangeHeader, "bytes=")
	if !ok {
		unit, _, _ := strings.Cut(rangeHeader, "=")
		return nil, fmt.Errorf("parse range header: unsupported unit %q", unit)
	}
	var result []RangeSpec
	for spec := range strings.SplitSeq(rangeHeader, ",") {
		spec = strings.Trim(spec, " \t")
		start, end, hasDash := strings.Cut(spec, "-")
		switch {
		case hasDash && start == "" && isDigits(end):
			i, err := strconv.ParseInt(spec, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse range header: suffix-range %q: %v", spec, err)
			}
			result = append(result, RangeSpec{start: i, end: -1})
		case hasDash && isDigits(start) && end == "":
			i, err := strconv.ParseInt(start, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse range header: int-range %q: %v", spec, err)
			}
			result = append(result, RangeSpec{start: i, end: -1})
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
			result = append(result, RangeSpec{start: i, end: j})
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
		if !isDigit(rune(c)) {
			return false
		}
	}
	return true
}
