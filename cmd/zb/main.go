// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"sync"
	"time"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"zb.256lights.llc/pkg/bytebuffer"
	"zb.256lights.llc/pkg/internal/frontend"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/luac"
	"zb.256lights.llc/pkg/internal/lualex"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
)

func main() {
	rootCommand := &cobra.Command{
		Use:           "zb",
		Short:         "zb build tool",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	g := defaultGlobalConfig()
	if err := g.mergeEnvironment(); err != nil {
		initLogging(g.Debug)
		log.Errorf(context.Background(), "%v", err)
		os.Exit(1)
	}
	configFilePaths := iter.Seq[string](func(yield func(string) bool) {
		for dir := range systemConfigDirs() {
			if !yield(filepath.Join(dir, "zb", "config.json")) {
				return
			}
			if !yield(filepath.Join(dir, "zb", "config.jwcc")) {
				return
			}
		}
	})
	if err := g.mergeFiles(configFilePaths); err != nil {
		initLogging(g.Debug)
		log.Errorf(context.Background(), "%v", err)
		os.Exit(1)
	}

	ignoreSIGPIPE()
	ctx, cancel := signal.NotifyContext(context.Background(), interruptSignals...)

	rootCommand.PersistentFlags().StringVar(&g.CacheDB, "cache", g.CacheDB, "`path` to cache database")
	rootCommand.PersistentFlags().Var((*storeDirectoryFlag)(&g.Directory), "store", "path to store `dir`ectory")
	rootCommand.PersistentFlags().StringVar(&g.StoreSocket, "store-socket", g.StoreSocket, "`path` to store server socket")
	rootCommand.PersistentFlags().BoolVar(&g.Debug, "debug", g.Debug, "show debugging output")
	versionCobraFlag := rootCommand.PersistentFlags().VarPF(versionFlag{ctx}, "version", "", "show version information")
	versionCobraFlag.NoOptDefVal = "true"
	extraConfigs := rootCommand.PersistentFlags().StringArray("config", nil, "`path` to a configuration file to load (can be passed multiple times)")

	rootCommand.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// Load any command-line-passed configuration files before merging the rest.
		if err := g.mergeFiles(slices.Values(*extraConfigs)); err != nil {
			return err
		}
		initLogging(g.Debug)
		if err := g.validate(); err != nil {
			return err
		}
		return nil
	}

	luacCommand := luac.New()
	luacCommand.Hidden = true

	rootCommand.AddCommand(
		newBuildCommand(g),
		newDerivationCommand(g),
		newEvalCommand(g),
		newKeyCommand(),
		newNARCommand(),
		newServeCommand(g),
		newStoreCommand(g),
		newVersionCommand(g),
		luacCommand,
	)

	err := rootCommand.ExecuteContext(ctx)
	if errors.Is(err, errShowVersion) {
		err = runVersion(ctx)
	}
	cancel()
	if err != nil {
		initLogging(g.Debug)
		log.Errorf(context.Background(), "%v", err)
		os.Exit(1)
	}
}

type evalOptions struct {
	expression bool
	args       []string
	keepFailed bool
}

func (opts *evalOptions) newEval(g *globalConfig, storeClient *jsonrpc.Client, di *zbstorerpc.DeferredImporter) (*frontend.Eval, error) {
	store := &rpcStore{
		dir:        g.Directory,
		keepFailed: opts.keepFailed,
		Store: zbstorerpc.Store{
			Handler: storeClient,
		},
	}
	di.SetImporter(store)
	return frontend.NewEval(&frontend.Options{
		Store:          store,
		StoreDirectory: g.Directory,
		CacheDBPath:    g.CacheDB,
		LookupEnv: func(ctx context.Context, key string) (string, bool) {
			if !g.AllowEnv.Has(key) {
				log.Warnf(ctx, "os.getenv(%s) not permitted (use --allow-env=%s if this is intentional)", lualex.Quote(key), key)
				return "", false
			}
			return os.LookupEnv(key)
		},
		DownloadBufferCreator: bytebuffer.TempFileCreator{
			Pattern: "zb-download-*",
		},
	})
}

func newEvalCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "eval [options] [INSTALLABLE [...]]",
		Short:                 "evaluate a Lua expression",
		DisableFlagsInUseLine: true,
		Args: func(c *cobra.Command, args []string) error {
			if expr, _ := c.Flags().GetBool("expression"); expr {
				return cobra.ExactArgs(1)(c, args)
			}
			return cobra.MinimumNArgs(1)(c, args)
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	opts := new(evalOptions)
	c.Flags().BoolVarP(&opts.expression, "expression", "e", false, "interpret argument as Lua expression")
	c.Flags().BoolVarP(&opts.keepFailed, "keep-failed", "k", false, "keep temporary directories of failed builds")
	addEnvAllowListFlag(c.Flags(), &g.AllowEnv)
	c.RunE = func(cmd *cobra.Command, args []string) error {
		opts.args = args
		return runEval(cmd.Context(), g, opts)
	}
	return c
}

func runEval(ctx context.Context, g *globalConfig, opts *evalOptions) error {
	di := new(zbstorerpc.DeferredImporter)
	storeClient, waitStoreClient := g.storeClient(&zbstorerpc.CodecOptions{
		Importer: di,
	})
	defer func() {
		storeClient.Close()
		waitStoreClient()
	}()
	eval, err := opts.newEval(g, storeClient, di)
	if err != nil {
		return err
	}
	defer func() {
		if err := eval.Close(); err != nil {
			log.Errorf(ctx, "%v", err)
		}
	}()

	var results []any
	if opts.expression {
		results = make([]any, 1)
		results[0], err = eval.Expression(ctx, opts.args[0])
	} else {
		results, err = eval.URLs(ctx, opts.args)
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
		Use:                   "build [options] URL [...]",
		Short:                 "build one or more derivations",
		DisableFlagsInUseLine: true,
		Args: func(c *cobra.Command, args []string) error {
			if expr, _ := c.Flags().GetBool("expression"); expr {
				return cobra.ExactArgs(1)(c, args)
			}
			return cobra.MinimumNArgs(1)(c, args)
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	opts := new(buildOptions)
	c.Flags().BoolVarP(&opts.expression, "expression", "e", false, "interpret argument as a Lua expression")
	c.Flags().BoolVarP(&opts.keepFailed, "keep-failed", "k", false, "keep temporary directories of failed builds")
	addEnvAllowListFlag(c.Flags(), &g.AllowEnv)
	c.Flags().StringVarP(&opts.outLink, "out-link", "o", "result", "change the name of the output path symlink to `path`")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		opts.args = args
		return runBuild(cmd.Context(), g, opts)
	}
	return c
}

func runBuild(ctx context.Context, g *globalConfig, opts *buildOptions) error {
	di := new(zbstorerpc.DeferredImporter)
	storeClient, waitStoreClient := g.storeClient(&zbstorerpc.CodecOptions{
		Importer: di,
	})
	defer func() {
		storeClient.Close()
		waitStoreClient()
	}()
	eval, err := opts.newEval(g, storeClient, di)
	if err != nil {
		return err
	}
	defer func() {
		if err := eval.Close(); err != nil {
			log.Errorf(ctx, "%v", err)
		}
	}()

	var results []any
	if opts.expression {
		results = make([]any, 1)
		results[0], err = eval.Expression(ctx, opts.args[0])
	} else {
		results, err = eval.URLs(ctx, opts.args)
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
	zbstorerpc.Store
	dir        zbstore.Directory
	keepFailed bool
}

func (store *rpcStore) Realize(ctx context.Context, want sets.Set[zbstore.OutputReference]) ([]*zbstorerpc.BuildResult, error) {
	var realizeResponse zbstorerpc.RealizeResponse
	err := jsonrpc.Do(ctx, store.Handler, zbstorerpc.RealizeMethod, &realizeResponse, &zbstorerpc.RealizeRequest{
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
	build, _, err := waitForBuild(ctx, store.Handler, realizeResponse.BuildID)
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
func waitForBuild(ctx context.Context, storeClient jsonrpc.Handler, buildID string) (_ *zbstorerpc.Build, _ jsontext.Value, err error) {
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

	paramsJSON, err := jsonv2.Marshal(&zbstorerpc.GetBuildRequest{
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
		buildResponse := new(zbstorerpc.Build)
		if err := jsonv2.Unmarshal(buildRPCResponse.Result, buildResponse); err != nil {
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

func copyLogToStderr(ctx context.Context, storeClient jsonrpc.Handler, buildID string, drvPath zbstore.Path) error {
	off := int64(0)
	for {
		payload, err := readLog(ctx, storeClient, &zbstorerpc.ReadLogRequest{
			BuildID:    buildID,
			DrvPath:    drvPath,
			RangeStart: off,
		})
		if len(payload) > 0 {
			toWrite := payload
			if off == 0 {
				// Write header.
				toWrite = nil
				toWrite = append(toWrite, "--- "...)
				toWrite = append(toWrite, drvPath...)
				toWrite = append(toWrite, " ---\n"...)
				toWrite = append(toWrite, payload...)
			}
			if _, err := os.Stderr.Write(toWrite); err != nil {
				return err
			}
		}
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return err
		}
		off += int64(len(payload))
	}
}

func readLog(ctx context.Context, storeClient jsonrpc.Handler, req *zbstorerpc.ReadLogRequest) ([]byte, error) {
	response := new(zbstorerpc.ReadLogResponse)
	err := jsonrpc.Do(ctx, storeClient, zbstorerpc.ReadLogMethod, response, req)
	if err != nil {
		return nil, fmt.Errorf("read log for %s in build %s: %w", req.DrvPath, req.BuildID, err)
	}
	payload, err := response.Payload()
	if err != nil {
		return nil, fmt.Errorf("read log for %s in build %s: %v", req.DrvPath, req.BuildID, err)
	}
	if response.EOF {
		return payload, io.EOF
	}
	return payload, nil
}

// openInputFile opens a file for reading using [os.Open].
// If name is "-", then it returns [os.Stdin].
func openInputFile(name string) (fs.File, error) {
	if name == "-" {
		return stdinInputFile{}, nil
	}
	return os.Open(name)
}

type stdinInputFile struct{}

func (stdinInputFile) Read(p []byte) (int, error) { return os.Stdin.Read(p) }
func (stdinInputFile) Stat() (fs.FileInfo, error) { return os.Stdin.Stat() }
func (stdinInputFile) Close() error               { return nil }

func inputFileName(name string) string {
	if name == "-" {
		return "stdin"
	}
	return name
}

// openOutputFile opens a file for writing using [os.Create].
// If name is "-", then it returns [os.Stdout].
func openOutputFile(name string) (io.WriteCloser, error) {
	if name == "-" {
		return nopWriteCloser{os.Stdout}, nil
	}
	return os.Create(name)
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func outputFileName(name string) string {
	if name == "-" {
		return "stdout"
	}
	return name
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
