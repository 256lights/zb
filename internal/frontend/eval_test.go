// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package frontend

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zb.256lights.llc/pkg/internal/backendtest"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/lua"
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

	_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          testRPCStore{store},
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

func TestPath(t *testing.T) {
	t.Run("SingleFile", func(t *testing.T) {
		wantContent, err := os.ReadFile(filepath.Join("testdata", "hello.txt"))
		if err != nil {
			t.Fatal(err)
		}

		ctx, cancel := testcontext.New(t)
		defer cancel()
		storeDir := backendtest.NewStoreDirectory(t)

		_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
			TempDir: t.TempDir(),
		})
		if err != nil {
			t.Fatal(err)
		}
		eval, err := NewEval(&Options{
			Store:          testRPCStore{store},
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

		got, err := eval.Expression(ctx, `path("testdata/hello.txt")`)
		if err != nil {
			t.Fatal(err)
		}
		gotString, ok := got.(string)
		if !ok {
			t.Fatalf("expression result is %T; want string", got)
		}
		gotPath, gotSubpath, err := storeDir.ParsePath(gotString)
		if err != nil {
			t.Fatal(err)
		}
		if gotSubpath != "" {
			t.Errorf("expression result contains subpath %q", gotSubpath)
		}

		gotContent, err := os.ReadFile(filepath.Join(string(storeDir), gotPath.Base()))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gotContent, wantContent) {
			t.Errorf("content of %s = %q; want %q", gotPath, gotContent, wantContent)
		}
	})

	t.Run("Directory", func(t *testing.T) {
		ctx, cancel := testcontext.New(t)
		defer cancel()
		storeDir := backendtest.NewStoreDirectory(t)

		_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
			TempDir: t.TempDir(),
		})
		if err != nil {
			t.Fatal(err)
		}
		eval, err := NewEval(&Options{
			Store:          testRPCStore{store},
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

		got, err := eval.Expression(ctx, `path("testdata/dir")`)
		if err != nil {
			t.Fatal(err)
		}
		gotString, ok := got.(string)
		if !ok {
			t.Fatalf("expression result is %T; want string", got)
		}
		gotPath, gotSubpath, err := storeDir.ParsePath(gotString)
		if err != nil {
			t.Fatal(err)
		}
		if gotSubpath != "" {
			t.Errorf("expression result contains subpath %q", gotSubpath)
		}

		wantFiles := []string{"a.txt", "b.txt", "c/d.txt"}
		for _, name := range wantFiles {
			wantContent, err := os.ReadFile(filepath.Join("testdata", "dir", filepath.FromSlash(name)))
			if err != nil {
				t.Fatal(err)
			}
			gotContent, err := os.ReadFile(filepath.Join(string(storeDir), gotPath.Base(), filepath.FromSlash(name)))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(gotContent, wantContent) {
				t.Errorf("content of %s = %q; want %q",
					filepath.Join(string(gotPath), filepath.FromSlash(name)), gotContent, wantContent)
			}
		}
	})

	t.Run("FilteredDirectory", func(t *testing.T) {
		ctx, cancel := testcontext.New(t)
		defer cancel()
		storeDir := backendtest.NewStoreDirectory(t)

		_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
			TempDir: t.TempDir(),
		})
		if err != nil {
			t.Fatal(err)
		}
		eval, err := NewEval(&Options{
			Store:          testRPCStore{store},
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

		got, err := eval.Expression(ctx, `path{path = "testdata/dir"; filter = function(name) return name ~= "c/d.txt" end }`)
		if err != nil {
			t.Fatal(err)
		}
		gotString, ok := got.(string)
		if !ok {
			t.Fatalf("expression result is %T; want string", got)
		}
		gotPath, gotSubpath, err := storeDir.ParsePath(gotString)
		if err != nil {
			t.Fatal(err)
		}
		if gotSubpath != "" {
			t.Errorf("expression result contains subpath %q", gotSubpath)
		}

		wantFiles := []string{"a.txt", "b.txt"}
		for _, name := range wantFiles {
			wantContent, err := os.ReadFile(filepath.Join("testdata", "dir", filepath.FromSlash(name)))
			if err != nil {
				t.Fatal(err)
			}
			gotContent, err := os.ReadFile(filepath.Join(string(storeDir), gotPath.Base(), filepath.FromSlash(name)))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(gotContent, wantContent) {
				t.Errorf("content of %s = %q; want %q",
					filepath.Join(string(gotPath), filepath.FromSlash(name)), gotContent, wantContent)
			}
		}
		if _, err := os.Lstat(filepath.Join(string(storeDir), gotPath.Base(), "c", "d.txt")); err == nil {
			t.Error("c/d.txt included")
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Error(err)
		}
		if _, err := os.Lstat(filepath.Join(string(storeDir), gotPath.Base(), "c")); err != nil {
			t.Error(err)
		}
	})

	t.Run("FilteredFile", func(t *testing.T) {
		wantContent, err := os.ReadFile(filepath.Join("testdata", "hello.txt"))
		if err != nil {
			t.Fatal(err)
		}

		ctx, cancel := testcontext.New(t)
		defer cancel()
		storeDir := backendtest.NewStoreDirectory(t)

		_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
			TempDir: t.TempDir(),
		})
		if err != nil {
			t.Fatal(err)
		}
		eval, err := NewEval(&Options{
			Store:          testRPCStore{store},
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

		got, err := eval.Expression(ctx, `path{path = "testdata/hello.txt"; filter = function() return false end }`)
		if err != nil {
			t.Fatal(err)
		}
		gotString, ok := got.(string)
		if !ok {
			t.Fatalf("expression result is %T; want string", got)
		}
		gotPath, gotSubpath, err := storeDir.ParsePath(gotString)
		if err != nil {
			t.Fatal(err)
		}
		if gotSubpath != "" {
			t.Errorf("expression result contains subpath %q", gotSubpath)
		}

		gotContent, err := os.ReadFile(filepath.Join(string(storeDir), gotPath.Base()))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gotContent, wantContent) {
			t.Errorf("content of %s = %q; want %q", gotPath, gotContent, wantContent)
		}
	})
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
			_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
				TempDir: t.TempDir(),
			})
			if err != nil {
				t.Fatal(err)
			}
			callCount := 0
			eval, err := NewEval(&Options{
				Store:          testRPCStore{store},
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

	_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          testRPCStore{store},
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

	_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          testRPCStore{store},
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

func TestImportCycle(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()
	storeDir := backendtest.NewStoreDirectory(t)

	_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          testRPCStore{store},
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

	_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          testRPCStore{store},
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

	_, store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          testRPCStore{store},
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

	_, store, err := backendtest.NewServer(ctx, b, storeDir, &backendtest.Options{
		TempDir: b.TempDir(),
	})
	if err != nil {
		b.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          testRPCStore{store},
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
// Realization logs are ignored.
type testRPCStore struct {
	client *jsonrpc.Client
}

func (store testRPCStore) Exists(ctx context.Context, path string) (bool, error) {
	var response bool
	err := jsonrpc.Do(ctx, store.client, zbstorerpc.ExistsMethod, &response, &zbstorerpc.ExistsRequest{
		Path: path,
	})
	if err != nil {
		return false, err
	}
	return response, nil
}

func (store testRPCStore) Import(ctx context.Context, r io.Reader) error {
	generic, releaseConn, err := store.client.Codec(ctx)
	if err != nil {
		return err
	}
	defer releaseConn()
	codec, ok := generic.(*zbstorerpc.Codec)
	if !ok {
		return fmt.Errorf("store connection is %T (want %T)", generic, (*zbstorerpc.Codec)(nil))
	}
	return codec.Export(nil, r)
}

func (store testRPCStore) Realize(ctx context.Context, want sets.Set[zbstore.OutputReference]) ([]*zbstorerpc.BuildResult, error) {
	var realizeResponse zbstorerpc.RealizeResponse
	err := jsonrpc.Do(ctx, store.client, zbstorerpc.RealizeMethod, &realizeResponse, &zbstorerpc.RealizeRequest{
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
	build, err := backendtest.WaitForSuccessfulBuild(ctx, store.client, realizeResponse.BuildID)
	if err != nil {
		return nil, err
	}
	return build.Results, nil
}

func TestMain(m *testing.M) {
	testlog.Main(nil)
	os.Exit(m.Run())
}
