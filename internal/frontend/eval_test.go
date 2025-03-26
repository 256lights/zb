// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package frontend

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zb.256lights.llc/pkg/internal/backendtest"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/lua"
	"zb.256lights.llc/pkg/internal/system"
	"zb.256lights.llc/pkg/internal/testcontext"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log/testlog"
)

func TestLuaToGo(t *testing.T) {
	tests := []struct {
		expr string
		want []any
	}{
		{
			expr: "nil",
			want: []any{nil},
		},
		{
			expr: "true",
			want: []any{true},
		},
		{
			expr: "false",
			want: []any{false},
		},
		{
			expr: `"foo"`,
			want: []any{"foo"},
		},
		{
			expr: "42",
			want: []any{int64(42)},
		},
		{
			expr: "3.14",
			want: []any{3.14},
		},
		{
			expr: `{n=0}`,
			want: []any{[]any{}},
		},
		{
			expr: "{123, 456}",
			want: []any{[]any{int64(123), int64(456)}},
		},
		{
			expr: "{123, nil, 456}",
			want: []any{[]any{int64(123), nil, int64(456)}},
		},
		{
			expr: "{n=3, 123}",
			want: []any{[]any{int64(123), nil, nil}},
		},
		{
			expr: `{}`,
			want: []any{map[string]any{}},
		},
		{
			expr: `{foo="bar", baz=42}`,
			want: []any{map[string]any{"foo": "bar", "baz": int64(42)}},
		},
	}

	ctx, cancel := testcontext.New(t)
	defer cancel()
	storeDir := backendtest.NewStoreDirectory(t)

	store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          store,
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
		got, err := eval.Expression(ctx, test.expr, nil)
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
			store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
				TempDir: t.TempDir(),
			})
			if err != nil {
				t.Fatal(err)
			}
			callCount := 0
			eval, err := NewEval(&Options{
				Store:          store,
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
			got, err := eval.Expression(ctx, expr, nil)
			if err != nil {
				t.Fatalf("%s: %v", expr, err)
			}
			if diff := cmp.Diff([]any{test.want}, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("%s (-want +got):\n%s", expr, diff)
			}
		})
	}
}

func TestStringMethod(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()
	storeDir := backendtest.NewStoreDirectory(t)

	store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          store,
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
	storeDir := backendtest.NewStoreDirectory(t)

	store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          store,
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

func TestImportCycle(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()
	storeDir := backendtest.NewStoreDirectory(t)

	store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          store,
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
		results, err := eval.File(ctx, path, nil)
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
		results, err := eval.File(ctx, path, nil)
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
		got, err := eval.File(ctx, path, []string{"[4]"})
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

	store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          store,
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

	results, err := eval.File(ctx, filepath.Join("testdata", "extract.lua"), []string{
		"full",
		"stripped",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(results), 2; got != want {
		t.Fatalf("len(results) = %d; want %d", got, want)
	}

	t.Run("Full", func(t *testing.T) {
		drv, ok := results[0].(*Derivation)
		if !ok {
			t.Fatalf("result is %T; want *Derivation", results[0])
		}
		var response zbstore.RealizeResponse
		err := jsonrpc.Do(ctx, store, zbstore.RealizeMethod, &response, &zbstore.RealizeRequest{
			DrvPath: drv.Path,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := len(response.Outputs), 1; got != want {
			t.Fatalf("received %d outputs from building %s; want %d", got, drv.Path, want)
		}
		if got, want := response.Outputs[0].Name, zbstore.DefaultDerivationOutputName; got != want {
			t.Errorf("name of output from %s = %q; want %q", drv.Path, got, want)
		}
		if !response.Outputs[0].Path.Valid {
			t.Fatalf("build of %s failed", drv.Path)
		}
		outputPath := response.Outputs[0].Path.X
		got, err := os.ReadFile(filepath.Join(string(outputPath), "foo", "bar.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if want := "Hello, World!\n"; string(got) != want {
			t.Errorf("content of %s = %q; want %q", outputPath, got, want)
		}
	})

	t.Run("Stripped", func(t *testing.T) {
		drv, ok := results[1].(*Derivation)
		if !ok {
			t.Fatalf("result is %T; want *Derivation", results[1])
		}
		var response zbstore.RealizeResponse
		err := jsonrpc.Do(ctx, store, zbstore.RealizeMethod, &response, &zbstore.RealizeRequest{
			DrvPath: drv.Path,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := len(response.Outputs), 1; got != want {
			t.Fatalf("received %d outputs from building %s; want %d", got, drv.Path, want)
		}
		if got, want := response.Outputs[0].Name, zbstore.DefaultDerivationOutputName; got != want {
			t.Errorf("name of output from %s = %q; want %q", drv.Path, got, want)
		}
		if !response.Outputs[0].Path.Valid {
			t.Fatalf("build of %s failed", drv.Path)
		}
		outputPath := response.Outputs[0].Path.X
		got, err := os.ReadFile(filepath.Join(string(outputPath), "bar.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if want := "Hello, World!\n"; string(got) != want {
			t.Errorf("content of %s = %q; want %q", outputPath, got, want)
		}
	})
}

func TestNewState(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()
	storeDir := backendtest.NewStoreDirectory(t)

	store, err := backendtest.NewServer(ctx, t, storeDir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          store,
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

	store, err := backendtest.NewServer(ctx, b, storeDir, &backendtest.Options{
		TempDir: b.TempDir(),
	})
	if err != nil {
		b.Fatal(err)
	}
	eval, err := NewEval(&Options{
		Store:          store,
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

type testBuildLogger struct {
	tb testing.TB
}

func (l *testBuildLogger) JSONRPC(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	return jsonrpc.ServeMux{
		zbstore.LogMethod: jsonrpc.HandlerFunc(l.log),
	}.JSONRPC(ctx, req)
}

func (l *testBuildLogger) log(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	args := new(zbstore.LogNotification)
	if err := json.Unmarshal(req.Params, args); err != nil {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
	}
	payload := args.Payload()
	if len(payload) == 0 {
		return nil, nil
	}
	l.tb.Logf("Build %s: %s", args.DrvPath, payload)
	return nil, nil
}

func TestMain(m *testing.M) {
	testlog.Main(nil)
	os.Exit(m.Run())
}
