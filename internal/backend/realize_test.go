// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"zombiezen.com/go/log/testlog"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/zb/internal/jsonrpc"
	"zombiezen.com/go/zb/internal/storetest"
	"zombiezen.com/go/zb/internal/system"
	"zombiezen.com/go/zb/sortedset"
	"zombiezen.com/go/zb/zbstore"
)

const (
	shPath         = "/bin/sh"
	powershellPath = `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`
)

func TestRealizeSingleDerivation(t *testing.T) {
	ctx := testlog.WithTB(context.Background(), t)
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
	drvContent.Builder, drvContent.Args = catcatBuilder()
	drvPath, err := storetest.ExportDerivation(exporter, drvContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := exporter.Close(); err != nil {
		t.Fatal(err)
	}

	client := newTestServer(t, dir, string(dir), &testBuildLogger{t}, nil)
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

	const wantOutputContent = "Hello, World!\nHello, World!\n"
	wantOutputPath, err := singleFileOutputPath(dir, wantOutputName, []byte(wantOutputContent), zbstore.References{})
	if err != nil {
		t.Fatal(err)
	}
	checkSingleFileOutput(t, wantOutputPath, []byte(wantOutputContent), got)
}

func TestRealizeMultiStep(t *testing.T) {
	ctx := testlog.WithTB(context.Background(), t)
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
	drv1Content := &zbstore.Derivation{
		Name:   "hello2.txt",
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
	drv1Content.Builder, drv1Content.Args = catcatBuilder()
	drv1Path, err := storetest.ExportDerivation(exporter, drv1Content)
	if err != nil {
		t.Fatal(err)
	}
	const wantOutputName = "hello4.txt"
	drv2Content := &zbstore.Derivation{
		Name:   "hello4.txt",
		Dir:    dir,
		System: system.Current().String(),
		Env: map[string]string{
			"in":  zbstore.UnknownCAOutputPlaceholder(drv1Path, zbstore.DefaultDerivationOutputName),
			"out": zbstore.HashPlaceholder("out"),
		},
		InputDerivations: map[zbstore.Path]*sortedset.Set[string]{
			drv1Path: sortedset.New(zbstore.DefaultDerivationOutputName),
		},
		Outputs: map[string]*zbstore.DerivationOutput{
			zbstore.DefaultDerivationOutputName: zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
		},
	}
	drv2Content.Builder, drv2Content.Args = catcatBuilder()
	drv2Path, err := storetest.ExportDerivation(exporter, drv2Content)
	if err != nil {
		t.Fatal(err)
	}
	if err := exporter.Close(); err != nil {
		t.Fatal(err)
	}

	client := newTestServer(t, dir, string(dir), &testBuildLogger{t}, nil)
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
		DrvPath: drv2Path,
	})
	if err != nil {
		t.Fatal(err)
	}

	wantOutputContent := strings.Repeat(inputContent, 4)
	wantOutputPath, err := singleFileOutputPath(dir, wantOutputName, []byte(wantOutputContent), zbstore.References{})
	if err != nil {
		t.Fatal(err)
	}
	checkSingleFileOutput(t, wantOutputPath, []byte(wantOutputContent), got)
}

func TestRealizeFixed(t *testing.T) {
	ctx := testlog.WithTB(context.Background(), t)
	dir, err := zbstore.CleanDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	exportBuffer := new(bytes.Buffer)
	exporter := zbstore.NewExporter(exportBuffer)
	const wantOutputName = "hello.txt"
	const wantOutputContent = "Hello, World!\n"
	wantOutputCA := nix.FlatFileContentAddress(mustParseHash(t, "sha256:c98c24b677eff44860afea6f493bbaec5bb1c4cbb209c6fc2bbb47f66ff2ad31"))
	drv1Content := &zbstore.Derivation{
		Name:   wantOutputName,
		Dir:    dir,
		System: system.Current().String(),
		Env: map[string]string{
			"out": zbstore.HashPlaceholder("out"),
		},
		Outputs: map[string]*zbstore.DerivationOutput{
			zbstore.DefaultDerivationOutputName: zbstore.FixedCAOutput(wantOutputCA),
		},
	}
	if runtime.GOOS == "windows" {
		drv1Content.Builder = powershellPath
		drv1Content.Args = []string{
			"-Command",
			"\"Hello, World!`n\" | Out-File -NoNewline -Encoding ascii -FilePath ${env:out}",
		}
	} else {
		drv1Content.Builder = shPath
		drv1Content.Args = []string{
			"-c",
			`echo 'Hello, World!' > $out`,
		}
	}
	drv1Path, err := storetest.ExportDerivation(exporter, drv1Content)
	if err != nil {
		t.Fatal(err)
	}
	// Create a second derivation with the same output hash
	// but a totally failing builder.
	drv2Content := &zbstore.Derivation{
		Name:   wantOutputName,
		Dir:    dir,
		System: system.Current().String(),
		Env: map[string]string{
			"out": zbstore.HashPlaceholder("out"),
		},
		Outputs: map[string]*zbstore.DerivationOutput{
			zbstore.DefaultDerivationOutputName: zbstore.FixedCAOutput(wantOutputCA),
		},
	}
	if runtime.GOOS == "windows" {
		drv2Content.Builder = powershellPath
		drv2Content.Args = []string{"-Command", "exit 1"}
	} else {
		drv2Content.Builder = shPath
		drv2Content.Args = []string{"-c", "exit 1"}
	}
	drv2Path, err := storetest.ExportDerivation(exporter, drv2Content)
	if err != nil {
		t.Fatal(err)
	}
	if err := exporter.Close(); err != nil {
		t.Fatal(err)
	}
	wantOutputPath, err := zbstore.FixedCAOutputPath(dir, wantOutputName, wantOutputCA, zbstore.References{})
	if err != nil {
		t.Fatal(err)
	}

	client := newTestServer(t, dir, string(dir), &testBuildLogger{t}, nil)
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
		DrvPath: drv1Path,
	})
	if err != nil {
		t.Fatal("build drv1:", err)
	}
	checkSingleFileOutput(t, wantOutputPath, []byte(wantOutputContent), got)

	// Now let's build the second derivation to see whether the output gets reused.
	got = new(zbstore.RealizeResponse)
	err = jsonrpc.Do(ctx, client, zbstore.RealizeMethod, got, &zbstore.RealizeRequest{
		DrvPath: drv2Path,
	})
	if err != nil {
		t.Fatal("build drv2:", err)
	}
	checkSingleFileOutput(t, wantOutputPath, []byte(wantOutputContent), got)
}

func checkSingleFileOutput(tb testing.TB, wantOutputPath zbstore.Path, wantOutputContent []byte, got *zbstore.RealizeResponse) {
	tb.Helper()
	want := &zbstore.RealizeResponse{
		Outputs: []*zbstore.RealizeOutput{
			{
				Name: zbstore.DefaultDerivationOutputName,
				Path: zbstore.NonNull(wantOutputPath),
			},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		tb.Errorf("realize response (-want +got):\n%s", diff)
	}

	// Try to compare the file if the response is the right shape.
	gotOutputs := slices.Collect(got.OutputsByName(zbstore.DefaultDerivationOutputName))
	if len(gotOutputs) != 1 || !gotOutputs[0].Path.Valid {
		return
	}
	gotOutputPath := gotOutputs[0].Path.X

	if got, err := os.ReadFile(string(gotOutputPath)); err != nil {
		tb.Error(err)
	} else if !bytes.Equal(got, wantOutputContent) {
		tb.Errorf("%s content = %q; want %q", wantOutputPath, got, wantOutputContent)
	}
	if info, err := os.Lstat(string(gotOutputPath)); err != nil {
		tb.Error(err)
	} else if got := info.Mode(); got&0o111 != 0 {
		tb.Errorf("%s mode = %v; want non-executable", gotOutputPath, got)
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

// catcatBuilder returns a builder that writes $in twice to $out
// with no dependencies other than the system shell.
func catcatBuilder() (builder string, builderArgs []string) {
	if runtime.GOOS == "windows" {
		return powershellPath, []string{
			"-Command",
			`$x = Get-Content -Raw ${env:in} ; ($x + $x) | Out-File -NoNewline -Encoding ascii -FilePath ${env:out}`,
		}
	}
	return shPath, []string{
		"-c",
		`while read line; do echo "$line"; echo "$line"; done < $in > $out`,
	}
}

func singleFileOutputPath(dir zbstore.Directory, name string, data []byte, refs zbstore.References) (zbstore.Path, error) {
	wantOutputNAR := new(bytes.Buffer)
	if err := storetest.SingleFileNAR(wantOutputNAR, []byte(data)); err != nil {
		return "", err
	}
	ca, _, err := zbstore.SourceSHA256ContentAddress("", bytes.NewReader(wantOutputNAR.Bytes()))
	if err != nil {
		return "", err
	}
	p, err := zbstore.FixedCAOutputPath(dir, name, ca, refs)
	if err != nil {
		return "", err
	}
	return p, nil
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

func mustParseHash(tb testing.TB, s string) nix.Hash {
	tb.Helper()
	h, err := nix.ParseHash(s)
	if err != nil {
		tb.Fatal(err)
	}
	return h
}
