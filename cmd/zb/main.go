// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"zb.256lights.llc/pkg/internal/frontend"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/luac"
	"zb.256lights.llc/pkg/internal/lualex"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
)

type globalConfig struct {
	storeDir    zbstore.Directory
	storeSocket string
	cacheDB     string
}

func (g *globalConfig) storeClient(receiver zbstore.NARReceiver) (_ *jsonrpc.Client, wait func()) {
	var wg sync.WaitGroup
	c := jsonrpc.NewClient(func(ctx context.Context) (jsonrpc.ClientCodec, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "unix", g.storeSocket)
		if err != nil {
			return nil, err
		}
		return zbstorerpc.NewCodec(conn, receiver), nil
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

	ignoreSIGPIPE()
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
	keepFailed   bool
}

func (opts *evalOptions) newEval(g *globalConfig, storeClient *jsonrpc.Client) (*frontend.Eval, error) {
	return frontend.NewEval(&frontend.Options{
		Store: &rpcStore{
			client:     storeClient,
			keepFailed: opts.keepFailed,
		},
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
	c.Flags().BoolVarP(&opts.keepFailed, "keep-failed", "k", false, "keep temporary directories of failed builds")
	addEnvAllowListFlag(c.Flags(), &opts.allowEnv)
	c.RunE = func(cmd *cobra.Command, args []string) error {
		opts.installables = args
		return runEval(cmd.Context(), g, opts)
	}
	return c
}

func runEval(ctx context.Context, g *globalConfig, opts *evalOptions) error {
	storeClient, waitStoreClient := g.storeClient(nil)
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
	c.Flags().BoolVarP(&opts.keepFailed, "keep-failed", "k", false, "keep temporary directories of failed builds")
	addEnvAllowListFlag(c.Flags(), &opts.allowEnv)
	c.Flags().StringVarP(&opts.outLink, "out-link", "o", "result", "change the name of the output path symlink to `path`")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		opts.installables = args
		return runBuild(cmd.Context(), g, opts)
	}
	return c
}

func runBuild(ctx context.Context, g *globalConfig, opts *buildOptions) error {
	storeClient, waitStoreClient := g.storeClient(nil)
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

	drvPaths := make([]zbstore.Path, 0, len(results))
	for _, result := range results {
		drv, _ := result.(*frontend.Derivation)
		if drv == nil {
			return fmt.Errorf("%v is not a derivation", result)
		}
		drvPaths = append(drvPaths, drv.Path)
	}
	realizeResponse := new(zbstorerpc.RealizeResponse)
	err = jsonrpc.Do(ctx, storeClient, zbstorerpc.RealizeMethod, realizeResponse, &zbstorerpc.RealizeRequest{
		DrvPaths:   drvPaths,
		KeepFailed: opts.keepFailed,
	})
	if err != nil {
		return err
	}
	build, _, buildError := waitForBuild(ctx, storeClient, realizeResponse.BuildID)
	if build != nil {
		for _, drvPath := range drvPaths {
			result, err := build.ResultForPath(drvPath)
			if err != nil {
				continue
			}
			for _, output := range result.Outputs {
				if output.Path.Valid {
					fmt.Println(output.Path.X)
				}
			}
		}
	}
	return buildError
}

// rpcStore is an implementation of [frontend.Store]
// that communicates with a store over RPC.
// It copies builder logs to stderr
// and propagates options from [evalOptions].
type rpcStore struct {
	client     *jsonrpc.Client
	keepFailed bool
}

func (store *rpcStore) Exists(ctx context.Context, path string) (bool, error) {
	var response bool
	err := jsonrpc.Do(ctx, store.client, zbstorerpc.ExistsMethod, &response, &zbstorerpc.ExistsRequest{
		Path: path,
	})
	if err != nil {
		return false, err
	}
	return response, nil
}

func (store *rpcStore) Import(ctx context.Context, r io.Reader) error {
	generic, releaseConn, err := store.client.Codec(ctx)
	if err != nil {
		return err
	}
	defer releaseConn()
	codec, ok := generic.(*zbstorerpc.Codec)
	if !ok {
		return fmt.Errorf("store connection is %T (want %T)", generic, (*zbstorerpc.Codec)(nil))
	}
	return codec.Export(r)
}

func (store *rpcStore) Realize(ctx context.Context, want sets.Set[zbstore.OutputReference]) ([]*zbstorerpc.BuildResult, error) {
	var realizeResponse zbstorerpc.RealizeResponse
	err := jsonrpc.Do(ctx, store.client, zbstorerpc.RealizeMethod, &realizeResponse, &zbstorerpc.RealizeRequest{
		DrvPaths: slices.Collect(func(yield func(zbstore.Path) bool) {
			for ref := range want.All() {
				if !yield(ref.DrvPath) {
					return
				}
			}
		}),
		KeepFailed: store.keepFailed,
	})
	if err != nil {
		return nil, err
	}
	build, _, err := waitForBuild(ctx, store.client, realizeResponse.BuildID)
	if err != nil {
		return nil, err
	}
	return build.Results, nil
}

// waitForBuild polls the store until the given build is no longer active,
// returning the last response that it received.
// The second return value is the raw JSON of the build response.
// If the build was not successful,
// the build response is returned along with a non-nil error.
// waitForBuild will also copy build logs to stderr.
func waitForBuild(ctx context.Context, storeClient *jsonrpc.Client, buildID string) (_ *zbstorerpc.GetBuildResponse, _ json.RawMessage, err error) {
	defer func() {
		if err != nil && ctx.Err() != nil {
			log.Debugf(ctx, "Context canceled while waiting for build %s. Canceling build...", buildID)
			cancelCtx, cleanupCtx := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cleanupCtx()
			cancelError := jsonrpc.Notify(cancelCtx, storeClient, zbstorerpc.CancelBuildMethod, &zbstorerpc.CancelBuildNotification{
				BuildID: buildID,
			})
			if cancelError != nil {
				log.Warnf(ctx, "Failed to cancel build %s: %v", buildID, cancelError)
			}
		}
	}()

	paramsJSON, err := json.Marshal(&zbstorerpc.GetBuildRequest{
		BuildID: buildID,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("wait for build %s: build request: %v", buildID, err)
	}

	visited := make(sets.Set[zbstore.Path])
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		log.Debugf(ctx, "Polling build %s...", buildID)
		buildRPCResponse, err := storeClient.JSONRPC(ctx, &jsonrpc.Request{
			Method: zbstorerpc.GetBuildMethod,
			Params: paramsJSON,
		})
		if err != nil {
			// TODO(maybe): Are some errors retryable?
			return nil, nil, fmt.Errorf("wait for build %s: %w", buildID, err)
		}
		buildResponse := new(zbstorerpc.GetBuildResponse)
		if err := json.Unmarshal(buildRPCResponse.Result, buildResponse); err != nil {
			return nil, nil, fmt.Errorf("wait for build %s: %v", buildID, err)
		}
		log.Debugf(ctx, "Build %s is currently in status %q", buildID, buildResponse.Status)
		if buildResponse.Status == zbstorerpc.BuildUnknown {
			return nil, nil, fmt.Errorf("wait for build %s: not found in store", buildID)
		}

		for _, result := range buildResponse.Results {
			if visited.Has(result.DrvPath) {
				continue
			}
			visited.Add(result.DrvPath)
			log.Debugf(ctx, "Found new log in build %s for %s", buildID, result.DrvPath)

			// The overall build response might be successful
			// even if log-reading was interrupted.
			// Don't prevent errors in log-reading from failing the overall operation.
			if err := ctx.Err(); err != nil {
				log.Debugf(ctx, "Context canceled while reading logs for build %s: %v", buildID, err)
				break
			}
			if err := copyLogToStderr(ctx, storeClient, buildID, result.DrvPath); err != nil {
				log.Warnf(ctx, "Failed to read logs for %s in build %s: %v", result.DrvPath, buildID, err)
			}
		}

		switch buildResponse.Status {
		case zbstorerpc.BuildActive:
			// Poll again after a brief delay.
			log.Debugf(ctx, "Waiting to poll build %s again...", buildID)
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return nil, nil, fmt.Errorf("wait for build %s: %w", buildID, ctx.Err())
			}
		case zbstorerpc.BuildSuccess:
			return buildResponse, buildRPCResponse.Result, nil
		case zbstorerpc.BuildFail:
			return buildResponse, buildRPCResponse.Result, fmt.Errorf("build %s failed", buildID)
		case zbstorerpc.BuildError:
			return buildResponse, buildRPCResponse.Result, fmt.Errorf("build %s encountered an internal error", buildID)
		default:
			return buildResponse, buildRPCResponse.Result, fmt.Errorf("build %s finished with status %q", buildID, buildResponse.Status)
		}
	}
}

func copyLogToStderr(ctx context.Context, storeClient *jsonrpc.Client, buildID string, drvPath zbstore.Path) error {
	off := int64(0)
	for {
		response := new(zbstorerpc.ReadLogResponse)
		err := jsonrpc.Do(ctx, storeClient, zbstorerpc.ReadLogMethod, response, &zbstorerpc.ReadLogRequest{
			BuildID:    buildID,
			DrvPath:    drvPath,
			RangeStart: off,
		})
		if err != nil {
			return fmt.Errorf("read log for %s in build %s: %w", drvPath, buildID, err)
		}

		payload := response.Payload()
		if len(payload) > 0 && off == 0 {
			// Write header.
			oldPayload := payload
			payload = nil
			payload = append(payload, "--- "...)
			payload = append(payload, drvPath...)
			payload = append(payload, " ---\n"...)
			payload = append(payload, oldPayload...)
		}
		off += int64(len(payload))
		if _, err := os.Stderr.Write(payload); err != nil {
			return err
		}
		if response.EOF {
			return nil
		}
	}
}

// defaultVarDir returns "/zb/var/zb" on Unix-like systems or `C:\zb\var\zb` on Windows systems.
func defaultVarDir() string {
	return filepath.Join(filepath.Dir(string(zbstore.DefaultDirectory())), "var", "zb")
}

func addEnvAllowListFlag(fset *pflag.FlagSet, list *stringAllowList) {
	fset.Var(list.argFlag(true), "allow-env", "allow the given environment `var`iable to be accessed with os.getenv")
	all := fset.VarPF(list.allFlag(), "allow-all-env", "", "allow all environment variables to be accessed with os.getenv")
	all.NoOptDefVal = "true"
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
