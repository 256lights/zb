// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/spf13/cobra"
	"zombiezen.com/go/log"
	"zombiezen.com/go/zb/internal/backend"
	"zombiezen.com/go/zb/internal/jsonrpc"
	"zombiezen.com/go/zb/zbstore"
)

type serveOptions struct {
	dbPath   string
	buildDir string
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
	opts := new(serveOptions)
	if runtime.GOOS == "windows" {
		opts.dbPath = `C:\zb\var\zb\db.sqlite`
	} else {
		opts.dbPath = "/zb/var/zb/db.sqlite"
	}
	c.Flags().StringVar(&opts.dbPath, "db", opts.dbPath, "`path` to store database file")
	c.Flags().StringVar(&opts.buildDir, "build-root", os.TempDir(), "`dir`ectory to store temporary build artifacts")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		return runServe(cmd.Context(), g, opts)
	}
	return c
}

func runServe(ctx context.Context, g *globalConfig, opts *serveOptions) error {
	if !g.storeDir.IsNative() {
		return fmt.Errorf("%s cannot be used on this system", g.storeDir)
	}
	if err := os.MkdirAll(filepath.Dir(string(g.storeDir)), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(string(g.storeDir), 0o755|os.ModeSticky); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(g.storeSocket), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(opts.dbPath), 0o755); err != nil {
		return err
	}
	// TODO(someday): Properly set permissions on the created database.

	laddr := &net.UnixAddr{
		Net:  "unix",
		Name: g.storeSocket,
	}
	l, err := net.ListenUnix(laddr.Net, laddr)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	openConns := make(map[*net.UnixConn]struct{})
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
		for conn := range openConns {
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
	srv := backend.NewServer(&backend.Options{
		Dir:      g.storeDir,
		BuildDir: opts.buildDir,
		DBPath:   opts.dbPath,
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
		openConns[conn] = struct{}{}
		openConnsMu.Unlock()

		wg.Add(1)
		go func() {
			defer wg.Done()
			recv := srv.NewNARReceiver()
			defer recv.Cleanup(ctx)
			jsonrpc.Serve(ctx, zbstore.NewServerCodec(conn, recv), srv)

			openConnsMu.Lock()
			delete(openConns, conn)
			openConnsMu.Unlock()

			if err := conn.Close(); err != nil {
				log.Errorf(ctx, "%v", err)
			}
		}()
	}
}
