// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package frontend

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zb.256lights.llc/pkg/internal/backendtest"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/lua"
	"zb.256lights.llc/pkg/internal/lualex"
	"zb.256lights.llc/pkg/internal/system"
	"zb.256lights.llc/pkg/internal/testcontext"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log/testlog"
)

func TestLuaToGo(t *testing.T) {
	tests := []struct {
		expr string
		want any
	}{
		{
			expr: "nil",
			want: nil,
		},
		{
			expr: "true",
			want: true,
		},
		{
			expr: "false",
			want: false,
		},
		{
			expr: `"foo"`,
			want: "foo",
		},
		{
			expr: "42",
			want: int64(42),
		},
		{
			expr: "3.14",
			want: 3.14,
		},
		{
			expr: `{n=0}`,
			want: []any{},
		},
		{
			expr: "{123, 456}",
			want: []any{int64(123), int64(456)},
		},
		{
			expr: "{123, nil, 456}",
			want: []any{int64(123), nil, int64(456)},
		},
		{
			expr: "{n=3, 123}",
			want: []any{int64(123), nil, nil},
		},
		{
			expr: `{}`,
			want: map[string]any{},
		},
		{
			expr: `{foo="bar", baz=42}`,
			want: map[string]any{"foo": "bar", "baz": int64(42)},
		},
	}

	ctx, cancel := testcontext.New(t)
	defer cancel()
	storeDir := backendtest.NewStoreDirectory(t)

	di := new(zbstorerpc.DeferredImporter)
	_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
		TempDir: t.TempDir(),
		ClientOptions: zbstorerpc.CodecOptions{
			Importer: di,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          newTestRPCStore(store, di),
		StoreDirectory: storeDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := eval.Close(); err != nil {
			t.Error("eval.Close:", err)
		}
	}()

	for _, test := range tests {
		got, err := eval.Expression(ctx, test.expr)
		if err != nil {
			t.Errorf("%s: %v", test.expr, err)
			continue
		}
		if diff := cmp.Diff(test.want, got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("%s (-want +got):\n%s", test.expr, diff)
		}
	}
}

func TestGetenv(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		envOK    bool
		want     any
	}{
		{
			name:     "Success",
			envValue: "foo",
			envOK:    true,
			want:     "foo",
		},
		{
			name:     "Missing",
			envValue: "",
			envOK:    false,
			want:     nil,
		},
		{
			name:     "Empty",
			envValue: "",
			envOK:    true,
			want:     "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := testcontext.New(t)
			defer cancel()
			storeDir := backendtest.NewStoreDirectory(t)

			const wantKey = "BAR"
			di := new(zbstorerpc.DeferredImporter)
			_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
				TempDir: t.TempDir(),
				ClientOptions: zbstorerpc.CodecOptions{
					Importer: di,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			callCount := 0
			eval, err := NewEval(&Options{
				Store:          newTestRPCStore(store, di),
				StoreDirectory: storeDir,
				LookupEnv: func(ctx context.Context, key string) (string, bool) {
					callCount++
					if key != wantKey {
						t.Errorf("LookupEnv called with %q; want %q", key, wantKey)
					}
					return test.envValue, test.envOK
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := eval.Close(); err != nil {
					t.Error("eval.Close:", err)
				}
			}()

			expr := "os.getenv('" + wantKey + "')"
			got, err := eval.Expression(ctx, expr)
			if err != nil {
				t.Fatalf("%s: %v", expr, err)
			}
			if diff := cmp.Diff(test.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("%s (-want +got):\n%s", expr, diff)
			}
		})
	}
}

func TestStringMethod(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()
	storeDir := backendtest.NewStoreDirectory(t)

	di := new(zbstorerpc.DeferredImporter)
	_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
		TempDir: t.TempDir(),
		ClientOptions: zbstorerpc.CodecOptions{
			Importer: di,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          newTestRPCStore(store, di),
		StoreDirectory: storeDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := eval.Close(); err != nil {
			t.Error("eval.Close:", err)
		}
	}()

	const expr = `("abcdef"):sub(2, 4)`
	got, err := eval.Expression(ctx, expr)
	if err != nil {
		t.Fatal(err)
	}
	want := any("bcd")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("%s (-want +got):\n%s", expr, diff)
	}
}

func TestImportFromDerivation(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()
	storeDir := backendtest.NewStoreDirectory(t)

	di := new(zbstorerpc.DeferredImporter)
	_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
		TempDir: t.TempDir(),
		ClientOptions: zbstorerpc.CodecOptions{
			Importer: di,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          newTestRPCStore(store, di),
		StoreDirectory: storeDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := eval.Close(); err != nil {
			t.Error("eval.Close:", err)
		}
	}()

	results, err := eval.URLs(ctx, []string{
		filepath.Join("testdata", "ifd.lua") + `#` + system.Current().String(),
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

func TestImportExitStore(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()
	storeDir := backendtest.NewStoreDirectory(t)

	di := new(zbstorerpc.DeferredImporter)
	_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
		TempDir: t.TempDir(),
		ClientOptions: zbstorerpc.CodecOptions{
			Importer: di,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          newTestRPCStore(store, di),
		StoreDirectory: storeDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := eval.Close(); err != nil {
			t.Error("eval.Close:", err)
		}
	}()

	secretPath := filepath.Join(t.TempDir(), "secret.lua")
	if err := os.WriteFile(secretPath, []byte("return \"secret\"\n"), 0o666); err != nil {
		t.Fatal(err)
	}

	fContent := `return import(` + lualex.Quote(secretPath) + `)`
	expr := `local f = toFile("f.lua", ` + lualex.Quote(fContent) + `); local m = await(import(f)); assert(m == nil, string.format("%s is not nil", type(m)))`
	if _, err := eval.Expression(ctx, expr); err != nil {
		t.Fatal(err)
	}
}

func TestImportCycle(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()
	storeDir := backendtest.NewStoreDirectory(t)

	di := new(zbstorerpc.DeferredImporter)
	_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
		TempDir: t.TempDir(),
		ClientOptions: zbstorerpc.CodecOptions{
			Importer: di,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          newTestRPCStore(store, di),
		StoreDirectory: storeDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := eval.Close(); err != nil {
			t.Error("eval.Close:", err)
		}
	}()

	toString := func(x any) string {
		s, _ := x.(string)
		return s
	}

	t.Run("Self", func(t *testing.T) {
		path := filepath.Join("testdata", "cycle", "self.lua")
		results, err := eval.URLs(ctx, []string{path})
		if err != nil {
			t.Fatal(err)
		}
		got, _ := results[0].([]any)
		const want = "import cycle"
		if len(got) != 2 || got[0] != nil || !strings.Contains(toString(got[1]), want) {
			t.Errorf("import(%q) = %v; want nil, (string containing %q)", path, got, want)
		} else {
			t.Logf("Error message: %s", toString(got[1]))
		}
	})

	t.Run("MultipleFiles", func(t *testing.T) {
		path := filepath.Join("testdata", "cycle", "a.lua")
		results, err := eval.URLs(ctx, []string{path})
		if err != nil {
			t.Fatal(err)
		}
		got, _ := results[0].([]any)
		const want = "import cycle"
		if len(got) != 2 || got[0] != nil || !strings.Contains(toString(got[1]), want) {
			t.Errorf("import(%q) = %v; want nil, (string containing %q)", path, got, want)
		} else {
			t.Logf("Error message: %s", toString(got[1]))
		}
	})

	t.Run("Defer", func(t *testing.T) {
		path := filepath.Join("testdata", "cycle", "defer_a.lua")
		got, err := eval.URLs(ctx, []string{path + "#4"})
		if err != nil {
			t.Fatal(err)
		}
		want := []any{int64(7)}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("import(%q) (-want +got):\n%s", path, diff)
		}
	})
}

func TestExtract(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()
	storeDir := backendtest.NewStoreDirectory(t)

	di := new(zbstorerpc.DeferredImporter)
	_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
		TempDir: t.TempDir(),
		ClientOptions: zbstorerpc.CodecOptions{
			Importer: di,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          newTestRPCStore(store, di),
		StoreDirectory: storeDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := eval.Close(); err != nil {
			t.Error("eval.Close:", err)
		}
	}()

	path := filepath.Join("testdata", "extract.lua")
	results, err := eval.URLs(ctx, []string{
		path + "#full",
		path + "#stripped",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(results), 2; got != want {
		t.Fatalf("len(results) = %d; want %d", got, want)
	}

	t.Run("Full", func(t *testing.T) {
		ctx, cancel := testcontext.New(t)
		defer cancel()

		drv, ok := results[0].(*Derivation)
		if !ok {
			t.Fatalf("result is %T; want *Derivation", results[0])
		}
		var response zbstorerpc.RealizeResponse
		err := jsonrpc.Do(ctx, store, zbstorerpc.RealizeMethod, &response, &zbstorerpc.RealizeRequest{
			DrvPaths: []zbstore.Path{drv.Path},
		})
		if err != nil {
			t.Fatal(err)
		}
		build, err := backendtest.WaitForSuccessfulBuild(ctx, store, response.BuildID)
		if err != nil {
			t.Fatal(err)
		}
		outputPath, err := build.FindRealizeOutput(zbstore.OutputReference{
			DrvPath:    drv.Path,
			OutputName: zbstore.DefaultDerivationOutputName,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !outputPath.Valid {
			t.Fatalf("missing path for %s", drv.Path)
		}
		got, err := os.ReadFile(filepath.Join(string(outputPath.X), "foo", "bar.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if want := "Hello, World!\n"; string(got) != want {
			t.Errorf("content of %s = %q; want %q", outputPath.X, got, want)
		}
	})

	t.Run("Stripped", func(t *testing.T) {
		ctx, cancel := testcontext.New(t)
		defer cancel()

		drv, ok := results[1].(*Derivation)
		if !ok {
			t.Fatalf("result is %T; want *Derivation", results[1])
		}
		var response zbstorerpc.RealizeResponse
		err := jsonrpc.Do(ctx, store, zbstorerpc.RealizeMethod, &response, &zbstorerpc.RealizeRequest{
			DrvPaths: []zbstore.Path{drv.Path},
		})
		if err != nil {
			t.Fatal(err)
		}
		build, err := backendtest.WaitForSuccessfulBuild(ctx, store, response.BuildID)
		if err != nil {
			t.Fatal(err)
		}
		outputPath, err := build.FindRealizeOutput(zbstore.OutputReference{
			DrvPath:    drv.Path,
			OutputName: zbstore.DefaultDerivationOutputName,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !outputPath.Valid {
			t.Fatalf("missing path for %s", drv.Path)
		}
		got, err := os.ReadFile(filepath.Join(string(outputPath.X), "bar.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if want := "Hello, World!\n"; string(got) != want {
			t.Errorf("content of %s = %q; want %q", outputPath.X, got, want)
		}
	})
}

func TestNewState(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()
	storeDir := backendtest.NewStoreDirectory(t)

	di := new(zbstorerpc.DeferredImporter)
	_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
		TempDir: t.TempDir(),
		ClientOptions: zbstorerpc.CodecOptions{
			Importer: di,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          newTestRPCStore(store, di),
		StoreDirectory: storeDir,
	})
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
	ctx, cancel := testcontext.New(b)
	defer cancel()
	storeDir := backendtest.NewStoreDirectory(b)

	di := new(zbstorerpc.DeferredImporter)
	_, store, err := backendtest.NewServer(ctx, b, storeDir, &backendtest.Options{
		TempDir: b.TempDir(),
		ClientOptions: zbstorerpc.CodecOptions{
			Importer: di,
		},
	})
	if err != nil {
		b.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          newTestRPCStore(store, di),
		StoreDirectory: storeDir,
	})
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

// testRPCStore is an implementation of [Store]
// that communicates to a real backend using JSON-RPC.
// Imported paths are tracked.
// Realization logs are ignored.
type testRPCStore struct {
	zbstorerpc.Store

	mu      sync.Mutex
	imports []zbstore.Path
}

func newTestRPCStore(client *jsonrpc.Client, di *zbstorerpc.DeferredImporter) *testRPCStore {
	store := &testRPCStore{
		Store: zbstorerpc.Store{Handler: client},
	}
	di.SetImporter(store)
	return store
}

func (store *testRPCStore) readImports() []zbstore.Path {
	store.mu.Lock()
	defer store.mu.Unlock()
	return slices.Clone(store.imports)
}

func (store *testRPCStore) StoreImport(ctx context.Context, r io.Reader) error {
	done := make(chan struct{})
	pr, pw := io.Pipe()
	go func() {
		defer close(done)
		defer pr.Close()
		zbstore.ReceiveExport(exportSpy{store}, pr)
	}()
	err := store.Store.StoreImport(ctx, io.TeeReader(r, pw))
	<-done
	return err
}

func (store *testRPCStore) Realize(ctx context.Context, want sets.Set[zbstore.OutputReference]) ([]*zbstorerpc.BuildResult, error) {
	var realizeResponse zbstorerpc.RealizeResponse
	err := jsonrpc.Do(ctx, store.Handler, zbstorerpc.RealizeMethod, &realizeResponse, &zbstorerpc.RealizeRequest{
		DrvPaths: slices.Collect(func(yield func(zbstore.Path) bool) {
			for ref := range want.All() {
				if !yield(ref.DrvPath) {
					return
				}
			}
		}),
	})
	if err != nil {
		return nil, err
	}
	build, err := backendtest.WaitForSuccessfulBuild(ctx, store.Handler, realizeResponse.BuildID)
	if err != nil {
		return nil, err
	}
	return build.Results, nil
}

type exportSpy struct {
	store *testRPCStore
}

func (e exportSpy) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (e exportSpy) ReceiveNAR(trailer *zbstore.ExportTrailer) {
	e.store.mu.Lock()
	defer e.store.mu.Unlock()
	e.store.imports = append(e.store.imports, trailer.StorePath)
}

func TestMain(m *testing.M) {
	testlog.Main(nil)
	os.Exit(m.Run())
}
