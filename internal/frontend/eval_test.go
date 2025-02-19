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

	"github.com/google/go-cmp/cmp"
	"zb.256lights.llc/pkg/internal/backend"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/lua"
	"zb.256lights.llc/pkg/internal/system"
	"zb.256lights.llc/pkg/internal/testcontext"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log/testlog"
)

func TestStringMethod(t *testing.T) {
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

	const expr = `("abcdef"):sub(2, 4)`
	got, err := eval.Expression(ctx, expr, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{"bcd"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("%s (-want +got):\n%s", expr, diff)
	}
}

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

func TestNewState(t *testing.T) {
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

	l, err := eval.newState()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := l.Close(); err != nil {
			t.Error("Close:", err)
		}
	}()

	if got, want := l.Top(), 0; got != want {
		t.Errorf("l.Top() = %d; want %d", got, want)
	}
	if tp, err := l.Global(ctx, "derivation"); err != nil || tp != lua.TypeFunction {
		t.Errorf("l.Global(ctx, \"derivation\") = %v, %v; want function, <nil>", tp, err)
	}
}

// BenchmarkNewState measures the performance of spinning up a new interpreter.
func BenchmarkNewState(b *testing.B) {
	realStoreDir := b.TempDir()
	storeDir, err := zbstore.CleanDirectory(realStoreDir)
	if err != nil {
		b.Fatal(err)
	}
	store := newTestServer(b, storeDir, realStoreDir, jsonrpc.MethodNotFoundHandler{}, nil)
	eval, err := NewEval(storeDir, store, filepath.Join(b.TempDir(), "cache.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := eval.Close(); err != nil {
			b.Error("eval.Close:", err)
		}
	}()

	for b.Loop() {
		l, err := eval.newState()
		if err != nil {
			b.Fatal(err)
		}
		if err := l.Close(); err != nil {
			b.Error(err)
		}
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
