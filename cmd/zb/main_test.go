// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/alecthomas/kong"
)

func TestKongTags(t *testing.T) {
	if _, err := kong.New(new(zbCommand), zbKongOption()); err != nil {
		t.Error("kong.New:", err)
	}
}
