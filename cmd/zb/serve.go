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
	"path/filepath"
	"strings"
	"sync"

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
	dbPath       string
	buildDir     string
	sandbox      bool
	sandboxPaths map[string]string
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
	c.Flags().StringVar(&opts.dbPath, "db", opts.dbPath, "`path` to store database file")
	c.Flags().StringVar(&opts.buildDir, "build-root", os.TempDir(), "`dir`ectory to store temporary build artifacts")
	c.Flags().BoolVar(&opts.sandbox, "sandbox", opts.sandbox, "run builders in a restricted environment")
	c.Flags().Var(pathMapFlag(opts.sandboxPaths), "sandbox-path", "`path` to allow in sandbox (can be passed multiple times)")
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
	if err := osutil.MkdirAll(string(g.storeDir), 0o755, 0o755|os.ModeSticky); err != nil {
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
		BuildDir:       opts.buildDir,
		SandboxPaths:   opts.sandboxPaths,
		DisableSandbox: !opts.sandbox,
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
			peer := jsonrpc.NewClient(func(ctx context.Context) (jsonrpc.ClientCodec, error) {
				return codec, nil
			})
			jsonrpc.Serve(backend.WithPeer(ctx, peer), codec, srv)
			peer.Close()

			openConnsMu.Lock()
			openConns.Delete(conn)
			openConnsMu.Unlock()

			if err := conn.Close(); err != nil {
				log.Errorf(ctx, "%v", err)
			}
		}()
	}
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
