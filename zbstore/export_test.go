// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package zbstore

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"zb.256lights.llc/pkg/sets"
	"zombiezen.com/go/nix/nar"
)

func TestExportWriter(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		got := new(bytes.Buffer)
		ew := NewExportWriter(got)
		if err := ew.Close(); err != nil {
			t.Error("Close:", err)
		}
		verifyExport(t, got.Bytes())
	})

	t.Run("SingleObject", func(t *testing.T) {
		const fileContent = "Hello, World!\n"

		got := new(bytes.Buffer)
		ew := NewExportWriter(got)
		nw := nar.NewWriter(ew)
		err := nw.WriteHeader(&nar.Header{
			Size: int64(len(fileContent)),
		})
		if err != nil {
			t.Error("write NAR header:", err)
		}
		if _, err := nw.Write([]byte(fileContent)); err != nil {
			t.Error("write NAR file content:", err)
		}
		if err := nw.Close(); err != nil {
			t.Error("close NAR:", err)
		}
		err = ew.Trailer(&ExportTrailer{
			StorePath:      "/opt/zb/store/mv4z5c5znjdnc40fvqfl1qknszgbdyxd-hello.txt",
			ContentAddress: mustParseContentAddress(t, "fixed:r:sha256:1qicirpsz48j7a2r5h9lj04kipdyvxanwglv9ymfq0qsv7isywdf"),
		})
		if err != nil {
			t.Error("write trailer:", err)
		}
		if err := ew.Close(); err != nil {
			t.Error("Close:", err)
		}

		verifyExport(t, got.Bytes())
	})

	t.Run("WriteObject", func(t *testing.T) {
		const fileContent = "Hello, World!\n"

		got := new(bytes.Buffer)
		ew := NewExportWriter(got)
		err := ew.WriteObject(t.Context(), &fakeObject{
			nar: singleFileNAR(t, []byte(fileContent)),
			info: ObjectInfo{
				StorePath:      "/opt/zb/store/mv4z5c5znjdnc40fvqfl1qknszgbdyxd-hello.txt",
				ContentAddress: mustParseContentAddress(t, "fixed:r:sha256:1qicirpsz48j7a2r5h9lj04kipdyvxanwglv9ymfq0qsv7isywdf"),
			},
		})
		if err != nil {
			t.Error("WriteObject:", err)
		}
		if err := ew.Close(); err != nil {
			t.Error("Close:", err)
		}

		verifyExport(t, got.Bytes())
	})

	t.Run("TrailerWithReference", func(t *testing.T) {
		const file1Content = "Hello, World!\n"
		const file1Path Path = "/opt/zb/store/mv4z5c5znjdnc40fvqfl1qknszgbdyxd-hello.txt"
		const file2Content = "I have a ref!\n" + string(file1Path) + "\n"

		got := new(bytes.Buffer)
		ew := NewExportWriter(got)

		nw := nar.NewWriter(ew)
		err := nw.WriteHeader(&nar.Header{
			Size: int64(len(file1Content)),
		})
		if err != nil {
			t.Error("write NAR header 1:", err)
		}
		if _, err := nw.Write([]byte(file1Content)); err != nil {
			t.Error("write NAR 1 file content:", err)
		}
		if err := nw.Close(); err != nil {
			t.Error("close NAR 1:", err)
		}
		err = ew.Trailer(&ExportTrailer{
			StorePath:      file1Path,
			ContentAddress: mustParseContentAddress(t, "fixed:r:sha256:1qicirpsz48j7a2r5h9lj04kipdyvxanwglv9ymfq0qsv7isywdf"),
		})
		if err != nil {
			t.Error("write trailer:", err)
		}

		nw = nar.NewWriter(ew)
		err = nw.WriteHeader(&nar.Header{
			Size: int64(len(file2Content)),
		})
		if err != nil {
			t.Error("write NAR 2 header:", err)
		}
		if _, err := nw.Write([]byte(file2Content)); err != nil {
			t.Error("write NAR 2 file content:", err)
		}
		if err := nw.Close(); err != nil {
			t.Error("close NAR 2:", err)
		}

		err = ew.Trailer(&ExportTrailer{
			StorePath:      "/opt/zb/store/cpkdqrc1w4kaklx7881c8c4lw46dzrf7-ref.txt",
			References:     *sets.NewSorted(file1Path),
			ContentAddress: mustParseContentAddress(t, "fixed:r:sha256:0zxvgbzvvxrxx23h50afmv5rzl5xgkbh20rss0xd5319wrd8511y"),
		})
		if err != nil {
			t.Error("write trailer:", err)
		}

		if err := ew.Close(); err != nil {
			t.Error("Close:", err)
		}

		verifyExport(t, got.Bytes())
	})
}

func verifyExport(tb testing.TB, got []byte) {
	tb.Helper()

	artifactDir, err := filepath.Abs(tb.ArtifactDir())
	if err != nil {
		tb.Fatal(err)
	}
	artifactPath := filepath.Join(artifactDir, "got.out")

	wantPath := filepath.Join("testdata", filepath.FromSlash(tb.Name())+".out")
	wantPath, err = filepath.Abs(wantPath)
	if err != nil {
		tb.Fatal(err)
	}
	want, err := os.ReadFile(wantPath)
	if errors.Is(err, os.ErrNotExist) {
		os.WriteFile(artifactPath, got, 0o666)
		tb.Errorf("%s does not exist. To fix: cp '%s' '%s'", wantPath, artifactPath, wantPath)
		return
	}
	if err != nil {
		tb.Fatal(err)
	}

	if !bytes.Equal(got, want) {
		os.WriteFile(artifactPath, got, 0o666)
		tb.Errorf("Export data %s did not match %s", artifactPath, wantPath)
	}
}
