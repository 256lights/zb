// Copyright 2024 Ross Light
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"go4.org/xdgdir"
	"zombiezen.com/go/bass/sigterm"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/zb"
)

type globalConfig struct {
	cacheDB string
}

func main() {
	rootCommand := &cobra.Command{
		Use:           "zb",
		Short:         "zombiezen build",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	g := &globalConfig{
		cacheDB: filepath.Join(xdgdir.Cache.Path(), "zb", "cache.db"),
	}
	rootCommand.PersistentFlags().StringVar(&g.cacheDB, "cache", g.cacheDB, "`path` to cache database")
	showDebug := rootCommand.PersistentFlags().Bool("debug", false, "show debugging output")
	rootCommand.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		initLogging(*showDebug)
		return nil
	}

	rootCommand.AddCommand(
		newBuildCommand(g),
		newEvalCommand(g),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), sigterm.Signals()...)
	err := rootCommand.ExecuteContext(ctx)
	cancel()
	if err != nil {
		initLogging(*showDebug)
		log.Errorf(context.Background(), "%v", err)
		os.Exit(1)
	}
}

type evalOptions struct {
	expr         string
	file         string
	installables []string
}

func newEvalCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "eval [options] [INSTALLABLE [...]]",
		Short:                 "evaluate a Lua expression",
		DisableFlagsInUseLine: true,
		Args:                  cobra.ArbitraryArgs,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	opts := new(evalOptions)
	c.Flags().StringVar(&opts.expr, "expr", "", "interpret installables as attribute paths relative to the Lua expression `expr`")
	c.Flags().StringVar(&opts.file, "file", "", "interpret installables as attribute paths relative to the Lua expression stored in `path`")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		opts.installables = args
		return runEval(cmd.Context(), g, opts)
	}
	return c
}

func runEval(ctx context.Context, g *globalConfig, opts *evalOptions) error {
	eval, err := zb.NewEval(nix.DefaultStoreDirectory, g.cacheDB)
	if err != nil {
		return err
	}
	defer func() {
		if err := eval.Close(); err != nil {
			log.Errorf(ctx, "%v", err)
		}
	}()

	var results []any
	switch {
	case opts.expr != "" && opts.file != "":
		return fmt.Errorf("can specify at most one of --expr or --file")
	case opts.expr != "":
		results, err = eval.Expression(opts.expr, opts.installables)
	case opts.file != "":
		results, err = eval.File(opts.file, opts.installables)
	default:
		return fmt.Errorf("installables not supported yet")
	}
	if err != nil {
		return err
	}

	for _, result := range results {
		fmt.Println(result)
	}

	return nil
}

type buildOptions struct {
	evalOptions
	outLink string
}

func newBuildCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "build [options] [INSTALLABLE [...]]",
		Short:                 "build one or more derivations",
		DisableFlagsInUseLine: true,
		Args:                  cobra.ArbitraryArgs,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	opts := new(buildOptions)
	c.Flags().StringVar(&opts.expr, "expr", "", "interpret installables as attribute paths relative to the Lua expression `expr`")
	c.Flags().StringVar(&opts.file, "file", "", "interpret installables as attribute paths relative to the Lua expression stored in `path`")
	c.Flags().StringVarP(&opts.outLink, "out-link", "o", "result", "change the name of the output path symlink to `path`")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		opts.installables = args
		return runBuild(cmd.Context(), g, opts)
	}
	return c
}

func runBuild(ctx context.Context, g *globalConfig, opts *buildOptions) error {
	eval, err := zb.NewEval(nix.DefaultStoreDirectory, g.cacheDB)
	if err != nil {
		return err
	}
	defer func() {
		if err := eval.Close(); err != nil {
			log.Errorf(ctx, "%v", err)
		}
	}()

	var results []any
	switch {
	case opts.expr != "" && opts.file != "":
		return fmt.Errorf("can specify at most one of --expr or --file")
	case opts.expr != "":
		results, err = eval.Expression(opts.expr, opts.installables)
	case opts.file != "":
		results, err = eval.File(opts.file, opts.installables)
	default:
		return fmt.Errorf("installables not supported yet")
	}
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return fmt.Errorf("no evaluation results")
	}

	args := []string{"--realise"}
	if opts.outLink != "" {
		args = append(args, "--add-root", opts.outLink)
	}
	args = append(args, "--")
	for _, result := range results {
		drv, _ := result.(*zb.Derivation)
		if drv == nil {
			return fmt.Errorf("%v is not a derivation", result)
		}
		p, err := drv.StorePath()
		if err != nil {
			return err
		}
		args = append(args, string(p))
	}

	stdout := new(strings.Builder)
	c := exec.CommandContext(ctx, "nix-store", args...)
	if opts.outLink == "" {
		c.Stdout = os.Stdout
	} else {
		c.Stdout = stdout
	}
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("nix-store --realise: %v", err)
	}
	if opts.outLink != "" {
		outLinks := strings.FieldsFunc(stdout.String(), func(c rune) bool {
			return c == '\n'
		})
		for _, out := range outLinks {
			target, err := os.Readlink(out)
			if err != nil {
				fmt.Println(out)
			} else {
				fmt.Println(target)
			}
		}
	}
	return nil
}

var initLogOnce sync.Once

func initLogging(showDebug bool) {
	initLogOnce.Do(func() {
		minLogLevel := log.Info
		if showDebug {
			minLogLevel = log.Debug
		}
		log.SetDefault(&log.LevelFilter{
			Min:    minLogLevel,
			Output: log.New(os.Stderr, "zb: ", log.StdFlags, nil),
		})
	})
}
