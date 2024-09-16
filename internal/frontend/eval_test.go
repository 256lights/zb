// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package frontend

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"zb.256lights.llc/pkg/internal/backend"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/system"
	"zb.256lights.llc/pkg/internal/testcontext"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log/testlog"
)

func TestImportFromDerivation(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()

	realStoreDir := t.TempDir()
	storeDir, err := zbstore.CleanDirectory(realStoreDir)
	if err != nil {
		t.Fatal(err)
	}
	store := newTestServer(t, storeDir, realStoreDir, jsonrpc.MethodNotFoundHandler{}, nil)
	eval, err := NewEval(storeDir, store, filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := eval.Close(); err != nil {
			t.Error("eval.Close:", err)
		}
	}()

	results, err := eval.File(ctx, filepath.Join("testdata", "ifd.lua"), []string{
		`["` + system.Current().String() + `"]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Error("No returned values")
	}
	const want = "Hello, World!"
	if results[0] != want {
		t.Errorf("result = %#v; want %#v", results[0], want)
	}
}

// newTestServer creates a new [Server] suitable for testing
// and returns a client connected to it.
// newTestServer must be called from the goroutine running the test or benchmark.
// The server and the client will be closed as part of test cleanup.
func newTestServer(tb testing.TB, storeDir zbstore.Directory, realStoreDir string, clientHandler jsonrpc.Handler, clientReceiver zbstore.NARReceiver) *jsonrpc.Client {
	tb.Helper()
	helperDir := tb.TempDir()
	buildDir := filepath.Join(helperDir, "build")
	if err := os.Mkdir(buildDir, 0o777); err != nil {
		tb.Fatal(err)
	}

	var wg sync.WaitGroup
	srv := backend.NewServer(storeDir, filepath.Join(helperDir, "db.sqlite"), &backend.Options{
		RealDir:  realStoreDir,
		BuildDir: buildDir,
	})
	serverConn, clientConn := net.Pipe()

	ctx, cancel := context.WithCancel(testlog.WithTB(context.Background(), tb))
	serverReceiver := srv.NewNARReceiver(ctx)
	serverCodec := zbstore.NewCodec(serverConn, serverReceiver)
	wg.Add(1)
	go func() {
		defer wg.Done()
		peer := jsonrpc.NewClient(func(ctx context.Context) (jsonrpc.ClientCodec, error) {
			return serverCodec, nil
		})
		jsonrpc.Serve(backend.WithPeer(ctx, peer), serverCodec, srv)
		peer.Close() // closes serverCodec implicitly
	}()

	clientCodec := zbstore.NewCodec(clientConn, clientReceiver)
	wg.Add(1)
	go func() {
		defer wg.Done()
		jsonrpc.Serve(ctx, clientCodec, clientHandler)
	}()
	client := jsonrpc.NewClient(func(ctx context.Context) (jsonrpc.ClientCodec, error) {
		return clientCodec, nil
	})

	tb.Cleanup(func() {
		if err := client.Close(); err != nil {
			tb.Error("client.Close:", err)
		}

		cancel()
		wg.Wait()

		serverReceiver.Cleanup(testlog.WithTB(context.Background(), tb))
		if err := srv.Close(); err != nil {
			tb.Error("srv.Close:", err)
		}
	})

	return client
}

func TestMain(m *testing.M) {
	testlog.Main(nil)
	os.Exit(m.Run())
}
