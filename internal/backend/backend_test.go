// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package backend

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/storetest"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log/testlog"
	"zombiezen.com/go/nix"
)

func TestImport(t *testing.T) {
	runTest := func(t *testing.T, dir zbstore.Directory, realStoreDir string) {
		ctx := testlog.WithTB(context.Background(), t)

		const fileContent = "Hello, World!\n"
		exportBuffer := new(bytes.Buffer)
		exporter := zbstore.NewExporter(exportBuffer)
		storePath, err := storetest.ExportFlatFile(exporter, dir, "hello.txt", []byte(fileContent), nix.SHA256)
		if err != nil {
			t.Fatal(err)
		}
		if err := exporter.Close(); err != nil {
			t.Fatal(err)
		}

		client := newTestServer(t, dir, realStoreDir, jsonrpc.MethodNotFoundHandler{}, nil)

		codec, releaseCodec, err := storeCodec(ctx, client)
		if err != nil {
			t.Fatal(err)
		}
		err = codec.Export(exportBuffer)
		releaseCodec()
		if err != nil {
			t.Fatal(err)
		}

		// Call exists method.
		// Exports don't send a response, so this introduces a sync point.
		var exists bool
		err = jsonrpc.Do(ctx, client, zbstore.ExistsMethod, &exists, &zbstore.ExistsRequest{
			Path: string(storePath),
		})
		if err != nil {
			t.Error(err)
		}
		if !exists {
			t.Errorf("store reports exists=false for %s", storePath)
		}

		// Verify that store object exists on disk.
		realFilePath := filepath.Join(realStoreDir, storePath.Base())
		if got, err := os.ReadFile(realFilePath); err != nil {
			t.Error(err)
		} else if string(got) != fileContent {
			t.Errorf("%s content = %q; want %q", storePath, got, fileContent)
		}
		if info, err := os.Lstat(realFilePath); err != nil {
			t.Error(err)
		} else if got := info.Mode(); got&0o111 != 0 {
			t.Errorf("mode = %v; want non-executable", got)
		}
	}

	t.Run("ActualDir", func(t *testing.T) {
		realStoreDir := t.TempDir()
		storeDir, err := zbstore.CleanDirectory(realStoreDir)
		if err != nil {
			t.Fatal(err)
		}
		runTest(t, storeDir, realStoreDir)
	})

	t.Run("MappedDir", func(t *testing.T) {
		runTest(t, zbstore.DefaultDirectory(), t.TempDir())
	})
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
	srv := NewServer(storeDir, filepath.Join(helperDir, "db.sqlite"), &Options{
		RealDir:        realStoreDir,
		BuildDir:       buildDir,
		DisableSandbox: true,
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
		jsonrpc.Serve(WithPeer(ctx, peer), serverCodec, srv)
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

func storeCodec(ctx context.Context, client *jsonrpc.Client) (codec *zbstore.Codec, release func(), err error) {
	generic, release, err := client.Codec(ctx)
	if err != nil {
		return nil, nil, err
	}
	codec, ok := generic.(*zbstore.Codec)
	if !ok {
		release()
		return nil, nil, fmt.Errorf("store connection is %T (want %T)", generic, (*zbstore.Codec)(nil))
	}
	return codec, release, nil
}

func TestMain(m *testing.M) {
	testlog.Main(nil)
	os.Exit(m.Run())
}
