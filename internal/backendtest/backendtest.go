// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

// Package backendtest provides utilities for running a [backend.Server] for tests.
package backendtest

import (
	"context"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"zb.256lights.llc/pkg/internal/backend"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/zbstore"
)

// NewStoreDirectory returns the path to a newly created, empty [zbstore.Directory]
// that is cleaned up when the test finishes.
// If directory creation fails, NewStoreDirectory terminates the test by calling [testing.TB.Fatal].
// As such, NewStoreDirectory must be called from the goroutine running the test or benchmark function.
func NewStoreDirectory(tb testing.TB) zbstore.Directory {
	tb.Helper()
	realStoreDir, err := filepath.EvalSymlinks(tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}
	storeDir, err := zbstore.CleanDirectory(realStoreDir)
	if err != nil {
		tb.Fatal(err)
	}
	return storeDir
}

// TB is a subset of the [testing.TB] interface that can be safely called from any goroutine.
type TB interface {
	Logf(format string, args ...any)
	Fail()
	Cleanup(func())
}

// Options is the set of optional parameters to [NewServer].
type Options struct {
	backend.Options

	// TempDir is the directory to use to store intermediate build results
	// and the store database.
	// If empty, then a new directory is created and registered for cleanup.
	TempDir string

	ClientHandler  jsonrpc.Handler
	ClientReceiver zbstore.NARReceiver
}

// NewServer creates a new [backend.Server] suitable for testing
// and returns a client connected to it.
// The server and the client will be closed as part of test cleanup.
// If opts is nil, it is treated the same as if it was passed new(Options).
func NewServer(ctx context.Context, tb TB, storeDir zbstore.Directory, opts *Options) (*jsonrpc.Client, error) {
	if opts == nil {
		opts = new(Options)
	}
	tempDir := opts.TempDir
	if tempDir == "" {
		var err error
		tempDir, err = os.MkdirTemp("", "zb-backendtest-*")
		if err != nil {
			return nil, err
		}
		tb.Cleanup(func() {
			if err := os.RemoveAll(tempDir); err != nil {
				tb.Logf("%v", err)
			}
		})
	}

	buildDir := filepath.Join(tempDir, "build")
	if err := os.Mkdir(buildDir, 0o777); err != nil {
		return nil, err
	}

	var wg sync.WaitGroup
	opts2 := new(backend.Options)
	if opts != nil {
		*opts2 = opts.Options
	}
	opts2.BuildDir = buildDir
	opts2.DisableSandbox = true
	if opts2.CoresPerBuild < 1 {
		opts2.CoresPerBuild = 1
	}
	realStoreDir := opts2.RealDir
	if realStoreDir == "" {
		realStoreDir = string(storeDir)
	}
	srv := backend.NewServer(storeDir, filepath.Join(tempDir, "db.sqlite"), opts2)
	serverConn, clientConn := net.Pipe()

	serveCtx, stopServe := context.WithCancel(context.WithoutCancel(ctx))
	serverReceiver := srv.NewNARReceiver(serveCtx)
	serverCodec := zbstore.NewCodec(serverConn, serverReceiver)
	wg.Add(1)
	go func() {
		defer wg.Done()
		peer := jsonrpc.NewClient(func(ctx context.Context) (jsonrpc.ClientCodec, error) {
			return serverCodec, nil
		})
		jsonrpc.Serve(backend.WithPeer(serveCtx, peer), serverCodec, srv)
		peer.Close() // closes serverCodec implicitly
	}()

	clientCodec := zbstore.NewCodec(clientConn, opts.ClientReceiver)
	clientHandler := opts.ClientHandler
	if clientHandler == nil {
		clientHandler = jsonrpc.MethodNotFoundHandler{}
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		jsonrpc.Serve(serveCtx, clientCodec, clientHandler)
	}()
	client := jsonrpc.NewClient(func(ctx context.Context) (jsonrpc.ClientCodec, error) {
		return clientCodec, nil
	})

	tb.Cleanup(func() {
		if err := client.Close(); err != nil {
			tb.Logf("client.Close: %v", err)
			tb.Fail()
		}

		stopServe()
		wg.Wait()

		serverReceiver.Cleanup(context.WithoutCancel(ctx))
		if err := srv.Close(); err != nil {
			tb.Logf("srv.Close: %v", err)
			tb.Fail()
		}

		// Make entire store writable for deletion.
		filepath.WalkDir(string(realStoreDir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			perm := os.FileMode(0o666)
			if entry.IsDir() {
				perm = 0o777
			}
			if err := os.Chmod(path, perm); err != nil {
				tb.Logf("%v", err)
			}
			return nil
		})
	})

	return client, nil
}
