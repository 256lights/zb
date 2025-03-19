// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package backend

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/storetest"
	"zb.256lights.llc/pkg/internal/system"
	"zb.256lights.llc/pkg/sets"
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
		storePath1, ca1, err := storetest.ExportFlatFile(exporter, dir, "hello.txt", []byte(fileContent), nix.SHA256)
		if err != nil {
			t.Fatal(err)
		}
		drv := &zbstore.Derivation{
			Dir:          dir,
			Name:         "a",
			System:       system.Current().String(),
			Builder:      "true",
			InputSources: *sets.NewSorted(storePath1),
			Outputs: map[string]*zbstore.DerivationOutputType{
				zbstore.DefaultDerivationOutputName: zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
			},
		}
		drvName := drv.Name + zbstore.DerivationExt
		drvData, err := drv.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		storePath2, ca2, err := storetest.ExportText(exporter, dir, drvName, drvData, drv.References().ToSet(""))
		if err != nil {
			t.Fatal(err)
		}
		if err := exporter.Close(); err != nil {
			t.Fatal(err)
		}

		client := newTestServer(t, dir, jsonrpc.MethodNotFoundHandler{}, nil, &Options{
			RealDir: realStoreDir,
		})

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
			Path: string(storePath1),
		})
		if err != nil {
			t.Error(err)
		}
		if !exists {
			t.Errorf("store reports exists=false for %s", storePath1)
		}
		err = jsonrpc.Do(ctx, client, zbstore.ExistsMethod, &exists, &zbstore.ExistsRequest{
			Path: string(storePath2),
		})
		if err != nil {
			t.Error(err)
		}
		if !exists {
			t.Errorf("store reports exists=false for %s", storePath2)
		}

		// Call info method.
		var info zbstore.InfoResponse
		err = jsonrpc.Do(ctx, client, zbstore.InfoMethod, &info, &zbstore.InfoRequest{
			Path: storePath1,
		})
		if err != nil {
			t.Error(err)
		} else {
			want := wantFileObjectInfo(info.Info, []byte(fileContent), ca1, nil)
			if diff := cmp.Diff(want, info.Info); diff != "" {
				t.Errorf("%s info (-want +got):\n%s", storePath1, diff)
			}
		}
		err = jsonrpc.Do(ctx, client, zbstore.InfoMethod, &info, &zbstore.InfoRequest{
			Path: storePath2,
		})
		if err != nil {
			t.Error(err)
		} else {
			want := wantFileObjectInfo(info.Info, []byte(drvData), ca2, drv.References().ToSet(storePath2))
			if diff := cmp.Diff(want, info.Info); diff != "" {
				t.Errorf("%s info (-want +got):\n%s", storePath2, diff)
			}
		}

		// Verify that store objects exist on disk.
		realFilePath := filepath.Join(realStoreDir, storePath1.Base())
		if got, err := os.ReadFile(realFilePath); err != nil {
			t.Error(err)
		} else if string(got) != fileContent {
			t.Errorf("%s content = %q; want %q", storePath1, got, fileContent)
		}
		if info, err := os.Lstat(realFilePath); err != nil {
			t.Error(err)
		} else if got := info.Mode(); got&0o111 != 0 {
			t.Errorf("mode = %v; want non-executable", got)
		}
		realFilePath = filepath.Join(realStoreDir, storePath2.Base())
		if got, err := os.ReadFile(realFilePath); err != nil {
			t.Error(err)
		} else if !bytes.Equal(got, drvData) {
			t.Errorf("%s content = %q; want %q", storePath2, got, fileContent)
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
func newTestServer(tb testing.TB, storeDir zbstore.Directory, clientHandler jsonrpc.Handler, clientReceiver zbstore.NARReceiver, opts *Options) *jsonrpc.Client {
	tb.Helper()
	helperDir := tb.TempDir()
	buildDir := filepath.Join(helperDir, "build")
	if err := os.Mkdir(buildDir, 0o777); err != nil {
		tb.Fatal(err)
	}

	var wg sync.WaitGroup
	opts2 := new(Options)
	if opts != nil {
		*opts2 = *opts
	}
	opts2.BuildDir = buildDir
	opts2.DisableSandbox = true
	if opts2.CoresPerBuild < 1 {
		opts2.CoresPerBuild = 1
	}
	srv := NewServer(storeDir, filepath.Join(helperDir, "db.sqlite"), opts2)
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

		// Make entire store writable for deletion.
		filepath.WalkDir(string(storeDir), func(path string, entry fs.DirEntry, err error) error {
			perm := os.FileMode(0o666)
			if entry.IsDir() {
				perm = 0o777
			}
			if err := os.Chmod(path, perm); err != nil {
				tb.Log(err)
			}
			return nil
		})
	})

	return client
}

// wantObjectInfo builds the expected [*zbstore.ObjectInfo]
// for the given data, content address, and references.
// It uses got.NARHash to determine the hashing algorithm to check against.
func wantObjectInfo(got *zbstore.ObjectInfo, narData []byte, ca zbstore.ContentAddress, refs *sets.Sorted[zbstore.Path]) *zbstore.ObjectInfo {
	info := &zbstore.ObjectInfo{
		NARSize:    int64(len(narData)),
		References: slices.Collect(refs.Values()),
		CA:         ca,
	}
	if info.References == nil {
		// Should not be null.
		info.References = []zbstore.Path{}
	}

	ht := got.NARHash.Type()
	if ht == 0 {
		ht = nix.SHA256
	}
	h := nix.NewHasher(ht)
	h.Write(narData)
	info.NARHash = h.SumHash()

	return info
}

func wantFileObjectInfo(got *zbstore.ObjectInfo, fileData []byte, ca zbstore.ContentAddress, refs *sets.Sorted[zbstore.Path]) *zbstore.ObjectInfo {
	buf := new(bytes.Buffer)
	if err := storetest.SingleFileNAR(buf, fileData); err != nil {
		panic(err)
	}
	return wantObjectInfo(got, buf.Bytes(), ca, refs)
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
