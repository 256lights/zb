// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package frontend

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"zb.256lights.llc/pkg/internal/backend"
	"zb.256lights.llc/pkg/internal/backendtest"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/lua"
	"zb.256lights.llc/pkg/internal/lualex"
	"zb.256lights.llc/pkg/internal/storetest"
	"zb.256lights.llc/pkg/internal/system"
	"zb.256lights.llc/pkg/internal/testcontext"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log/testlog"
)

func TestExpression(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{
			expr: "nil",
			want: "nil",
		},
		{
			expr: "true",
			want: "true",
		},
		{
			expr: "false",
			want: "false",
		},
		{
			expr: `"foo"`,
			want: "foo",
		},
		{
			expr: `"foo".."bar"`,
			want: "foobar",
		},
		{
			expr: "42",
			want: "42",
		},
		{
			expr: "3.14",
			want: "3.14",
		},
	}

	ctx := testcontext.New(t)
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
		if got.String() != test.want {
			t.Errorf("%s = %s; want %s", test.expr, got, test.want)
		}
	}
}

func TestGetenv(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		envOK    bool
		want     string
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
			want:     "nil",
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
			ctx := testcontext.New(t)
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
			if got.String() != test.want {
				t.Errorf("%s = %q; want %q", expr, got, test.want)
			}
		})
	}
}

func TestStringMethod(t *testing.T) {
	ctx := testcontext.New(t)
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
	const want = "bcd"
	if got.String() != want {
		t.Errorf("%s = %s; want %s", expr, got, want)
	}
}

func TestImportFromDerivation(t *testing.T) {
	ctx := testcontext.New(t)
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

	results, err := eval.URLOutputs(ctx, []string{
		filepath.Join("testdata", "TestImportFromDerivation", "ifd.lua"),
	}, system.Current())
	if err != nil {
		t.Fatal(err)
	}
	const want = "Hello, World!"
	if got := results.Get("").String(); got != want {
		t.Errorf("result = %q; want %q", got, want)
	}
}

func TestImportExitStore(t *testing.T) {
	ctx := testcontext.New(t)
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
	ctx := testcontext.New(t)
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

	t.Run("Self", func(t *testing.T) {
		path := filepath.Join("testdata", "TestImportCycle", "self.lua")
		got, err := eval.URLs(ctx, []string{path})
		if err != nil {
			t.Fatal(err)
		}
		const want = "import cycle"
		if len(got) != 1 || !strings.Contains(got[0].String(), want) {
			t.Errorf("import(%q) = %v; want string containing %q", path, got, want)
		} else {
			t.Logf("Error message: %s", got[0])
		}
	})

	t.Run("MultipleFiles", func(t *testing.T) {
		path := filepath.Join("testdata", "TestImportCycle", "a.lua")
		got, err := eval.URLs(ctx, []string{path})
		if err != nil {
			t.Fatal(err)
		}
		const want = "import cycle"
		if len(got) != 1 || !strings.Contains(got[0].String(), want) {
			t.Errorf("import(%q) = %q; want string containing %q", path, got, want)
		} else {
			t.Logf("Error message: %s", got[0])
		}
	})

	t.Run("Defer", func(t *testing.T) {
		path := filepath.Join("testdata", "TestImportCycle", "defer_a.lua")
		gotOutputs, err := eval.URLs(ctx, []string{path + "#4"})
		if err != nil {
			t.Fatal(err)
		}
		got := make([]string, len(gotOutputs))
		for i, out := range gotOutputs {
			got[i] = out.String()
		}
		want := []string{"7"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("import(%q) (-want +got):\n%s", path, diff)
		}
	})
}

func TestStorePath(t *testing.T) {
	t.Run("ExistsLocally", func(t *testing.T) {
		ctx := testcontext.New(t)

		storeDir := backendtest.NewStoreDirectory(t)
		exportBuffer := new(bytes.Buffer)
		exporter := zbstore.NewExportWriter(exportBuffer)
		wantPath, _, err := storetest.ExportSourceFile(exporter, []byte("Hello, World!\n"), storetest.SourceExportOptions{
			Name:      "hello.txt",
			Directory: storeDir,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := exporter.Close(); err != nil {
			t.Fatal(err)
		}

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
		rpcStore := newTestRPCStore(store, di)
		if err := rpcStore.StoreImport(ctx, exportBuffer); err != nil {
			t.Fatal(err)
		}

		eval, err := NewEval(&Options{
			Store:          rpcStore,
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

		got, err := eval.Expression(ctx, "storePath("+lualex.Quote(string(wantPath))+")")
		if err != nil {
			t.Fatal(err)
		}
		if got.String() != string(wantPath) {
			t.Errorf("storePath(%q) = %s; want %s", wantPath, got, wantPath)
		}
	})

	t.Run("FromFallback", func(t *testing.T) {
		ctx := testcontext.New(t)

		storeDir := backendtest.NewStoreDirectory(t)
		exportBuffer := new(bytes.Buffer)
		exporter := zbstore.NewExportWriter(exportBuffer)
		wantPath, _, err := storetest.ExportSourceFile(exporter, []byte("Hello, World!\n"), storetest.SourceExportOptions{
			Name:      "hello.txt",
			Directory: storeDir,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := exporter.Close(); err != nil {
			t.Fatal(err)
		}
		fallback := new(storetest.Store)
		if err := fallback.StoreImport(ctx, bytes.NewReader(exportBuffer.Bytes())); err != nil {
			t.Fatal(err)
		}

		di := new(zbstorerpc.DeferredImporter)
		_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
			TempDir: t.TempDir(),
			Options: backend.Options{
				Fallback: fallback,
			},
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

		got, err := eval.Expression(ctx, "storePath("+lualex.Quote(string(wantPath))+")")
		if err != nil {
			t.Fatal(err)
		}
		if got.String() != string(wantPath) {
			t.Errorf("storePath(%q) = %s; want %s", wantPath, got, wantPath)
		}
	})
}

func TestExtract(t *testing.T) {
	ctx := testcontext.New(t)
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

	path := filepath.Join("testdata", "TestExtract", "extract.lua")
	results, err := eval.URLOutputs(ctx, []string{
		path + "#full",
		path + "#stripped",
	}, system.Current())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Full", func(t *testing.T) {
		ctx := testcontext.New(t)

		refs := results.Group(1).OutputReferences()
		if len(refs) == 0 {
			t.Fatal("No output references for result")
		}
		var response zbstorerpc.RealizeResponse
		err := jsonrpc.Do(ctx, store, zbstorerpc.RealizeMethod, &response, &zbstorerpc.RealizeRequest{
			DrvPaths: slices.Collect(func(yield func(zbstore.Path) bool) {
				for ref := range refs {
					if !yield(ref.DrvPath) {
						return
					}
				}
			}),
		})
		if err != nil {
			t.Fatal(err)
		}
		build, err := backendtest.WaitForSuccessfulBuild(ctx, store, response.BuildID)
		if err != nil {
			t.Fatal(err)
		}
		for ref := range refs {
			outputPath, err := build.FindRealizeOutput(ref)
			if err != nil {
				t.Fatal(err)
			}
			if !outputPath.Valid {
				t.Fatalf("missing path for %v", ref)
			}
			got, err := os.ReadFile(filepath.Join(string(outputPath.X), "foo", "bar.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if want := "Hello, World!\n"; string(got) != want {
				t.Errorf("content of %s = %q; want %q", outputPath.X, got, want)
			}
		}
	})

	t.Run("Stripped", func(t *testing.T) {
		ctx := testcontext.New(t)

		refs := results.Group(2).OutputReferences()
		if len(refs) == 0 {
			t.Fatal("No output references for result-2")
		}
		var response zbstorerpc.RealizeResponse
		err := jsonrpc.Do(ctx, store, zbstorerpc.RealizeMethod, &response, &zbstorerpc.RealizeRequest{
			DrvPaths: slices.Collect(func(yield func(zbstore.Path) bool) {
				for ref := range refs {
					if !yield(ref.DrvPath) {
						return
					}
				}
			}),
		})
		if err != nil {
			t.Fatal(err)
		}
		build, err := backendtest.WaitForSuccessfulBuild(ctx, store, response.BuildID)
		if err != nil {
			t.Fatal(err)
		}
		for ref := range refs {
			outputPath, err := build.FindRealizeOutput(ref)
			if err != nil {
				t.Fatal(err)
			}
			if !outputPath.Valid {
				t.Fatalf("missing path for %v", ref)
			}
			got, err := os.ReadFile(filepath.Join(string(outputPath.X), "bar.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if want := "Hello, World!\n"; string(got) != want {
				t.Errorf("content of %s = %q; want %q", outputPath.X, got, want)
			}
		}
	})
}

func TestNewState(t *testing.T) {
	ctx := testcontext.New(t)
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
	ctx := testcontext.New(b)
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

func (store *testRPCStore) FetchObjects(ctx context.Context, paths []zbstore.Path) (map[zbstore.Path]*zbstorerpc.ObjectInfo, error) {
	var resp zbstorerpc.FetchResponse
	err := jsonrpc.Do(ctx, store.Handler, zbstorerpc.FetchMethod, &resp, &zbstorerpc.FetchRequest{
		Paths: paths,
	})
	if err != nil {
		return nil, err
	}
	return resp.Found, nil
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
		Reuse: &zbstorerpc.ReusePolicy{All: true},
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
