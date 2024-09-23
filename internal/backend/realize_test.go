// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/storetest"
	"zb.256lights.llc/pkg/internal/system"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log/testlog"
	"zombiezen.com/go/nix"
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
		InputSources: *sets.NewSorted(
			inputFilePath,
		),
		Outputs: map[string]*zbstore.DerivationOutputType{
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

	logBuffer := new(bytes.Buffer)
	client := newTestServer(t, dir, string(dir), &writerLogger{logBuffer}, nil)
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
	gotLog := bytes.ReplaceAll(logBuffer.Bytes(), []byte("\r\n"), []byte("\n"))
	if err != nil {
		t.Fatalf("RPC error: %v\nlog:\n%s", err, gotLog)
	}

	if want := "catcat\n"; string(gotLog) != want {
		t.Errorf("build log:\n%s\n(want %q)", gotLog, want)
	}
	const wantOutputContent = "Hello, World!\nHello, World!\n"
	wantOutputPath, err := singleFileOutputPath(dir, wantOutputName, []byte(wantOutputContent), zbstore.References{})
	if err != nil {
		t.Fatal(err)
	}
	checkSingleFileOutput(t, wantOutputPath, []byte(wantOutputContent), got)
}

func TestRealizeReuse(t *testing.T) {
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
		InputSources: *sets.NewSorted(
			inputFilePath,
		),
		Outputs: map[string]*zbstore.DerivationOutputType{
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

	logBuffer := new(bytes.Buffer)
	client := newTestServer(t, dir, string(dir), &writerLogger{logBuffer}, nil)
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
		gotLog := bytes.ReplaceAll(logBuffer.Bytes(), []byte("\r\n"), []byte("\n"))
		t.Fatalf("first RPC error: %v\nlog:\n%s", err, gotLog)
	}
	err = jsonrpc.Do(ctx, client, zbstore.RealizeMethod, got, &zbstore.RealizeRequest{
		DrvPath: drvPath,
	})
	gotLog := bytes.ReplaceAll(logBuffer.Bytes(), []byte("\r\n"), []byte("\n"))
	if err != nil {
		t.Fatalf("second RPC error: %v\nlog:\n%s", err, gotLog)
	}

	if want := "catcat\n"; string(gotLog) != want {
		t.Errorf("build log:\n%s\n(want %q)", gotLog, want)
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
		InputSources: *sets.NewSorted(
			inputFilePath,
		),
		Outputs: map[string]*zbstore.DerivationOutputType{
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

func TestRealizeReferenceToDep(t *testing.T) {
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
	drv1Path, err := storetest.ExportDerivation(exporter, drv1Content)
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
	checkSingleFileOutput(t, wantOutputPath, wantOutputContent, got)
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

func TestRealizeFailure(t *testing.T) {
	ctx := testlog.WithTB(context.Background(), t)
	dir, err := zbstore.CleanDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	exportBuffer := new(bytes.Buffer)
	exporter := zbstore.NewExporter(exportBuffer)
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
		t.Fatal("build drv:", err)
	}
	want := &zbstore.RealizeResponse{
		Outputs: []*zbstore.RealizeOutput{
			{
				Name: zbstore.DefaultDerivationOutputName,
			},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("realize response (-want +got):\n%s", diff)
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
}

func TestRealizeFetchURL(t *testing.T) {
	ctx := testlog.WithTB(context.Background(), t)
	dir, err := zbstore.CleanDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	const fileContent = "Hello, World!\n"
	mux := http.NewServeMux()
	mux.HandleFunc("/hello.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "hello.txt", time.Time{}, strings.NewReader(fileContent))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	exportBuffer := new(bytes.Buffer)
	exporter := zbstore.NewExporter(exportBuffer)
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

	wantOutputPath, err := zbstore.FixedCAOutputPath(dir, wantOutputName, wantOutputCA, zbstore.References{})
	if err != nil {
		t.Fatal(err)
	}
	checkSingleFileOutput(t, wantOutputPath, []byte(fileContent), got)
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

type writerLogger struct {
	w io.Writer
}

func (wl *writerLogger) JSONRPC(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	return jsonrpc.ServeMux{
		zbstore.LogMethod: jsonrpc.HandlerFunc(wl.log),
	}.JSONRPC(ctx, req)
}

func (wl *writerLogger) log(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	args := new(zbstore.LogNotification)
	if err := json.Unmarshal(req.Params, args); err != nil {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
	}
	payload := args.Payload()
	if len(payload) == 0 {
		return nil, nil
	}
	_, err := wl.w.Write(payload)
	return nil, err
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
