// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"

	"zb.256lights.llc/pkg/internal/luac"
)

func main() {
	rootCommand := luac.New()
	if err := rootCommand.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "zb-luac:", err)
		os.Exit(1)
	}
}
