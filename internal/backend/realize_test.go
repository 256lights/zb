// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package backend

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/zb/internal/jsonrpc"
	"zombiezen.com/go/zb/internal/storetest"
	"zombiezen.com/go/zb/internal/system"
	"zombiezen.com/go/zb/sortedset"
	"zombiezen.com/go/zb/zbstore"
)

func TestRealize(t *testing.T) {
	ctx := context.Background()
	dir, err := zbstore.CleanDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	const inputContent = "Hello, World!\n"
	exportBuffer := new(bytes.Buffer)
	exporter := zbstore.NewExporter(exportBuffer)
	inputFilePath, err := storetest.ExportSourceFile(exporter, dir, "", "hello.txt", []byte(inputContent), zbstore.References{})
	if err != nil {
		t.Fatal(err)
	}
	const wantOutputName = "hello2.txt"
	drvContent := &zbstore.Derivation{
		Name:   wantOutputName,
		Dir:    dir,
		System: system.Current().String(),
		Env: map[string]string{
			"in":  string(inputFilePath),
			"out": zbstore.HashPlaceholder("out"),
		},
		InputSources: *sortedset.New(
			inputFilePath,
		),
		Outputs: map[string]*zbstore.DerivationOutput{
			zbstore.DefaultDerivationOutputName: zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
		},
	}
	if runtime.GOOS == "windows" {
		drvContent.Builder = `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`
		drvContent.Args = []string{
			"-Command",
			`$x = Get-Content -Raw ${env:in} ; ($x + $x) | Out-File -NoNewline -Encoding ascii -FilePath ${env:out}`,
		}
	} else {
		drvContent.Builder = "/bin/sh"
		drvContent.Args = []string{
			"-c",
			`while read line; do echo "$line"; echo "$line"; done < $in > $out`,
		}
	}
	drvPath, err := storetest.ExportDerivation(exporter, drvContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := exporter.Close(); err != nil {
		t.Fatal(err)
	}

	const wantOutputContent = "Hello, World!\nHello, World!\n"
	wantOutputNAR := new(bytes.Buffer)
	storetest.SingleFileNAR(wantOutputNAR, []byte(wantOutputContent))
	wantOutputCA, _, err := zbstore.SourceSHA256ContentAddress("", bytes.NewReader(wantOutputNAR.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	wantOutputPath, err := zbstore.FixedCAOutputPath(dir, wantOutputName, wantOutputCA, zbstore.References{})
	if err != nil {
		t.Fatal(err)
	}

	client := newTestServer(t, dir, string(dir), jsonrpc.MethodNotFoundHandler{}, nil)
	codec, releaseCodec, err := storeCodec(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	err = codec.Export(exportBuffer)
	releaseCodec()
	if err != nil {
		t.Fatal(err)
	}

	got := new(zbstore.RealizeResponse)
	err = jsonrpc.Do(ctx, client, zbstore.RealizeMethod, got, &zbstore.RealizeRequest{
		DrvPath: drvPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := &zbstore.RealizeResponse{
		Outputs: []*zbstore.RealizeOutput{
			{
				Name: zbstore.DefaultDerivationOutputName,
				Path: zbstore.NonNull(wantOutputPath),
			},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("realize response (-want +got):\n%s", diff)
	}

	gotOutputs := slices.Collect(got.OutputsByName(zbstore.DefaultDerivationOutputName))
	if len(gotOutputs) == 1 && gotOutputs[0].Path.Valid {
		gotOutputPath := gotOutputs[0].Path.X
		if got, err := os.ReadFile(string(gotOutputPath)); err != nil {
			t.Error(err)
		} else if string(got) != wantOutputContent {
			t.Errorf("%s content = %q; want %q", wantOutputPath, got, wantOutputContent)
		}
		if info, err := os.Lstat(string(gotOutputPath)); err != nil {
			t.Error(err)
		} else if got := info.Mode(); got&0o111 != 0 {
			t.Errorf("%s mode = %v; want non-executable", gotOutputPath, got)
		}
	}
}

func TestMutexMap(t *testing.T) {
	// Prevent this test from blocking for more than 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mm := new(mutexMap[int])
	unlock1, err := mm.lock(ctx, 1)
	if err != nil {
		t.Fatal("lock(ctx, 1) on new map failed:", err)
	}

	// Verify that we can acquire a lock on an independent key.
	unlock2, err := mm.lock(ctx, 2)
	if err != nil {
		t.Fatal("lock(ctx, 2) after lock(ctx, 1) failed:", err)
	}

	// Verify that attempting a lock on the same key blocks until Done.
	failFastCtx, cancelFailFast := context.WithTimeout(ctx, 100*time.Millisecond)
	unlock1b, err := mm.lock(failFastCtx, 1)
	cancelFailFast()
	if err == nil {
		t.Error("lock(ctx, 1) acquired without releasing unlock1")
		unlock1b()
	}

	// Verify that unlocking a key allows a subsequent lock to succeed.
	unlock1()
	unlock1, err = mm.lock(ctx, 1)
	if err != nil {
		t.Fatal("lock(ctx, 1) after unlock1 failed:", err)
	}

	// Verify that unlocking a key allows a concurrent lock to succeed.
	lock2Done := make(chan error)
	go func() {
		_, err := mm.lock(ctx, 2)
		lock2Done <- err
	}()
	// Wait for a little bit to make it more likely that the other goroutine hit lock(2).
	timer := time.NewTimer(10 * time.Millisecond)
	select {
	case <-timer.C:
	case <-ctx.Done():
		timer.Stop()
	}
	unlock2()
	if err := <-lock2Done; err != nil {
		t.Error("lock(ctx, 2) with concurrent unlock2 failed:", err)
	}
}
