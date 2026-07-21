// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package xhttp

import (
	"strings"
	"testing"
)

func TestIsTokenChar(t *testing.T) {
	sb := new(strings.Builder)
	for i := range byte(0x80) {
		if IsTokenChar(rune(i)) {
			sb.WriteByte(i)
		}
	}
	got := sb.String()
	const want = "!#$%&'*+-.0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ^_`abcdefghijklmnopqrstuvwxyz|~"
	if got != want {
		t.Errorf("token characters:\n got: %s\nwant: %s", got, want)
	}
}
