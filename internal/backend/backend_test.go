// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package backend

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
	"zombiezen.com/go/zb/internal/jsonrpc"
	"zombiezen.com/go/zb/zbstore"
)

func TestImport(t *testing.T) {
	runTest := func(t *testing.T, dir zbstore.Directory, realStoreDir string) {
		ctx := context.Background()

		const fileContent = "Hello, World!\n"
		exportBuffer := new(bytes.Buffer)
		exporter := zbstore.NewExporter(exportBuffer)
		if err := writeSingleFileNAR(exporter, strings.NewReader(fileContent), int64(len(fileContent))); err != nil {
			t.Fatal(err)
		}
		h := nix.NewHasher(nix.SHA256)
		h.WriteString(fileContent)
		ca := nix.FlatFileContentAddress(h.SumHash())
		storePath, err := zbstore.FixedCAOutputPath(dir, "hello.txt", ca, zbstore.References{})
		if err != nil {
			t.Fatal(err)
		}
		err = exporter.Trailer(&zbstore.ExportTrailer{
			StorePath:      storePath,
			ContentAddress: ca,
		})
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

	var wg sync.WaitGroup
	srv := NewServer(storeDir, filepath.Join(helperDir, "db.sqlite"), &Options{
		RealDir:  realStoreDir,
		BuildDir: filepath.Join(helperDir, "build"),
	})
	serverConn, clientConn := net.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	serverReceiver := srv.NewNARReceiver()
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

		serverReceiver.Cleanup(context.Background())
		if err := srv.Close(); err != nil {
			tb.Error("srv.Close:", err)
		}
	})

	return client
}

func writeSingleFileNAR(w io.Writer, r io.Reader, sz int64) error {
	nw := nar.NewWriter(w)
	if err := nw.WriteHeader(&nar.Header{Size: sz}); err != nil {
		return err
	}
	if _, err := io.Copy(nw, r); err != nil {
		return err
	}
	if err := nw.Close(); err != nil {
		return err
	}
	return nil
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
