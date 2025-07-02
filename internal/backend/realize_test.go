// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package backend_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	. "zb.256lights.llc/pkg/internal/backend"
	"zb.256lights.llc/pkg/internal/backendtest"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/storetest"
	"zb.256lights.llc/pkg/internal/system"
	"zb.256lights.llc/pkg/internal/testcontext"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/nix"
)

const (
	shPath         = "/bin/sh"
	powershellPath = `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`
)

func TestRealizeSingleDerivation(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()
	dir := backendtest.NewStoreDirectory(t)

	const inputContent = "Hello, World!\n"
	exportBuffer := new(bytes.Buffer)
	exporter := zbstore.NewExportWriter(exportBuffer)
	inputFilePath, _, err := storetest.ExportSourceFile(exporter, []byte(inputContent), storetest.SourceExportOptions{
		Name:      "hello.txt",
		Directory: dir,
	})
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
		InputSources: *sets.NewSorted(
			inputFilePath,
		),
		Outputs: map[string]*zbstore.DerivationOutputType{
			zbstore.DefaultDerivationOutputName: zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
		},
	}
	drvContent.Builder, drvContent.Args = catcatBuilder()
	drvPath, _, err := storetest.ExportDerivation(exporter, drvContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := exporter.Close(); err != nil {
		t.Fatal(err)
	}

	_, client, err := backendtest.NewServer(ctx, t, dir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	codec, releaseCodec, err := storeCodec(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	err = codec.Export(nil, exportBuffer)
	releaseCodec()
	if err != nil {
		t.Fatal(err)
	}

	realizeResponse := new(zbstorerpc.RealizeResponse)
	err = jsonrpc.Do(ctx, client, zbstorerpc.RealizeMethod, realizeResponse, &zbstorerpc.RealizeRequest{
		DrvPaths: []zbstore.Path{drvPath},
	})
	if err != nil {
		t.Fatal("RPC error:", err)
	}
	if realizeResponse.BuildID == "" {
		t.Fatal("no build ID returned")
	}

	got, err := backendtest.WaitForSuccessfulBuild(ctx, client, realizeResponse.BuildID)
	if err != nil {
		t.Fatal(err)
	}
	if gotLog, err := backendtest.ReadLog(ctx, client, realizeResponse.BuildID, drvPath); err != nil {
		t.Error(err)
	} else {
		if want := "catcat\n"; string(gotLog) != want {
			t.Errorf("build log:\n%s\n(want %q)", gotLog, want)
		}
	}

	const wantOutputContent = "Hello, World!\nHello, World!\n"
	wantOutputPath, err := singleFileOutputPath(dir, wantOutputName, []byte(wantOutputContent), zbstore.References{})
	if err != nil {
		t.Fatal(err)
	}
	checkSingleFileOutput(t, drvPath, wantOutputPath, []byte(wantOutputContent), got)
}

func TestRealizeReuse(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()
	dir := backendtest.NewStoreDirectory(t)

	const inputContent = "Hello, World!\n"
	exportBuffer := new(bytes.Buffer)
	exporter := zbstore.NewExportWriter(exportBuffer)
	inputFilePath, _, err := storetest.ExportSourceFile(exporter, []byte(inputContent), storetest.SourceExportOptions{
		Name:      "hello.txt",
		Directory: dir,
	})
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
		InputSources: *sets.NewSorted(
			inputFilePath,
		),
		Outputs: map[string]*zbstore.DerivationOutputType{
			zbstore.DefaultDerivationOutputName: zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
		},
	}
	drvContent.Builder, drvContent.Args = catcatBuilder()
	drvPath, _, err := storetest.ExportDerivation(exporter, drvContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := exporter.Close(); err != nil {
		t.Fatal(err)
	}

	_, client, err := backendtest.NewServer(ctx, t, dir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	codec, releaseCodec, err := storeCodec(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	err = codec.Export(nil, exportBuffer)
	releaseCodec()
	if err != nil {
		t.Fatal(err)
	}

	realize1Response := new(zbstorerpc.RealizeResponse)
	err = jsonrpc.Do(ctx, client, zbstorerpc.RealizeMethod, realize1Response, &zbstorerpc.RealizeRequest{
		DrvPaths: []zbstore.Path{drvPath},
	})
	if err != nil {
		t.Fatal("first RPC error:", err)
	}
	if _, err := backendtest.WaitForSuccessfulBuild(ctx, client, realize1Response.BuildID); err != nil {
		gotLog, _ := backendtest.ReadLog(ctx, client, realize1Response.BuildID, drvPath)
		t.Fatalf("first build failed: %v\nlog:\n%s", err, gotLog)
	}

	realize2Response := new(zbstorerpc.RealizeResponse)
	err = jsonrpc.Do(ctx, client, zbstorerpc.RealizeMethod, realize2Response, &zbstorerpc.RealizeRequest{
		DrvPaths: []zbstore.Path{drvPath},
	})
	if err != nil {
		t.Fatal("second RPC error:", err)
	}
	got, err := backendtest.WaitForSuccessfulBuild(ctx, client, realize2Response.BuildID)
	if err != nil {
		t.Error("second build failed:", err)
	}

	gotLog, err := backendtest.ReadLog(ctx, client, realize2Response.BuildID, drvPath)
	if err != nil {
		t.Error("accessing second build's logs:", err)
	}
	if want := ""; string(gotLog) != want {
		t.Errorf("build log:\n%s\n(want %q)", gotLog, want)
	}

	const wantOutputContent = "Hello, World!\nHello, World!\n"
	wantOutputPath, err := singleFileOutputPath(dir, wantOutputName, []byte(wantOutputContent), zbstore.References{})
	if err != nil {
		t.Fatal(err)
	}
	checkSingleFileOutput(t, drvPath, wantOutputPath, []byte(wantOutputContent), got)
}

func TestRealizeMultiStep(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()
	dir := backendtest.NewStoreDirectory(t)

	const inputContent = "Hello, World!\n"
	exportBuffer := new(bytes.Buffer)
	exporter := zbstore.NewExportWriter(exportBuffer)
	inputFilePath, _, err := storetest.ExportSourceFile(exporter, []byte(inputContent), storetest.SourceExportOptions{
		Name:      "hello.txt",
		Directory: dir,
	})
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
		InputSources: *sets.NewSorted(
			inputFilePath,
		),
		Outputs: map[string]*zbstore.DerivationOutputType{
			zbstore.DefaultDerivationOutputName: zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
		},
	}
	drv1Content.Builder, drv1Content.Args = catcatBuilder()
	drv1Path, _, err := storetest.ExportDerivation(exporter, drv1Content)
	if err != nil {
		t.Fatal(err)
	}
	const wantOutputName = "hello4.txt"
	drv2Content := &zbstore.Derivation{
		Name:   "hello4.txt",
		Dir:    dir,
		System: system.Current().String(),
		Env: map[string]string{
			"in": zbstore.UnknownCAOutputPlaceholder(zbstore.OutputReference{
				DrvPath:    drv1Path,
				OutputName: zbstore.DefaultDerivationOutputName,
			}),
			"out": zbstore.HashPlaceholder("out"),
		},
		InputDerivations: map[zbstore.Path]*sets.Sorted[string]{
			drv1Path: sets.NewSorted(zbstore.DefaultDerivationOutputName),
		},
		Outputs: map[string]*zbstore.DerivationOutputType{
			zbstore.DefaultDerivationOutputName: zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
		},
	}
	drv2Content.Builder, drv2Content.Args = catcatBuilder()
	drv2Path, _, err := storetest.ExportDerivation(exporter, drv2Content)
	if err != nil {
		t.Fatal(err)
	}
	if err := exporter.Close(); err != nil {
		t.Fatal(err)
	}

	_, client, err := backendtest.NewServer(ctx, t, dir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	codec, releaseCodec, err := storeCodec(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	err = codec.Export(nil, exportBuffer)
	releaseCodec()
	if err != nil {
		t.Fatal(err)
	}

	realizeResponse := new(zbstorerpc.RealizeResponse)
	err = jsonrpc.Do(ctx, client, zbstorerpc.RealizeMethod, realizeResponse, &zbstorerpc.RealizeRequest{
		DrvPaths: []zbstore.Path{drv2Path},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := backendtest.WaitForSuccessfulBuild(ctx, client, realizeResponse.BuildID)
	if err != nil {
		gotLog1, _ := backendtest.ReadLog(ctx, client, realizeResponse.BuildID, drv1Path)
		gotLog2, _ := backendtest.ReadLog(ctx, client, realizeResponse.BuildID, drv2Path)
		t.Fatalf("build failed: %v\ndrv1 log:\n%s\ndrv2 log:\n%s", err, gotLog1, gotLog2)
	}

	wantOutputContent := strings.Repeat(inputContent, 4)
	wantOutputPath, err := singleFileOutputPath(dir, wantOutputName, []byte(wantOutputContent), zbstore.References{})
	if err != nil {
		t.Fatal(err)
	}
	checkSingleFileOutput(t, drv2Path, wantOutputPath, []byte(wantOutputContent), got)
}

func TestRealizeReferenceToDep(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()
	dir := backendtest.NewStoreDirectory(t)

	const inputContent = "Hello, World!\n"
	exportBuffer := new(bytes.Buffer)
	exporter := zbstore.NewExportWriter(exportBuffer)
	inputFilePath, _, err := storetest.ExportSourceFile(exporter, []byte(inputContent), storetest.SourceExportOptions{
		Name:      "hello.txt",
		Directory: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	const drv1OutputName = "hello2.txt"
	drv1Content := &zbstore.Derivation{
		Name:   drv1OutputName,
		Dir:    dir,
		System: system.Current().String(),
		Env: map[string]string{
			"in":  string(inputFilePath),
			"out": zbstore.HashPlaceholder("out"),
		},
		InputSources: *sets.NewSorted(
			inputFilePath,
		),
		Outputs: map[string]*zbstore.DerivationOutputType{
			zbstore.DefaultDerivationOutputName: zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
		},
	}
	drv1Content.Builder, drv1Content.Args = catcatBuilder()
	drv1Path, _, err := storetest.ExportDerivation(exporter, drv1Content)
	if err != nil {
		t.Fatal(err)
	}
	const wantOutputName = "hello-ref.txt"
	drv2Content := &zbstore.Derivation{
		Name:   wantOutputName,
		Dir:    dir,
		System: system.Current().String(),
		Env: map[string]string{
			"in": zbstore.UnknownCAOutputPlaceholder(zbstore.OutputReference{
				DrvPath:    drv1Path,
				OutputName: zbstore.DefaultDerivationOutputName,
			}),
			"out": zbstore.HashPlaceholder("out"),
		},
		InputDerivations: map[zbstore.Path]*sets.Sorted[string]{
			drv1Path: sets.NewSorted(zbstore.DefaultDerivationOutputName),
		},
		Outputs: map[string]*zbstore.DerivationOutputType{
			zbstore.DefaultDerivationOutputName: zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
		},
	}
	if runtime.GOOS == "windows" {
		drv2Content.Builder = powershellPath
		drv2Content.Args = []string{
			"-Command",
			"(${env:in} + \"`n\")" + ` | Out-File -NoNewline -Encoding ascii -FilePath ${env:out}`,
		}
	} else {
		drv2Content.Builder = shPath
		drv2Content.Args = []string{
			"-c",
			`echo "$in" > "$out"`,
		}
	}
	drv2Path, _, err := storetest.ExportDerivation(exporter, drv2Content)
	if err != nil {
		t.Fatal(err)
	}
	if err := exporter.Close(); err != nil {
		t.Fatal(err)
	}

	_, client, err := backendtest.NewServer(ctx, t, dir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	codec, releaseCodec, err := storeCodec(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	err = codec.Export(nil, exportBuffer)
	releaseCodec()
	if err != nil {
		t.Fatal(err)
	}

	realizeResponse := new(zbstorerpc.RealizeResponse)
	err = jsonrpc.Do(ctx, client, zbstorerpc.RealizeMethod, realizeResponse, &zbstorerpc.RealizeRequest{
		DrvPaths: []zbstore.Path{drv2Path},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := backendtest.WaitForSuccessfulBuild(ctx, client, realizeResponse.BuildID)
	if err != nil {
		gotLog1, _ := backendtest.ReadLog(ctx, client, realizeResponse.BuildID, drv1Path)
		gotLog2, _ := backendtest.ReadLog(ctx, client, realizeResponse.BuildID, drv2Path)
		t.Fatalf("build failed: %v\ndrv1 log:\n%s\ndrv2 log:\n%s", err, gotLog1, gotLog2)
	}

	drv1OutputContent := strings.Repeat(inputContent, 2)
	drv1OutputPath, err := singleFileOutputPath(dir, drv1OutputName, []byte(drv1OutputContent), zbstore.References{})
	if err != nil {
		t.Fatal(err)
	}

	wantOutputContent := append([]byte(drv1OutputPath), '\n')
	wantOutputPath, err := singleFileOutputPath(dir, wantOutputName, wantOutputContent, zbstore.References{
		Others: *sets.NewSorted(
			drv1OutputPath,
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	checkSingleFileOutput(t, drv2Path, wantOutputPath, wantOutputContent, got)

	var info zbstorerpc.InfoResponse
	err = jsonrpc.Do(ctx, client, zbstorerpc.InfoMethod, &info, &zbstorerpc.InfoRequest{
		Path: wantOutputPath,
	})
	if err != nil {
		t.Error(err)
	} else {
		buf := new(bytes.Buffer)
		if err := storetest.SingleFileNAR(buf, wantOutputContent); err != nil {
			t.Fatal(err)
		}
		narData := buf.Bytes()
		ca, _, err := zbstore.SourceSHA256ContentAddress(bytes.NewReader(narData), nil)
		if err != nil {
			t.Fatal(err)
		}

		want := wantObjectInfo(info.Info, narData, ca, sets.NewSorted(
			drv1OutputPath,
		))
		if diff := cmp.Diff(want, info.Info); diff != "" {
			t.Errorf("info (-want +got):\n%s", diff)
		}
	}
}

func TestRealizeSelfReference(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()
	dir := backendtest.NewStoreDirectory(t)

	const inputContent = "Hello, World!\n"
	exportBuffer := new(bytes.Buffer)
	exporter := zbstore.NewExportWriter(exportBuffer)
	const wantOutputName = "self.txt"
	drvContent := &zbstore.Derivation{
		Name:   wantOutputName,
		Dir:    dir,
		System: system.Current().String(),
		Env: map[string]string{
			"out": zbstore.HashPlaceholder("out"),
		},
		Outputs: map[string]*zbstore.DerivationOutputType{
			zbstore.DefaultDerivationOutputName: zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
		},
	}
	if runtime.GOOS == "windows" {
		drvContent.Builder = powershellPath
		drvContent.Args = []string{"-Command", "\"${env:out}`n\" | Out-File -NoNewline -Encoding ascii -FilePath ${env:out}"}
	} else {
		drvContent.Builder = shPath
		drvContent.Args = []string{"-c", `echo "$out" > "$out"`}
	}
	drvPath, _, err := storetest.ExportDerivation(exporter, drvContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := exporter.Close(); err != nil {
		t.Fatal(err)
	}

	_, client, err := backendtest.NewServer(ctx, t, dir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	codec, releaseCodec, err := storeCodec(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	err = codec.Export(nil, exportBuffer)
	releaseCodec()
	if err != nil {
		t.Fatal(err)
	}

	realizeResponse := new(zbstorerpc.RealizeResponse)
	err = jsonrpc.Do(ctx, client, zbstorerpc.RealizeMethod, realizeResponse, &zbstorerpc.RealizeRequest{
		DrvPaths: []zbstore.Path{drvPath},
	})
	if err != nil {
		t.Fatal("RPC error:", err)
	}
	if realizeResponse.BuildID == "" {
		t.Fatal("no build ID returned")
	}
	got, err := backendtest.WaitForSuccessfulBuild(ctx, client, realizeResponse.BuildID)
	if err != nil {
		t.Fatal(err)
	}

	const fakeDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	contentModuloHash := new(bytes.Buffer)
	if err := storetest.SingleFileNAR(contentModuloHash, []byte(dir.Join(fakeDigest+"-"+wantOutputName)+"\n")); err != nil {
		t.Fatal(err)
	}
	ca, _, err := zbstore.SourceSHA256ContentAddress(contentModuloHash, &zbstore.ContentAddressOptions{
		Digest: fakeDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantOutputPath, err := zbstore.FixedCAOutputPath(dir, wantOutputName, ca, zbstore.References{Self: true})
	if err != nil {
		t.Fatal(err)
	}
	wantOutputContent := dir.Join(wantOutputPath.Digest()+"-"+wantOutputName) + "\n"

	checkSingleFileOutput(t, drvPath, wantOutputPath, []byte(wantOutputContent), got)
}

func TestRealizeFixed(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()
	dir := backendtest.NewStoreDirectory(t)

	exportBuffer := new(bytes.Buffer)
	exporter := zbstore.NewExportWriter(exportBuffer)
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
		Outputs: map[string]*zbstore.DerivationOutputType{
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
	drv1Path, _, err := storetest.ExportDerivation(exporter, drv1Content)
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
		Outputs: map[string]*zbstore.DerivationOutputType{
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
	drv2Path, _, err := storetest.ExportDerivation(exporter, drv2Content)
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

	_, client, err := backendtest.NewServer(ctx, t, dir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	codec, releaseCodec, err := storeCodec(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	err = codec.Export(nil, exportBuffer)
	releaseCodec()
	if err != nil {
		t.Fatal(err)
	}

	realize1Response := new(zbstorerpc.RealizeResponse)
	err = jsonrpc.Do(ctx, client, zbstorerpc.RealizeMethod, realize1Response, &zbstorerpc.RealizeRequest{
		DrvPaths: []zbstore.Path{drv1Path},
	})
	if err != nil {
		t.Fatal("build drv1:", err)
	}
	got1, err := backendtest.WaitForSuccessfulBuild(ctx, client, realize1Response.BuildID)
	if err != nil {
		gotLog, _ := backendtest.ReadLog(ctx, client, realize1Response.BuildID, drv1Path)
		t.Fatalf("build drv1: %v\nlog:\n%s", err, gotLog)
	}
	checkSingleFileOutput(t, drv1Path, wantOutputPath, []byte(wantOutputContent), got1)

	// Now let's build the second derivation to see whether the output gets reused.
	realize2Response := new(zbstorerpc.RealizeResponse)
	err = jsonrpc.Do(ctx, client, zbstorerpc.RealizeMethod, realize2Response, &zbstorerpc.RealizeRequest{
		DrvPaths: []zbstore.Path{drv2Path},
	})
	if err != nil {
		t.Fatal("build drv2:", err)
	}
	got2, err := backendtest.WaitForSuccessfulBuild(ctx, client, realize2Response.BuildID)
	if err != nil {
		gotLog, _ := backendtest.ReadLog(ctx, client, realize2Response.BuildID, drv2Path)
		t.Fatalf("build drv2: %v\nlog:\n%s", err, gotLog)
	}
	checkSingleFileOutput(t, drv2Path, wantOutputPath, []byte(wantOutputContent), got2)
}

func TestRealizeFailure(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()
	dir := backendtest.NewStoreDirectory(t)

	exportBuffer := new(bytes.Buffer)
	exporter := zbstore.NewExportWriter(exportBuffer)
	const drvName = "hello.txt"
	// Create a derivation that fails after creating its output.
	drvContent := &zbstore.Derivation{
		Name:   drvName,
		Dir:    dir,
		System: system.Current().String(),
		Env: map[string]string{
			"out": zbstore.HashPlaceholder("out"),
		},
		Outputs: map[string]*zbstore.DerivationOutputType{
			zbstore.DefaultDerivationOutputName: zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
		},
	}
	if runtime.GOOS == "windows" {
		drvContent.Builder = powershellPath
		drvContent.Args = []string{"-Command", "New-Item ${env:out} -type file ; exit 1"}
	} else {
		drvContent.Builder = shPath
		drvContent.Args = []string{"-c", "echo > $out ; exit 1"}
	}
	drvPath, _, err := storetest.ExportDerivation(exporter, drvContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := exporter.Close(); err != nil {
		t.Fatal(err)
	}

	_, client, err := backendtest.NewServer(ctx, t, dir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	codec, releaseCodec, err := storeCodec(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	err = codec.Export(nil, exportBuffer)
	releaseCodec()
	if err != nil {
		t.Fatal(err)
	}

	realizeResponse := new(zbstorerpc.RealizeResponse)
	err = jsonrpc.Do(ctx, client, zbstorerpc.RealizeMethod, realizeResponse, &zbstorerpc.RealizeRequest{
		DrvPaths: []zbstore.Path{drvPath},
	})
	if err != nil {
		t.Fatal("build drv:", err)
	}
	got, err := backendtest.WaitForBuild(ctx, client, realizeResponse.BuildID)
	if err != nil {
		t.Fatal("build drv:", err)
	}
	want := &zbstorerpc.Build{
		ID:     realizeResponse.BuildID,
		Status: zbstorerpc.BuildFail,
		Results: []*zbstorerpc.BuildResult{
			{
				DrvPath: drvPath,
				Status:  zbstorerpc.BuildFail,
				Outputs: []*zbstorerpc.RealizeOutput{
					{
						Name: zbstore.DefaultDerivationOutputName,
					},
				},
			},
		},
	}
	buildType := reflect.TypeFor[zbstorerpc.Build]()
	diff := cmp.Diff(
		want, got,
		cmp.FilterPath(
			func(p cmp.Path) bool {
				if p.Index(-2).Type() != buildType {
					return false
				}
				fieldName := p.Last().(cmp.StructField).Name()
				return fieldName == "StartedAt" ||
					fieldName == "EndedAt"
			},
			cmp.Ignore(),
		),
		buildResultOption,
	)
	if diff != "" {
		t.Errorf("build (-want +got):\n%s", diff)
	}
	if !got.EndedAt.Valid {
		t.Error("build.endedAt = null")
	}
	// Ensure that the build didn't leave files in the store.
	storeListing, err := os.ReadDir(string(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range storeListing {
		name := ent.Name()
		if name != drvPath.Base() {
			t.Errorf("unknown object %s left in store", name)
		}
	}

	if gotLog, err := backendtest.ReadLog(ctx, client, realizeResponse.BuildID, drvPath); err != nil {
		t.Error(err)
	} else if want := "exit status"; !bytes.Contains(gotLog, []byte(want)) {
		t.Errorf("Log does not contain phrase %q. Full output:\n%s", want, gotLog)
	}
}

func TestRealizeNoOutput(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()
	dir := backendtest.NewStoreDirectory(t)

	exportBuffer := new(bytes.Buffer)
	exporter := zbstore.NewExportWriter(exportBuffer)
	const drvName = "hello.txt"
	// Create a derivation that no-ops.
	drvContent := &zbstore.Derivation{
		Name:   drvName,
		Dir:    dir,
		System: system.Current().String(),
		Env: map[string]string{
			"out": zbstore.HashPlaceholder("out"),
		},
		Outputs: map[string]*zbstore.DerivationOutputType{
			zbstore.DefaultDerivationOutputName: zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
		},
	}
	if runtime.GOOS == "windows" {
		drvContent.Builder = powershellPath
		drvContent.Args = []string{"-Command", "exit"}
	} else {
		drvContent.Builder = shPath
		drvContent.Args = []string{"-c", "true"}
	}
	drvPath, _, err := storetest.ExportDerivation(exporter, drvContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := exporter.Close(); err != nil {
		t.Fatal(err)
	}

	_, client, err := backendtest.NewServer(ctx, t, dir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	codec, releaseCodec, err := storeCodec(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	err = codec.Export(nil, exportBuffer)
	releaseCodec()
	if err != nil {
		t.Fatal(err)
	}

	realizeResponse := new(zbstorerpc.RealizeResponse)
	err = jsonrpc.Do(ctx, client, zbstorerpc.RealizeMethod, realizeResponse, &zbstorerpc.RealizeRequest{
		DrvPaths: []zbstore.Path{drvPath},
	})
	if err != nil {
		t.Fatal("build drv:", err)
	}
	got, err := backendtest.WaitForBuild(ctx, client, realizeResponse.BuildID)
	if err != nil {
		t.Fatal("build drv:", err)
	}
	want := &zbstorerpc.Build{
		ID:     realizeResponse.BuildID,
		Status: zbstorerpc.BuildFail,
		Results: []*zbstorerpc.BuildResult{
			{
				DrvPath: drvPath,
				Status:  zbstorerpc.BuildFail,
				Outputs: []*zbstorerpc.RealizeOutput{
					{
						Name: zbstore.DefaultDerivationOutputName,
					},
				},
			},
		},
	}
	buildType := reflect.TypeFor[zbstorerpc.Build]()
	diff := cmp.Diff(
		want, got,
		cmp.FilterPath(
			func(p cmp.Path) bool {
				if p.Index(-2).Type() != buildType {
					return false
				}
				fieldName := p.Last().(cmp.StructField).Name()
				return fieldName == "StartedAt" ||
					fieldName == "EndedAt"
			},
			cmp.Ignore(),
		),
		buildResultOption,
	)
	if diff != "" {
		t.Errorf("build (-want +got):\n%s", diff)
	}
	if !got.EndedAt.Valid {
		t.Error("build.endedAt = null")
	}
	// Ensure that the build didn't leave files in the store.
	storeListing, err := os.ReadDir(string(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range storeListing {
		name := ent.Name()
		if name != drvPath.Base() {
			t.Errorf("unknown object %s left in store", name)
		}
	}

	if gotLog, err := backendtest.ReadLog(ctx, client, realizeResponse.BuildID, drvPath); err != nil {
		t.Error(err)
	} else if want := "failed to produce output $out"; !bytes.Contains(gotLog, []byte(want)) {
		t.Errorf("Log does not contain phrase %q. Full output:\n%s", want, gotLog)
	}
}

func TestRealizeCores(t *testing.T) {
	tests := []int{1, 2}
	for _, n := range tests {
		t.Run(fmt.Sprintf("N%d", n), func(t *testing.T) {
			ctx, cancel := testcontext.New(t)
			defer cancel()
			dir := backendtest.NewStoreDirectory(t)

			exportBuffer := new(bytes.Buffer)
			exporter := zbstore.NewExportWriter(exportBuffer)
			const drvName = "cores.txt"
			// Create a derivation that fails after creating its output.
			drvContent := &zbstore.Derivation{
				Name:   drvName,
				Dir:    dir,
				System: system.Current().String(),
				Env: map[string]string{
					"out": zbstore.HashPlaceholder("out"),
				},
				Outputs: map[string]*zbstore.DerivationOutputType{
					zbstore.DefaultDerivationOutputName: zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
				},
			}
			if runtime.GOOS == "windows" {
				drvContent.Builder = powershellPath
				drvContent.Args = []string{"-Command", "\"${env:ZB_BUILD_CORES}`n\" | Out-File -NoNewline -Encoding ascii -FilePath ${env:out}"}
			} else {
				drvContent.Builder = shPath
				drvContent.Args = []string{"-c", `echo "$ZB_BUILD_CORES" > "$out"`}
			}
			drvPath, _, err := storetest.ExportDerivation(exporter, drvContent)
			if err != nil {
				t.Fatal(err)
			}
			if err := exporter.Close(); err != nil {
				t.Fatal(err)
			}

			_, client, err := backendtest.NewServer(ctx, t, dir, &backendtest.Options{
				TempDir: t.TempDir(),
				Options: Options{
					CoresPerBuild: n,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			codec, releaseCodec, err := storeCodec(ctx, client)
			if err != nil {
				t.Fatal(err)
			}
			err = codec.Export(nil, exportBuffer)
			releaseCodec()
			if err != nil {
				t.Fatal(err)
			}

			realizeResponse := new(zbstorerpc.RealizeResponse)
			err = jsonrpc.Do(ctx, client, zbstorerpc.RealizeMethod, realizeResponse, &zbstorerpc.RealizeRequest{
				DrvPaths: []zbstore.Path{drvPath},
			})
			if err != nil {
				t.Fatal("build drv:", err)
			}
			got, err := backendtest.WaitForSuccessfulBuild(ctx, client, realizeResponse.BuildID)
			if err != nil {
				gotLog, _ := backendtest.ReadLog(ctx, client, realizeResponse.BuildID, drvPath)
				t.Fatalf("build drv: %v\nlog:\n%s", err, gotLog)
			}
			wantOutputContent := fmt.Sprintf("%d\n", n)
			wantOutputPath, err := singleFileOutputPath(dir, drvName, []byte(wantOutputContent), zbstore.References{})
			if err != nil {
				t.Fatal(err)
			}
			checkSingleFileOutput(t, drvPath, wantOutputPath, []byte(wantOutputContent), got)
		})
	}
}

func TestRealizeFetchURL(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()
	dir := backendtest.NewStoreDirectory(t)

	const fileContent = "Hello, World!\n"
	mux := http.NewServeMux()
	mux.HandleFunc("/hello.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "hello.txt", time.Time{}, strings.NewReader(fileContent))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	exportBuffer := new(bytes.Buffer)
	exporter := zbstore.NewExportWriter(exportBuffer)
	const wantOutputName = "hello.txt"
	wantOutputCA := nix.FlatFileContentAddress(mustParseHash(t, "sha256:c98c24b677eff44860afea6f493bbaec5bb1c4cbb209c6fc2bbb47f66ff2ad31"))
	drvContent := &zbstore.Derivation{
		Name:    wantOutputName,
		Dir:     dir,
		Builder: "builtin:fetchurl",
		System:  "builtin",
		Env: map[string]string{
			"url": string(srv.URL + "/hello.txt"),
			"out": zbstore.HashPlaceholder("out"),
		},
		Outputs: map[string]*zbstore.DerivationOutputType{
			zbstore.DefaultDerivationOutputName: zbstore.FixedCAOutput(wantOutputCA),
		},
	}
	drvPath, _, err := storetest.ExportDerivation(exporter, drvContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := exporter.Close(); err != nil {
		t.Fatal(err)
	}

	_, client, err := backendtest.NewServer(ctx, t, dir, &backendtest.Options{
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	codec, releaseCodec, err := storeCodec(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	err = codec.Export(nil, exportBuffer)
	releaseCodec()
	if err != nil {
		t.Fatal(err)
	}

	realizeResponse := new(zbstorerpc.RealizeResponse)
	err = jsonrpc.Do(ctx, client, zbstorerpc.RealizeMethod, realizeResponse, &zbstorerpc.RealizeRequest{
		DrvPaths: []zbstore.Path{drvPath},
	})
	if err != nil {
		t.Fatal("build drv:", err)
	}
	got, err := backendtest.WaitForSuccessfulBuild(ctx, client, realizeResponse.BuildID)
	if err != nil {
		gotLog, _ := backendtest.ReadLog(ctx, client, realizeResponse.BuildID, drvPath)
		t.Fatalf("build drv: %v\nlog:\n%s", err, gotLog)
	}

	wantOutputPath, err := zbstore.FixedCAOutputPath(dir, wantOutputName, wantOutputCA, zbstore.References{})
	if err != nil {
		t.Fatal(err)
	}
	checkSingleFileOutput(t, drvPath, wantOutputPath, []byte(fileContent), got)
}

var buildResultOption = cmp.Options{
	cmp.FilterPath(func(p cmp.Path) bool {
		if p.Index(-2).Type() != reflect.TypeFor[zbstorerpc.BuildResult]() {
			return false
		}
		name := p.Last().(cmp.StructField).Name()
		return name == "LogSize"
	}, cmp.Ignore()),
}

func checkSingleFileOutput(tb testing.TB, drvPath, wantOutputPath zbstore.Path, wantOutputContent []byte, resp *zbstorerpc.Build) {
	tb.Helper()

	got, err := resp.ResultForPath(drvPath)
	if err != nil {
		tb.Error(err)
	}
	if got == nil {
		return
	}

	want := &zbstorerpc.BuildResult{
		DrvPath: drvPath,
		Status:  zbstorerpc.BuildSuccess,
		Outputs: []*zbstorerpc.RealizeOutput{
			{
				Name: zbstore.DefaultDerivationOutputName,
				Path: zbstorerpc.NonNull(wantOutputPath),
			},
		},
	}
	diff := cmp.Diff(want, got, buildResultOption)
	if diff != "" {
		tb.Errorf("realize response (-want +got):\n%s", diff)
	}

	// Try to compare the file if the response is the right shape.
	gotOutput, err := got.OutputForName(zbstore.DefaultDerivationOutputName)
	if err != nil {
		tb.Errorf("%s: %v", drvPath, err)
		return
	}
	if !gotOutput.Path.Valid {
		tb.Errorf("%s: output %s path is null", drvPath, zbstore.DefaultDerivationOutputName)
		return
	}
	gotOutputPath := gotOutput.Path.X

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

// catcatBuilder returns a builder that writes $in twice to $out
// with no dependencies other than the system shell.
// As a side-effect, it echoes "catcat" to its log to signal its execution.
func catcatBuilder() (builder string, builderArgs []string) {
	if runtime.GOOS == "windows" {
		return powershellPath, []string{
			"-Command",
			`Write-Output "catcat" ; $x = Get-Content -Raw ${env:in} ; ($x + $x) | Out-File -NoNewline -Encoding ascii -FilePath ${env:out}`,
		}
	}
	return shPath, []string{
		"-c",
		`echo catcat >&2 ; while read line; do echo "$line"; echo "$line"; done < $in > $out`,
	}
}

func singleFileOutputPath(dir zbstore.Directory, name string, data []byte, refs zbstore.References) (zbstore.Path, error) {
	wantOutputNAR := new(bytes.Buffer)
	if err := storetest.SingleFileNAR(wantOutputNAR, []byte(data)); err != nil {
		return "", err
	}
	ca, _, err := zbstore.SourceSHA256ContentAddress(bytes.NewReader(wantOutputNAR.Bytes()), nil)
	if err != nil {
		return "", err
	}
	p, err := zbstore.FixedCAOutputPath(dir, name, ca, refs)
	if err != nil {
		return "", err
	}
	return p, nil
}

func mustParseHash(tb testing.TB, s string) nix.Hash {
	tb.Helper()
	h, err := nix.ParseHash(s)
	if err != nil {
		tb.Fatal(err)
	}
	return h
}
