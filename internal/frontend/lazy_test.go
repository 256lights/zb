// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package frontend

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"zb.256lights.llc/pkg/internal/backendtest"
	"zb.256lights.llc/pkg/internal/testcontext"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
)

func TestLazy(t *testing.T) {
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

	expr := `lazy(function(fib, i) if math.type(i) ~= "integer" or i < 3 then return nil end; return fib[i-2] + fib[i-1]; end, {0, 1})[10]`
	got, err := eval.Expression(ctx, expr)
	if err != nil {
		t.Fatalf("%s: %v", expr, err)
	}
	if diff := cmp.Diff(int64(34), got); diff != "" {
		t.Errorf("%s (-want +got):\n%s", expr, diff)
	}
}
