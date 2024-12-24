// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import (
	"math"
	"testing"
)

func FuzzCeilLog2(f *testing.F) {
	for i := range uint(256) {
		f.Add(i)
	}

	f.Fuzz(func(t *testing.T, x uint) {
		if x == 0 {
			return
		}
		got := int64(ceilLog2(x))
		want := int64(math.Ceil(math.Log2(float64(x))))
		if got != want {
			t.Errorf("ceilLog2(%d) = %d; want %d", x, got, want)
		}
	})
}
