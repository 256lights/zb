// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package frontend

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"zb.256lights.llc/pkg/internal/backendtest"
	"zb.256lights.llc/pkg/internal/testcontext"
)

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

func TestCollatePath(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"", "a", -1},
		{"a", "", 1},
		{"a", "a", 0},
		{"a", "b", -1},
		{"b", "a", 1},
		{"a", "a/b", -1},
		{"a/b", "a", 1},
		{"a!b", "a/b", 1},
		{"a/b", "a!b", -1},
	}
	for _, test := range tests {
		if got := collatePath(test.a, test.b); got != test.want {
			t.Errorf("collatePath(%q, %q) = %d; want %d", test.a, test.b, got, test.want)
		}
	}
}
