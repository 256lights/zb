// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"zb.256lights.llc/pkg/internal/backend"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/osutil"
	"zb.256lights.llc/pkg/internal/system"
	"zb.256lights.llc/pkg/internal/xmaps"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
)

type serveOptions struct {
	dbPath            string
	buildDir          string
	buildUsersGroup   string
	sandbox           bool
	sandboxPaths      map[string]string
	allowKeepFailed   bool
	coresPerBuild     int
	buildLogRetention time.Duration
}

func newServeCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "serve [options]",
		Short:                 "run a build server",
		DisableFlagsInUseLine: true,
		Args:                  cobra.NoArgs,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	opts := &serveOptions{
		dbPath:       filepath.Join(defaultVarDir(), "db.sqlite"),
		sandbox:      backend.SystemSupportsSandbox(),
		sandboxPaths: make(map[string]string),
	}
	if osutil.IsRoot() {
		opts.buildUsersGroup = backend.DefaultBuildUsersGroup
	}
	c.Flags().StringVar(&opts.dbPath, "db", opts.dbPath, "`path` to store database file")
	c.Flags().StringVar(&opts.buildDir, "build-root", os.TempDir(), "`dir`ectory to store temporary build artifacts")
	c.Flags().StringVar(&opts.buildUsersGroup, "build-users-group", opts.buildUsersGroup, "name of Unix `group` of users to run builds as")
	c.Flags().BoolVar(&opts.sandbox, "sandbox", opts.sandbox, "run builders in a restricted environment")
	c.Flags().Var(pathMapFlag(opts.sandboxPaths), "sandbox-path", "`path` to allow in sandbox (can be passed multiple times)")
	c.Flags().BoolVar(&opts.allowKeepFailed, "allow-keep-failed", true, "allow user to skip cleanup of failed builds")
	c.Flags().IntVar(&opts.coresPerBuild, "cores-per-build", runtime.NumCPU(), "hint to builders for `number` of concurrent jobs to run")
	c.Flags().DurationVar(&opts.buildLogRetention, "build-log-retention", 7*24*time.Hour, "`duration` before deleting finished build logs")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		return runServe(cmd.Context(), g, opts)
	}
	return c
}

func runServe(ctx context.Context, g *globalConfig, opts *serveOptions) error {
	if !g.storeDir.IsNative() {
		return fmt.Errorf("%s cannot be used on this system", g.storeDir)
	}
	if opts.sandbox && !backend.CanSandbox() {
		if !backend.SystemSupportsSandbox() {
			return fmt.Errorf("sandboxing requested but not supported on %v", system.Current())
		}
		return fmt.Errorf("sandboxing requested but unable to use (are you running with admin privileges?)")
	}
	storeDirGroupID, buildUsers, err := buildUsersForGroup(ctx, opts.buildUsersGroup)
	if err != nil {
		return err
	}
	if err := ensureStoreDirectory(string(g.storeDir), storeDirGroupID); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(g.storeSocket), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(opts.dbPath), 0o755); err != nil {
		return err
	}
	// TODO(someday): Properly set permissions on the created database.

	l, err := listenUnix(g.storeSocket)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	openConns := make(sets.Set[*net.UnixConn])
	var openConnsMu sync.Mutex
	wg.Add(1)
	go func() {
		defer wg.Done()

		// Once the context is Done, refuse new connections and RPCs.
		<-ctx.Done()
		log.Infof(ctx, "Shutting down (signal received)...")

		if err := l.Close(); err != nil {
			log.Errorf(ctx, "Closing Unix socket: %v", err)
		}
		openConnsMu.Lock()
		for conn := range openConns.All() {
			if err := conn.CloseRead(); err != nil {
				log.Errorf(ctx, "Closing Unix socket: %v", err)
			}
		}
		openConnsMu.Unlock()
	}()
	defer func() {
		cancel()
		wg.Wait()

		if err := os.Remove(g.storeSocket); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Warnf(ctx, "Failed to clean up socket: %v", err)
		}
	}()

	log.Infof(ctx, "Listening on %s", g.storeSocket)
	srv := backend.NewServer(g.storeDir, opts.dbPath, &backend.Options{
		BuildDir:          opts.buildDir,
		SandboxPaths:      opts.sandboxPaths,
		DisableSandbox:    !opts.sandbox,
		BuildUsers:        buildUsers,
		AllowKeepFailed:   opts.allowKeepFailed,
		CoresPerBuild:     opts.coresPerBuild,
		BuildLogRetention: opts.buildLogRetention,
	})
	defer func() {
		if err := srv.Close(); err != nil {
			log.Errorf(ctx, "%v", err)
		}
	}()

	for {
		conn, err := l.AcceptUnix()
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		if err != nil {
			return err
		}
		openConnsMu.Lock()
		openConns.Add(conn)
		openConnsMu.Unlock()

		wg.Add(1)
		go func() {
			defer wg.Done()
			recv := srv.NewNARReceiver(ctx)
			defer recv.Cleanup(ctx)

			codec := zbstore.NewCodec(nopCloser{conn}, recv)
			jsonrpc.Serve(backend.WithExporter(ctx, codec), codec, srv)
			codec.Close()

			openConnsMu.Lock()
			openConns.Delete(conn)
			openConnsMu.Unlock()

			if err := conn.Close(); err != nil {
				log.Errorf(ctx, "%v", err)
			}
		}()
	}
}

func ensureStoreDirectory(path string, gid int) error {
	if err := os.MkdirAll(filepath.Dir(string(path)), 0o755); err != nil {
		return err
	}
	const mode os.FileMode = 0o775 | os.ModeSticky
	if err := os.Mkdir(path, mode); err != nil {
		if errors.Is(err, os.ErrExist) {
			err = nil
		}
		return err
	}
	// Run an extra chmod to bypass umask.
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	if gid == -1 || gid == os.Getegid() {
		return nil
	}
	if err := os.Chown(path, -1, gid); err != nil {
		return err
	}
	return nil
}

func buildUsersForGroup(ctx context.Context, name string) (gid int, buildUsers []backend.BuildUser, err error) {
	if name == "" {
		return -1, nil, nil
	}
	if runtime.GOOS == "windows" {
		return -1, nil, fmt.Errorf("cannot set --build-users-group on Windows")
	}
	g, userNames, err := osutil.LookupGroup(ctx, name)
	if err != nil {
		return -1, nil, err
	}
	gid, err = strconv.Atoi(g.Gid)
	if err != nil {
		return -1, nil, fmt.Errorf("build users group id: %v", err)
	}
	log.Debugf(ctx, "Using build group %s (gid=%d), users=%v", name, gid, userNames)
	for _, userName := range userNames {
		u, err := user.Lookup(userName)
		if err != nil {
			return gid, nil, fmt.Errorf("build users group: %v", err)
		}
		var buildUser backend.BuildUser
		buildUser.UID, err = strconv.Atoi(u.Uid)
		if err != nil {
			return gid, nil, fmt.Errorf("build users group: user %s: user id: %v", userName, err)
		}
		buildUser.GID, err = strconv.Atoi(u.Gid)
		if err != nil {
			return gid, nil, fmt.Errorf("build users group: user %s: group id: %v", userName, err)
		}
		buildUsers = append(buildUsers, buildUser)
	}
	return gid, buildUsers, nil
}

func listenUnix(path string) (*net.UnixListener, error) {
	laddr := &net.UnixAddr{
		Net:  "unix",
		Name: path,
	}
	l, err := net.ListenUnix(laddr.Net, laddr)
	if err != nil {
		return nil, err
	}

	// TODO(soon): Restrict to a group.
	if err := os.Chmod(path, 0o777); err != nil {
		l.Close()
		return nil, err
	}

	return l, nil
}

type pathMapFlag map[string]string

func (f pathMapFlag) Type() string {
	return "string"
}

func (f pathMapFlag) String() string {
	sb := new(strings.Builder)
	first := true
	for k, v := range xmaps.Sorted(f) {
		if first {
			first = false
		} else {
			sb.WriteString(" ")
		}
		sb.WriteString(k)
		if k != v || strings.Contains(k, "=") || strings.Contains(v, "=") {
			sb.WriteString("=")
			sb.WriteString(v)
		}
	}
	return sb.String()
}

func (f pathMapFlag) Get() any {
	return map[string]string(f)
}

func (f pathMapFlag) Set(s string) error {
	for _, word := range strings.Fields(s) {
		k, v, isMap := strings.Cut(word, "=")
		if !isMap {
			v = k
		}
		f[k] = v
	}
	return nil
}

type nopCloser struct {
	io.ReadWriter
}

func (nopCloser) Close() error {
	return nil
}
