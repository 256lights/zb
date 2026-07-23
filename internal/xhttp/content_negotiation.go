// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package xhttp

import (
	"cmp"
	"fmt"
	"iter"
	"slices"
	"strconv"
	"strings"
)

// EncodingQuality returns the [QValue] for the content coding
// specified in the [Accept-Encoding header field] value.
//
// [Accept-Encoding header field]: https://datatracker.ietf.org/doc/html/rfc9110#section-12.5.3
func EncodingQuality(acceptEncoding []string, coding string) QValue {
	coding = cmp.Or(coding, "identity")
	hasValues := false
	var wildcard QValue
	wildcardSet := false
	for k, v := range parseAcceptEncoding(acceptEncoding) {
		hasValues = true
		switch k {
		case "*":
			if !wildcardSet {
				wildcard = v
				wildcardSet = true
			}
		case coding:
			return v
		}
	}
	if !hasValues || coding == "identity" && !wildcardSet {
		return QValueMax
	}
	return wildcard
}

func parseAcceptEncoding(values []string) iter.Seq2[string, QValue] {
	return func(yield func(string, QValue) bool) {
		for _, value := range values {
			for elem := range SplitList(value) {
				coding, weight, hasWeight := strings.Cut(elem, ";")
				coding = strings.TrimRight(coding, whitespace)
				if coding != "*" && !isToken(coding) {
					continue
				}
				q := QValueMax
				if hasWeight {
					weight = strings.TrimLeft(weight, whitespace)
					weight, hasQ := strings.CutPrefix(weight, "q=")
					if !hasQ {
						continue
					}
					var err error
					q, err = parseQValue(weight)
					if err != nil {
						continue
					}
				}
				if !yield(coding, q) {
					return
				}
			}
		}
	}
}

// A QValue is a fixed-point [quality value].
// A zero indicates "not acceptable".
//
// [quality value]: https://datatracker.ietf.org/doc/html/rfc9110#section-12.4.2
type QValue int16

const (
	// QValueMin is the minimum acceptable [QValue].
	QValueMin QValue = 1
	// QValueMax is the maximum acceptable [QValue].
	QValueMax QValue = 1_000
)

// String formats q as a decimal.
func (q QValue) String() string {
	return string(q.format(nil, 0, -1, 0))
}

// AppendText appends q as a decimal to the dst byte slice
// and returns the new slice.
// AppendText returns an error if q is not in the range [0, [QValueMax]].
func (q QValue) AppendText(dst []byte) ([]byte, error) {
	if q < 0 || q > QValueMax {
		return dst, fmt.Errorf("%v is out of range [0,%v]", q, QValueMax)
	}
	return q.format(dst, 0, -1, 0), nil
}

// MarshalText is equivalent to calling [QValue.MarshalText] with a nil slice.
func (q QValue) MarshalText() ([]byte, error) {
	return q.AppendText(nil)
}

// UnmarshalText parses a quality value into *q.
func (q *QValue) UnmarshalText(text []byte) error {
	var err error
	*q, err = parseQValue(text)
	return err
}

const (
	qvalueAlwaysSign uint8 = 1 << iota
	qvalueLeftJustify
	qvalueAlwaysDecimalPoint
	qvalueSpaceForElidedSign
	qvalueZeroPad
)

func (q QValue) format(dst []byte, minWidth, prec int, flags uint8) []byte {
	dst = slices.Grow(dst, minWidth)
	start := len(dst)
	if q < 0 {
		dst = append(dst, '-')
	} else if flags&qvalueAlwaysSign != 0 {
		dst = append(dst, '+')
	} else if flags&qvalueSpaceForElidedSign != 0 {
		dst = append(dst, ' ')
	}
	afterSign := len(dst)

	intPart := int16(q / 1_000)
	if q < 0 {
		intPart = -intPart
	}
	var fracPart int16
	switch {
	case q == -0x8000:
		fracPart = (0x8000 % 1_000)
	case q < 0:
		fracPart = int16(-q % 1_000)
	default:
		fracPart = int16(q % 1_000)
	}
	switch {
	case prec == 0 && fracPart >= 500:
		intPart++
	case prec == 1 && fracPart%100 >= 50:
		fracPart = fracPart - (fracPart % 100) + 100
		if fracPart >= 1_000 {
			fracPart -= 1_000
			intPart++
		}
	case prec == 2 && fracPart%10 >= 5:
		fracPart = fracPart - (fracPart % 10) + 10
		if fracPart >= 1_000 {
			fracPart -= 1_000
			intPart++
		}
	}
	dst = strconv.AppendInt(dst, int64(intPart), 10)

	if flags&qvalueAlwaysDecimalPoint != 0 || (prec == -1 && fracPart != 0) || prec > 0 {
		dst = append(dst, '.')
	}
	const decimalDigits = "0123456789"
	for i, div := 0, int16(100); div != 0 && (i < prec || (fracPart != 0 && prec == -1)); i, div = i+1, div/10 {
		dst = append(dst, decimalDigits[fracPart/div])
		fracPart %= div
	}
	for range prec - 3 {
		dst = append(dst, '0')
	}
	switch newEnd := start + minWidth; {
	case flags&qvalueLeftJustify != 0:
		for len(dst) < newEnd {
			dst = append(dst, ' ')
		}
	case newEnd > len(dst) && flags&qvalueZeroPad != 0:
		padWidth := newEnd - len(dst)
		buf := dst[afterSign:newEnd] // Guaranteed by slices.Grow above.
		copy(buf[padWidth:], buf)
		for i := range padWidth {
			buf[i] = '0'
		}
		dst = dst[:newEnd]
	case newEnd > len(dst) && flags&qvalueZeroPad == 0:
		padWidth := newEnd - len(dst)
		buf := dst[start:newEnd] // Guaranteed by slices.Grow above.
		copy(buf[padWidth:], buf)
		for i := range padWidth {
			buf[i] = ' '
		}
		dst = dst[:newEnd]
	}
	return dst
}

// Float32 returns q as a floating-point number.
func (q QValue) Float32() float32 {
	return float32(q) / 1_000
}

// Format implements [fmt.Formatter]
// to format q for the "%f" and "%v" verbs.
func (q QValue) Format(f fmt.State, verb rune) {
	switch verb {
	case 'f', 'F':
		var flags uint8
		if f.Flag('+') {
			flags |= qvalueAlwaysSign
		}
		if f.Flag('-') {
			flags |= qvalueLeftJustify
		}
		if f.Flag('#') {
			flags |= qvalueAlwaysDecimalPoint
		}
		if f.Flag(' ') {
			flags |= qvalueSpaceForElidedSign
		}
		if f.Flag('0') {
			flags |= qvalueZeroPad
		}
		width, hasWidth := f.Width()
		if !hasWidth {
			width = 0
		}
		prec, hasPrec := f.Precision()
		if !hasPrec {
			prec = -1
		}
		f.Write(q.format(nil, width, prec, flags))
	case 'v':
		fmtString := new(strings.Builder)
		fmtString.Grow(len("%-0*.*s"))
		fmtString.WriteByte('%')
		args := make([]any, 0, 3)
		if f.Flag('#') {
			fmt.Fprintf(f, "xhttp.QValue(%d)", int16(q))
			return
		}
		const allowedFlags = "-0"
		for _, flag := range []byte(allowedFlags) {
			if f.Flag(int(flag)) {
				fmtString.WriteByte(flag)
			}
		}
		if w, ok := f.Width(); ok {
			fmtString.WriteByte('*')
			args = append(args, w)
		}
		if p, ok := f.Precision(); ok {
			fmtString.WriteString(".*")
			args = append(args, p)
		}
		fmtString.WriteByte('s')
		args = append(args, q.String())
		fmt.Fprintf(f, fmtString.String(), args...)
	default:
		var buf []byte
		buf = append(buf, "%!"...)
		buf = append(buf, string(verb)...)
		buf = append(buf, "(xhttp.QValue="...)
		buf = q.format(buf, 0, -1, 0)
		buf = append(buf, ")"...)
		f.Write(buf)
	}
}

func parseQValue[S ~string | ~[]byte](s S) (QValue, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("parse qvalue: empty")
	}
	var q QValue
	switch s[0] {
	case '0':
	case '1':
		q = 1_000
	default:
		return 0, fmt.Errorf("parse qvalue: first digit must be 0/1 (got %+q)", s[0])
	}
	if len(s) == 1 {
		return q, nil
	}
	if s[1] != '.' {
		return 0, fmt.Errorf("parse qvalue: first digit must be followed by '.' (got %+q)", s[1])
	}
	if len(s) == 2 {
		return q, nil
	}

	frac := s[2:]
	const maxPrecision = 3
	overflow := len(frac) > maxPrecision
	if overflow {
		frac = frac[:maxPrecision]
	}
	if q == 1_000 {
		for _, b := range []byte(frac) {
			if b != '0' {
				return 0, fmt.Errorf("parse qvalue: '1.' must be followed by zeroes (got %+q)", b)
			}
		}
	} else {
		mul := QValue(100)
		for _, b := range []byte(frac) {
			if !isDigit(rune(b)) {
				return 0, fmt.Errorf("parse qvalue: '0.' must be followed by digits (got %+q)", b)
			}
			q += QValue(b-'0') * mul
			mul /= 10
		}
	}
	if overflow {
		return 0, fmt.Errorf("parse qvalue: max precision is %d", maxPrecision)
	}
	return q, nil
}
