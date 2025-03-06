// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"zb.256lights.llc/pkg/internal/frontend"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/luac"
	"zb.256lights.llc/pkg/internal/lualex"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
)

type globalConfig struct {
	storeDir    zbstore.Directory
	storeSocket string
	cacheDB     string
}

func (g *globalConfig) storeClient(localHandler jsonrpc.Handler, receiver zbstore.NARReceiver) (_ *jsonrpc.Client, wait func()) {
	var wg sync.WaitGroup
	c := jsonrpc.NewClient(func(ctx context.Context) (jsonrpc.ClientCodec, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "unix", g.storeSocket)
		if err != nil {
			return nil, err
		}
		codec := zbstore.NewCodec(conn, receiver)
		wg.Add(1)
		go func() {
			defer wg.Done()
			jsonrpc.Serve(context.Background(), codec, localHandler)
		}()
		return codec, nil
	})
	return c, wg.Wait
}

func main() {
	rootCommand := &cobra.Command{
		Use:           "zb",
		Short:         "zb build tool",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	g := &globalConfig{
		cacheDB:     filepath.Join(cacheDir(), "zb", "cache.db"),
		storeSocket: os.Getenv("ZB_STORE_SOCKET"),
	}
	var err error
	g.storeDir, err = zbstore.DirectoryFromEnvironment()
	if err != nil {
		initLogging(false)
		log.Errorf(context.Background(), "%v", err)
		os.Exit(1)
	}
	if g.storeSocket == "" {
		g.storeSocket = filepath.Join(defaultVarDir(), "server.sock")
	}

	rootCommand.PersistentFlags().StringVar(&g.cacheDB, "cache", g.cacheDB, "`path` to cache database")
	rootCommand.PersistentFlags().Var((*storeDirectoryFlag)(&g.storeDir), "store", "path to store `dir`ectory")
	rootCommand.PersistentFlags().StringVar(&g.storeSocket, "store-socket", g.storeSocket, "`path` to store server socket")
	showDebug := rootCommand.PersistentFlags().Bool("debug", false, "show debugging output")

	rootCommand.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		initLogging(*showDebug)
		return nil
	}

	luacCommand := luac.New()
	luacCommand.Hidden = true

	rootCommand.AddCommand(
		newBuildCommand(g),
		newDerivationCommand(g),
		newEvalCommand(g),
		newNARCommand(),
		newServeCommand(g),
		newStoreCommand(g),
		luacCommand,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), interruptSignals...)
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
	allowEnv     stringAllowList
}

func (opts *evalOptions) newEval(g *globalConfig, storeClient *jsonrpc.Client) (*frontend.Eval, error) {
	return frontend.NewEval(&frontend.Options{
		Store:          storeClient,
		StoreDirectory: g.storeDir,
		CacheDBPath:    g.cacheDB,
		LookupEnv: func(ctx context.Context, key string) (string, bool) {
			if !opts.allowEnv.Has(key) {
				log.Warnf(ctx, "os.getenv(%s) not permitted (use --allow-env=%s if this is intentional)", lualex.Quote(key), key)
				return "", false
			}
			return os.LookupEnv(key)
		},
	})
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
	addEnvAllowListFlag(c.Flags(), &opts.allowEnv)
	c.RunE = func(cmd *cobra.Command, args []string) error {
		opts.installables = args
		return runEval(cmd.Context(), g, opts)
	}
	return c
}

func runEval(ctx context.Context, g *globalConfig, opts *evalOptions) error {
	storeClient, waitStoreClient := g.storeClient(jsonrpc.MethodNotFoundHandler{}, nil)
	defer func() {
		storeClient.Close()
		waitStoreClient()
	}()
	eval, err := opts.newEval(g, storeClient)
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
		results, err = eval.Expression(ctx, opts.expr, opts.installables)
	case opts.file != "":
		results, err = eval.File(ctx, opts.file, opts.installables)
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
	addEnvAllowListFlag(c.Flags(), &opts.allowEnv)
	c.Flags().StringVarP(&opts.outLink, "out-link", "o", "result", "change the name of the output path symlink to `path`")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		opts.installables = args
		return runBuild(cmd.Context(), g, opts)
	}
	return c
}

func runBuild(ctx context.Context, g *globalConfig, opts *buildOptions) error {
	storeClient, waitStoreClient := g.storeClient(new(clientRPCHandler), nil)
	defer func() {
		storeClient.Close()
		waitStoreClient()
	}()
	eval, err := opts.newEval(g, storeClient)
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
		results, err = eval.Expression(ctx, opts.expr, opts.installables)
	case opts.file != "":
		results, err = eval.File(ctx, opts.file, opts.installables)
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
	numBuilt := 0
	for _, result := range results {
		drv, _ := result.(*frontend.Derivation)
		if drv == nil {
			return fmt.Errorf("%v is not a derivation", result)
		}
		resp := new(zbstore.RealizeResponse)
		err = jsonrpc.Do(ctx, storeClient, zbstore.RealizeMethod, resp, &zbstore.RealizeRequest{
			DrvPath: drv.Path,
		})
		if err != nil {
			return err
		}
		hasValid := false
		for _, out := range resp.Outputs {
			if out.Path.Valid {
				hasValid = true
				fmt.Println(out.Path.X)
			}
		}
		if hasValid {
			numBuilt++
		}
	}

	if numBuilt < len(results) {
		return errors.New("not all derivations built")
	}
	return nil
}

type clientRPCHandler struct {
	mu          sync.Mutex
	prevDrvPath zbstore.Path
}

func (h *clientRPCHandler) JSONRPC(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	return jsonrpc.ServeMux{
		zbstore.LogMethod: jsonrpc.HandlerFunc(h.log),
	}.JSONRPC(ctx, req)
}

func (h *clientRPCHandler) log(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	args := new(zbstore.LogNotification)
	if err := json.Unmarshal(req.Params, args); err != nil {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
	}
	payload := args.Payload()
	if len(payload) == 0 {
		return nil, nil
	}

	h.mu.Lock()
	isNewDrvPath := args.DrvPath != h.prevDrvPath
	h.prevDrvPath = args.DrvPath
	h.mu.Unlock()

	if isNewDrvPath {
		oldPayload := payload
		payload = nil
		payload = append(payload, "--- "...)
		payload = append(payload, args.DrvPath...)
		payload = append(payload, " ---\n"...)
		payload = append(payload, oldPayload...)
	}
	os.Stderr.Write(payload)
	return nil, nil
}

// defaultVarDir returns "/zb/var/zb" on Unix-like systems or `C:\zb\var\zb` on Windows systems.
func defaultVarDir() string {
	return filepath.Join(filepath.Dir(string(zbstore.DefaultDirectory())), "var", "zb")
}

func addEnvAllowListFlag(fset *pflag.FlagSet, list *stringAllowList) {
	fset.Var(list.argFlag(true), "allow-env", "allow the given environment `var`iable to be accessed with os.getenv")
	fset.Var(list.allFlag(), "allow-all-env", "allow all environment variables to be accessed with os.getenv")
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
