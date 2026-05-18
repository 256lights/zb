// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/alecthomas/kong"
	"zb.256lights.llc/pkg/internal/frontend"
	"zb.256lights.llc/pkg/internal/system"
	"zombiezen.com/go/log"
)

// zbVersion is the version string filled in by the linker (e.g. "1.2.3").
var zbVersion string

type versionCommand struct {
}

func (c *versionCommand) Signature() string {
	return `help:"Show version information."`
}

func (c *versionCommand) Run(ctx context.Context, k *kong.Kong) error {
	firstLine := "zb"
	if zbVersion == "" {
		firstLine += " (version unknown)"
	} else {
		firstLine += " version " + zbVersion
	}

	currSystem := system.Current()
	fmt.Fprintf(k.Stdout, ""+
		"%s\n"+
		"System:       %v\n"+
		"CPUs:         %d\n",
		firstLine, frontend.SystemTriple(currSystem), runtime.NumCPU())

	switch {
	case currSystem.OS.IsLinux():
		output, err := exec.CommandContext(ctx, "uname", "-srv").Output()
		if err != nil {
			log.Errorf(ctx, "uname: %v", err)
		} else {
			output = bytes.TrimSuffix(output, []byte("\n"))
			fmt.Fprintf(k.Stdout, "OS:           %s\n", output)
		}

		output, err = exec.CommandContext(ctx, "lsb_release", "-ds").Output()
		if errors.Is(err, exec.ErrNotFound) {
			log.Debugf(ctx, "lsb_release: %v", err)
		} else if err != nil {
			log.Errorf(ctx, "lsb_release: %v", err)
		} else {
			output = bytes.TrimSuffix(output, []byte("\n"))
			fmt.Fprintf(k.Stdout, "Distribution: %s\n", output)
		}

	case currSystem.OS.IsMacOS():
		productVersion, err := exec.CommandContext(ctx, "sw_vers", "--productVersion").Output()
		if err != nil {
			log.Errorf(ctx, "sw_vers --productVersion: %v", err)
		}
		productVersion = bytes.TrimSuffix(productVersion, []byte("\n"))

		buildVersion, err := exec.CommandContext(ctx, "sw_vers", "--buildVersion").Output()
		if err != nil {
			log.Errorf(ctx, "sw_vers --buildVersion: %v", err)
		}
		buildVersion = bytes.TrimSuffix(buildVersion, []byte("\n"))

		switch {
		case len(productVersion) > 0 && len(buildVersion) > 0:
			fmt.Fprintf(k.Stdout, "OS:           macOS %s (build %s)\n", productVersion, buildVersion)
		case len(productVersion) > 0:
			fmt.Fprintf(k.Stdout, "OS:           macOS %s\n", productVersion)
		case len(buildVersion) > 0:
			fmt.Fprintf(k.Stdout, "OS:           macOS %s\n", buildVersion)
		}

	case currSystem.OS.IsWindows():
		output, err := exec.CommandContext(ctx, "cmd", "/c", "ver").Output()
		if err != nil {
			log.Errorf(ctx, "ver: %v", err)
		} else {
			output = bytes.Trim(output, "\n\r")
			fmt.Fprintf(k.Stdout, "OS:           %s\n", output)
		}
	}

	return nil
}

type versionFlag bool

func (flag *versionFlag) IgnoreDefault() {}

func (flag *versionFlag) BeforeReset() error {
	*flag = true
	return errShowVersion
}

var errShowVersion = errors.New("--version flag passed")
