// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/alecthomas/kong"
	"zb.256lights.llc/pkg/internal/luac"
)

func main() {
	kongContext := kong.Parse(new(luac.Command), kong.Name("zb-luac"))
	err := kongContext.Run()
	kongContext.FatalIfErrorf(err)
}
