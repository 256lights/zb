// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/alecthomas/kong"
	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	kongcompletion "github.com/jotaen/kong-completion"
	"zb.256lights.llc/pkg/bytebuffer"
	"zb.256lights.llc/pkg/internal/backend"
	"zb.256lights.llc/pkg/internal/frontend"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/luac"
	"zb.256lights.llc/pkg/internal/lualex"
	"zb.256lights.llc/pkg/internal/osutil"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
)

type zbCommand struct {
	Config       globalConfig `kong:"embed"`
	ExtraConfigs []string     `kong:"name=config,sep=none,placeholder=path,help=Load configuration file(s). (Can be passed multiple times.)"`

	Build      buildCommand      `kong:"cmd"`
	Eval       evalCommand       `kong:"cmd"`
	Derivation derivationCommand `kong:"cmd"`
	Store      storeCommand      `kong:"cmd"`
	Key        keyCommand        `kong:"cmd"`
	Serve      serveCommand      `kong:"cmd"`
	NAR        narCommand        `kong:"cmd"`

	Completion kongcompletion.Completion `kong:"cmd"`

	Version     versionCommand `kong:"cmd"`
	VersionFlag versionFlag    `kong:"name=version,help=Show version information."`

	Luac luac.Command `kong:"cmd,hidden"`
}

func (c *zbCommand) ProvideConfig() *globalConfig {
	return &c.Config
}

func (c *zbCommand) BeforeApply(kc *kong.Context, p *kong.Path) error {
	configFlag := findFlagByName("config", slices.Values(p.Flags))
	configValue := kc.Value(&kong.Path{
		Parent: p.Node(),
		Flag:   configFlag,
	})
	if configValue.IsValid() {
		configFlag.Apply(configValue)
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
		for _, path := range c.ExtraConfigs {
			if !yield(path) {
				return
			}
		}
	})
	if err := c.Config.mergeFiles(configFilePaths); err != nil {
		return err
	}

	if err := c.Config.mergeEnvironment(); err != nil {
		return err
	}

	return nil
}

func zbKongOption() kong.Option {
	var defaultBuildUsersGroup string
	if osutil.IsRoot() {
		defaultBuildUsersGroup = backend.DefaultBuildUsersGroup
	}
	var options iter.Seq[kong.Option] = func(yield func(kong.Option) bool) {
		if !yield(kong.BindToProvider((*zbCommand).ProvideConfig)) {
			return
		}
		if !yield(kong.TypeMapper(reflect.TypeFor[sets.Set[string]](), kong.MapperFunc(mapStringSet))) {
			return
		}
		if !yield(kong.NamedMapper("pathmap", kong.MapperFunc(mapPathMap))) {
			return
		}
		if !yield(kong.NamedMapper("nativeStorePath", kong.MapperFunc(mapNativeStorePath))) {
			return
		}
		g := defaultGlobalConfig()
		vars := kong.Vars{
			"default_store_dir":         string(g.Directory),
			"default_store_socket":      g.StoreSocket,
			"cache_db":                  g.CacheDB,
			"http_cache":                g.HTTPCacheDB,
			"netrc":                     g.NetrcPath,
			"default_store_db":          filepath.Join(defaultVarDir(), "db.sqlite"),
			"build_users_group":         defaultBuildUsersGroup,
			"default_build_users_group": backend.DefaultBuildUsersGroup,
			"default_log_dir":           filepath.Join(filepath.Dir(string(zbstore.DefaultDirectory())), "var", "log", "zb"),
			"temp_dir":                  os.TempDir(),
			"num_cpu":                   strconv.Itoa(runtime.NumCPU()),
			"supports_sandbox":          strconv.FormatBool(backend.SystemSupportsSandbox()),
		}
		if !yield(vars) {
			return
		}
	}
	return kong.OptionFunc(func(k *kong.Kong) error {
		for opt := range options {
			if err := opt.Apply(k); err != nil {
				return err
			}
		}
		return nil
	})
}

func main() {
	c := new(zbCommand)
	k := kong.Must(c,
		kong.Name("zb"),
		kong.Description("zb build tool"),
		kong.ConfigureHelp(kong.HelpOptions{
			NoExpandSubcommands: true,
		}),
		kong.Bind(c),
		zbKongOption(),
	)
	kongcompletion.Register(k)

	kc, err := k.Parse(os.Args[1:])
	initLogging(c.Config.Debug)
	if err != nil && !c.VersionFlag {
		log.Errorf(context.Background(), "%v", err)
		os.Exit(1)
	}

	ignoreSIGPIPE()
	ctx, cancel := signal.NotifyContext(context.Background(), interruptSignals...)
	if c.VersionFlag {
		err = c.Version.Run(ctx, k)
	} else {
		kc.BindTo(ctx, (*context.Context)(nil))
		err = kc.Run()
	}
	cancel()
	if err != nil {
		log.Errorf(context.Background(), "%v", err)
		os.Exit(1)
	}
}

type evalOptions struct {
	Expression bool     `kong:"short=e,help=Interpret argument as Lua expression."`
	Args       []string `kong:"name=URL,arg"`
	KeepFailed bool     `kong:"short=k,help=Keep temporary directories of failed builds."`
	Clean      bool     `kong:"help=Ignore any previous realizations in the store."`

	AllowEnv    sets.Set[string] `kong:"xor=allow_env,placeholder=var,help=Allow the given environment variable to be accessed with os.getenv. (Can be passed multiple times.)"`
	AllowAllEnv *bool            `kong:"xor=allow_env,help=Allow all environment variables to be accessed with os.getenv."`
}

func (opts *evalOptions) AfterApply(g *globalConfig) error {
	if opts.AllowAllEnv != nil {
		g.AllowEnv = stringAllowList{all: *opts.AllowAllEnv}
	} else if opts.AllowEnv.Len() > 0 {
		g.AllowEnv = stringAllowList{set: opts.AllowEnv.Clone()}
	}
	return nil
}

func (opts *evalOptions) Validate() error {
	switch {
	case opts.Expression && len(opts.Args) != 1:
		return fmt.Errorf("accepts 1 arg, received %d", len(opts.Args))
	case !opts.Expression && len(opts.Args) == 0:
		return fmt.Errorf("requires at least 1 arg, only received %d", len(opts.Args))
	}
	return nil
}

func (opts *evalOptions) newEval(g *globalConfig, httpClient frontend.HTTPClient, storeClient *jsonrpc.Client, di *zbstorerpc.DeferredImporter) (*frontend.Eval, error) {
	store := &rpcStore{
		dir:        g.Directory,
		keepFailed: opts.KeepFailed,
		Store: zbstorerpc.Store{
			Handler: storeClient,
		},
		reuse: opts.reusePolicy(g),
	}
	di.SetImporter(store)
	return frontend.NewEval(&frontend.Options{
		Store:          store,
		StoreDirectory: g.Directory,
		CacheDBPath:    g.CacheDB,
		HTTPClient:     httpClient,
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

func (opts *evalOptions) reusePolicy(g *globalConfig) *zbstorerpc.ReusePolicy {
	if opts.Clean {
		return nil
	}
	return g.reusePolicy()
}

type evalCommand struct {
	evalOptions `kong:"embed"`
}

func (c *evalCommand) Signature() string {
	return `kong:"help=Evaluate a Lua expression."`
}

func (c *evalCommand) Run(ctx context.Context, g *globalConfig) error {
	httpClient, httpCloser, err := g.newHTTPClient()
	if err != nil {
		return err
	}
	defer func() {
		httpClient.CloseIdleConnections()
		if err := httpCloser.Close(); err != nil {
			log.Warnf(ctx, "%v", err)
		}
	}()
	di := new(zbstorerpc.DeferredImporter)
	storeClient := g.storeClient(&zbstorerpc.CodecOptions{
		Importer: di,
	})
	defer storeClient.Close()
	eval, err := c.newEval(g, httpClient, storeClient, di)
	if err != nil {
		return err
	}
	defer func() {
		if err := eval.Close(); err != nil {
			log.Errorf(ctx, "%v", err)
		}
	}()

	var results []any
	if c.Expression {
		results = make([]any, 1)
		results[0], err = eval.Expression(ctx, c.Args[0])
	} else {
		results, err = eval.URLs(ctx, c.Args)
	}
	if err != nil {
		return err
	}

	for _, result := range results {
		fmt.Println(result)
	}

	return nil
}

type buildCommand struct {
	evalOptions `kong:"embed"`
	OutLink     string `kong:"short=o,default=result,placeholder=path,help=Change the name of the output path symlink. (Default: ${default})"`
}

func (c *buildCommand) Signature() string {
	return `kong:"help=Build one or more derivations."`
}

func (c *buildCommand) Run(ctx context.Context, g *globalConfig) error {
	httpClient, httpCloser, err := g.newHTTPClient()
	if err != nil {
		return err
	}
	defer func() {
		httpClient.CloseIdleConnections()
		if err := httpCloser.Close(); err != nil {
			log.Warnf(ctx, "%v", err)
		}
	}()
	di := new(zbstorerpc.DeferredImporter)
	storeClient := g.storeClient(&zbstorerpc.CodecOptions{
		Importer: di,
	})
	defer storeClient.Close()
	eval, err := c.newEval(g, httpClient, storeClient, di)
	if err != nil {
		return err
	}
	defer func() {
		if err := eval.Close(); err != nil {
			log.Errorf(ctx, "%v", err)
		}
	}()

	var results []any
	if c.Expression {
		results = make([]any, 1)
		results[0], err = eval.Expression(ctx, c.Args[0])
	} else {
		results, err = eval.URLs(ctx, c.Args)
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
		KeepFailed: c.KeepFailed,
		Reuse:      c.reusePolicy(g),
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
	reuse      *zbstorerpc.ReusePolicy
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
		Reuse:      store.reuse,
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

// ReadFrom implements [io.ReaderFrom] by calling [io.Copy] on the underlying writer.
// This keeps [io.Copy] efficient in case the underlying writer implements [io.ReaderFrom].
func (nwc nopWriteCloser) ReadFrom(r io.Reader) (n int64, err error) {
	return io.Copy(nwc.Writer, r)
}

func (nwc nopWriteCloser) Close() error { return nil }

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
