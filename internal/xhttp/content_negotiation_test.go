// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package xhttp

import (
	"encoding"
	"fmt"
	"strings"
	"testing"
)

func TestEncodingQuality(t *testing.T) {
	tests := []struct {
		acceptEncoding []string
		coding         string
		want           QValue
	}{
		{[]string{}, "identity", QValueMax},
		{[]string{}, "gzip", QValueMax},
		{[]string{""}, "identity", QValueMax},
		{[]string{""}, "gzip", QValueMax},
		{[]string{"compress, gzip"}, "identity", QValueMax},
		{[]string{"compress, gzip"}, "compress", QValueMax},
		{[]string{"compress, gzip"}, "gzip", QValueMax},
		{[]string{"compress, gzip"}, "br", 0},
		{[]string{"*"}, "identity", QValueMax},
		{[]string{"*"}, "gzip", QValueMax},
		{[]string{"compress;q=0.5, gzip;q=1.0"}, "identity", QValueMax},
		{[]string{"compress;q=0.5, gzip;q=1.0"}, "compress", 500},
		{[]string{"compress;q=0.5, gzip;q=1.0"}, "gzip", QValueMax},
		{[]string{"compress;q=0.5, gzip;q=1.0"}, "br", 0},
		{[]string{"compress;q=0.5", "gzip;q=1.0"}, "identity", QValueMax},
		{[]string{"compress;q=0.5", "gzip;q=1.0"}, "compress", 500},
		{[]string{"compress;q=0.5", "gzip;q=1.0"}, "gzip", QValueMax},
		{[]string{"compress;q=0.5", "gzip;q=1.0"}, "br", 0},
		{[]string{"gzip;q=1.0, identity; q=0.5, *;q=0"}, "identity", 500},
		{[]string{"gzip;q=1.0, identity; q=0.5, *;q=0"}, "gzip", QValueMax},
		{[]string{"gzip;q=1.0, identity; q=0.5, *;q=0"}, "br", 0},
		{[]string{"gzip;q=1.0", "identity; q=0.5", "*;q=0"}, "identity", 500},
		{[]string{"gzip;q=1.0", "identity; q=0.5", "*;q=0"}, "gzip", QValueMax},
		{[]string{"gzip;q=1.0", "identity; q=0.5", "*;q=0"}, "br", 0},
		{[]string{"gzip;q=1.0, *;q=0, identity; q=0.5"}, "identity", 500},
		{[]string{"gzip;q=1.0, *;q=0, identity; q=0.5"}, "gzip", QValueMax},
		{[]string{"gzip;q=1.0, *;q=0, identity; q=0.5"}, "br", 0},
		{[]string{"gzip;q=1.0, *;q=0"}, "identity", 0},
		{[]string{"gzip;q=1.0, *;q=0"}, "gzip", QValueMax},
		{[]string{"gzip;q=1.0, *;q=0"}, "br", 0},
	}

	for _, test := range tests {
		got := EncodingQuality(test.acceptEncoding, test.coding)
		if got != test.want {
			t.Errorf("EncodingQuality(%q, %q) = %v; want %v", test.acceptEncoding, test.coding, got, test.want)
		}
	}
}

var (
	_ encoding.TextMarshaler   = QValueMin
	_ encoding.TextAppender    = QValueMin
	_ encoding.TextUnmarshaler = (*QValue)(nil)
)

func TestQValue(t *testing.T) {
	t.Run("String", func(t *testing.T) {
		for i := -0x8000; i < 0x8000; i++ {
			q := QValue(i)
			if got, want := q.String(), trimmedQValueString(q); got != want {
				t.Errorf("QValue(%d).String() = %q; want %q", int16(q), got, want)
			}
		}
	})

	t.Run("Float32", func(t *testing.T) {
		for i := float32(-0x8000); i < 0x8000; i++ {
			q := QValue(i)
			if got, want := q.Float32(), i/1_000; got != want {
				t.Errorf("QValue(%d).Float32() = %v; want %v", int16(q), got, want)
			}
		}
	})

	t.Run("AppendText", func(t *testing.T) {
		for i := -0x8000; i < 0x8000; i++ {
			q := QValue(i)
			got, err := q.AppendText(nil)
			if 0 <= q && q <= QValueMax {
				if want := trimmedQValueString(q); string(got) != want || err != nil {
					t.Errorf("QValue(%d).AppendText(nil) = %q, %v; want %q, <nil>",
						int16(q), got, err, want)
				}
			} else if err == nil {
				t.Errorf("QValue(%d).AppendText(nil) = %q, <nil>; want _, <error>", int16(q), got)
			}
		}
	})

	t.Run("MarshalText", func(t *testing.T) {
		for i := -0x8000; i < 0x8000; i++ {
			q := QValue(i)
			got, err := q.MarshalText()
			if 0 <= q && q <= QValueMax {
				if want := trimmedQValueString(q); string(got) != want || err != nil {
					t.Errorf("QValue(%d).MarshalText() = %q, %v; want %q, <nil>",
						int16(q), got, err, want)
				}
			} else if err == nil {
				t.Errorf("QValue(%d).MarshalText() = %q, <nil>; want _, <error>", int16(q), got)
			}
		}
	})

	t.Run("UnmarshalText", func(t *testing.T) {
		for i := range int(QValueMax) + 1 {
			want := QValue(i)

			var got QValue
			s := fullQValueString(want)
			if err := got.UnmarshalText([]byte(s)); err != nil {
				t.Errorf("UnmarshalText(%q): %v", s, err)
			} else if got != want {
				t.Errorf("after UnmarshalText(%q), q = %d; want %d", s, int16(got), int16(want))
			}

			got = 0
			s = trimmedQValueString(want)
			if err := got.UnmarshalText([]byte(s)); err != nil {
				t.Errorf("UnmarshalText(%q): %v", s, err)
			} else if got != want {
				t.Errorf("after UnmarshalText(%q), q = %d; want %d", s, int16(got), int16(want))
			}
		}
	})

	t.Run("Format", func(t *testing.T) {
		tests := []struct {
			formatString string
			q            QValue
			want         string
		}{
			{"%f", QValue(0), "0"},
			{"%#f", QValue(0), "0."},
			{"%.1f", QValue(0), "0.0"},
			{"%f", QValue(900), "0.9"},
			{"%.1f", QValue(900), "0.9"},
			{"%f", QValue(1_000), "1"},
			{"%#f", QValue(1_000), "1."},
			{"%.1f", QValue(1_000), "1.0"},
			{"%.2f", QValue(4), "0.00"},
			{"%.2f", QValue(5), "0.01"},
			{"%.2f", QValue(6), "0.01"},
			{"%8.2f", QValue(100), "    0.10"},
			{"%08.2f", QValue(100), "00000.10"},
			{"%08.2f", QValue(-100), "-0000.10"},
			{"% 08.2f", QValue(100), " 0000.10"},
			{"%+08.2f", QValue(100), "+0000.10"},
			{"%-8.2f", QValue(100), "0.10    "},
			{"% -8.2f", QValue(100), " 0.10   "},
			{"%+-8.2f", QValue(100), "+0.10   "},
			{"%-08.2f", QValue(100), "0.10    "},
			{"% .2f", QValue(100), " 0.10"},
			{"% .2f", QValue(-100), "-0.10"},
			{"%.0f", QValue(999), "1"},
			{"%.1f", QValue(999), "1.0"},
			{"%.2f", QValue(999), "1.00"},
			{"%.3f", QValue(999), "0.999"},
			{"%.4f", QValue(999), "0.9990"},
		}

		for _, test := range tests {
			got := fmt.Sprintf(test.formatString, test.q)
			if got != test.want {
				t.Errorf("fmt.Sprintf(%q, QValue(%d)) = %q; want %q",
					test.formatString, int16(test.q), got, test.want)
			}
		}
	})
}

func fullQValueString(q QValue) string {
	abs := int(q)
	if abs < 0 {
		abs = -abs
	}
	intPart := abs / 1_000
	fracPart := abs % 1_000
	if q < 0 {
		return fmt.Sprintf("-%d.%03d", intPart, fracPart)
	}
	return fmt.Sprintf("%d.%03d", intPart, fracPart)
}

func trimmedQValueString(q QValue) string {
	s := strings.TrimRight(fullQValueString(q), "0")
	return strings.TrimSuffix(s, ".")
}
