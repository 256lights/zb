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

	"github.com/spf13/cobra"
	"zb.256lights.llc/pkg/internal/frontend"
	"zb.256lights.llc/pkg/internal/system"
	"zombiezen.com/go/log"
)

// zbVersion is the version string filled in by the linker (e.g. "1.2.3").
var zbVersion string

func newVersionCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "version",
		Short:                 "show version information",
		DisableFlagsInUseLine: true,
		Args:                  cobra.NoArgs,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		return runVersion(cmd.Context())
	}
	return c
}

func runVersion(ctx context.Context) error {
	firstLine := "zb"
	if zbVersion == "" {
		firstLine += " (version unknown)"
	} else {
		firstLine += " version " + zbVersion
	}

	currSystem := system.Current()
	fmt.Printf("%s\nSystem:       %v\nCPUs:         %d\n", firstLine, frontend.SystemTriple(currSystem), runtime.NumCPU())

	switch {
	case currSystem.OS.IsLinux():
		output, err := exec.CommandContext(ctx, "uname", "-srv").Output()
		if err != nil {
			log.Errorf(ctx, "uname: %v", err)
		} else {
			output = bytes.TrimSuffix(output, []byte("\n"))
			fmt.Printf("OS:           %s\n", output)
		}

		output, err = exec.CommandContext(ctx, "lsb_release", "-ds").Output()
		if errors.Is(err, exec.ErrNotFound) {
			log.Debugf(ctx, "lsb_release: %v", err)
		} else if err != nil {
			log.Errorf(ctx, "lsb_release: %v", err)
		} else {
			output = bytes.TrimSuffix(output, []byte("\n"))
			fmt.Printf("Distribution: %s\n", output)
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
			fmt.Printf("OS:           macOS %s (build %s)\n", productVersion, buildVersion)
		case len(productVersion) > 0:
			fmt.Printf("OS:           macOS %s\n", productVersion)
		case len(buildVersion) > 0:
			fmt.Printf("OS:           macOS %s\n", buildVersion)
		}

	case currSystem.OS.IsWindows():
		output, err := exec.CommandContext(ctx, "cmd", "/c", "ver").Output()
		if err != nil {
			log.Errorf(ctx, "ver: %v", err)
		} else {
			output = bytes.Trim(output, "\n\r")
			fmt.Printf("OS:           %s\n", output)
		}
	}

	return nil
}

type versionFlag struct {
	ctx context.Context
}

func (vf versionFlag) String() string     { return "false" }
func (vf versionFlag) Type() string       { return "bool" }
func (vf versionFlag) IsBoolFlag() bool   { return true }
func (vf versionFlag) Set(v string) error { return errShowVersion }

var errShowVersion = errors.New("--version flag passed")
