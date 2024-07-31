// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/spf13/cobra"
	"go4.org/xdgdir"
	"zombiezen.com/go/bass/sigterm"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/zb"
	"zombiezen.com/go/zb/internal/jsonrpc"
	"zombiezen.com/go/zb/zbstore"
)

type globalConfig struct {
	storeDir    zbstore.Directory
	storeSocket string
	cacheDB     string
}

func (g *globalConfig) storeClient() *jsonrpc.Client {
	return jsonrpc.NewClient(func(ctx context.Context) (jsonrpc.ClientCodec, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "unix", g.storeSocket)
		if err != nil {
			return nil, err
		}
		return zbstore.NewClientCodec(conn), nil
	})
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
	var err error
	g.storeDir, err = zbstore.DirectoryFromEnvironment()
	if err != nil {
		initLogging(false)
		log.Errorf(context.Background(), "%v", err)
		os.Exit(1)
	}
	if runtime.GOOS == "windows" {
		g.storeSocket = `C:\zb\var\zb\server.sock`
	} else {
		g.storeSocket = "/zb/var/zb/server.sock"
	}

	rootCommand.PersistentFlags().StringVar(&g.cacheDB, "cache", g.cacheDB, "`path` to cache database")
	rootCommand.PersistentFlags().Var((*storeDirectoryFlag)(&g.storeDir), "store", "path to store `dir`ectory")
	rootCommand.PersistentFlags().StringVar(&g.storeSocket, "store-socket", g.storeSocket, "`path` to store server socket")
	showDebug := rootCommand.PersistentFlags().Bool("debug", false, "show debugging output")

	rootCommand.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		initLogging(*showDebug)
		return nil
	}

	rootCommand.AddCommand(
		newBuildCommand(g),
		newEvalCommand(g),
		newServeCommand(g),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), sigterm.Signals()...)
	err = rootCommand.ExecuteContext(ctx)
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
	storeClient := g.storeClient()
	defer storeClient.Close()
	eval, err := zb.NewEval(g.storeDir, storeClient, g.cacheDB)
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
	storeClient := g.storeClient()
	defer storeClient.Close()
	eval, err := zb.NewEval(g.storeDir, storeClient, g.cacheDB)
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

	// TODO(soon): Batch.
	for _, result := range results {
		drv, _ := result.(*zbstore.Derivation)
		if drv == nil {
			return fmt.Errorf("%v is not a derivation", result)
		}
		// TODO(someday): Evaluation should store the path of the exported result.
		p, _, err := drv.Export(nix.SHA256)
		if err != nil {
			return err
		}
		resp := new(zbstore.RealizeResponse)
		err = jsonrpc.Do(ctx, storeClient, zbstore.RealizeMethod, resp, &zbstore.RealizeRequest{
			DrvPath: p,
		})
		if err != nil {
			return err
		}
		for _, out := range resp.Outputs {
			fmt.Println(out.Path)
		}
	}

	return nil
}

type storeDirectoryFlag zbstore.Directory

func (f *storeDirectoryFlag) Type() string  { return "string" }
func (f storeDirectoryFlag) String() string { return string(f) }
func (f storeDirectoryFlag) Get() any       { return zbstore.Directory(f) }

func (f *storeDirectoryFlag) Set(s string) error {
	dir, err := zbstore.CleanDirectory(s)
	if err != nil {
		return err
	}
	*f = storeDirectoryFlag(dir)
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
