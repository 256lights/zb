// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package zbstorehttp

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"zb.256lights.llc/pkg/internal/fileurl"
	"zb.256lights.llc/pkg/internal/testcontext"
	"zb.256lights.llc/pkg/zbstore"
)

func TestStorePutObject(t *testing.T) {
	t.Run("Create", func(t *testing.T) {
		ctx := testcontext.New(t)

		narData, err := os.ReadFile(testdataPath(t, "hello.txt.nar"))
		if err != nil {
			t.Fatal(err)
		}
		ca, _, err := zbstore.SourceSHA256ContentAddress(bytes.NewReader(narData), nil)
		if err != nil {
			t.Fatal(err)
		}
		objectPath, err := zbstore.FixedCAOutputPath(zbstore.DefaultUnixDirectory, "hello.txt", ca, zbstore.References{})
		if err != nil {
			t.Fatal(err)
		}
		const wantDigest = "mv4z5c5znjdnc40fvqfl1qknszgbdyxd"
		if got := objectPath.Digest(); got != wantDigest {
			t.Errorf("computed store path = %s; want digest of %s", objectPath, wantDigest)
		}
		wantNARInfo, err := os.ReadFile(testdataPath(t, wantDigest+".narinfo"))
		if err != nil {
			t.Fatal(err)
		}

		dir := t.TempDir()
		copyToDir(t, dir, "discovery.json")
		discoveryPath, err := filepath.Abs(filepath.Join(dir, "discovery.json"))
		if err != nil {
			t.Fatal(err)
		}
		discoveryURL := fileurl.FromPath(discoveryPath)
		store := &Store{
			URL: discoveryURL,
			HTTPClient: &http.Client{
				Transport: fileurl.Transport{},
			},
		}

		err = store.PutObject(ctx, &PutObjectRequest{
			StorePath:      objectPath,
			ContentAddress: ca,
			NAR:            bytes.NewReader(narData),
			NARSize:        int64(len(narData)),
		})
		if err != nil {
			t.Error("store.PutObject:", err)
		}

		if got, err := os.ReadFile(filepath.Join(dir, objectPath.Digest()+".narinfo")); err != nil {
			t.Error(err)
		} else if !bytes.Equal(got, wantNARInfo) {
			dst := filepath.Join(t.ArtifactDir(), objectPath.Digest()+".narinfo")
			t.Errorf("%s.narinfo content does not match (wrote to %s)", objectPath.Digest(), dst)
			os.WriteFile(dst, got, 0o666)
		}

		if got, err := os.ReadFile(filepath.Join(dir, "nar", objectPath.Digest()+".nar")); err != nil {
			t.Error(err)
		} else if !bytes.Equal(got, narData) {
			dst := filepath.Join(t.ArtifactDir(), "hello.txt.nar")
			t.Errorf("hello.txt.nar content does not match (wrote to %s)", dst)
			os.WriteFile(dst, got, 0o666)
		}
	})

	t.Run("Existing", func(t *testing.T) {
		ctx := testcontext.New(t)

		narData, err := os.ReadFile(testdataPath(t, "hello.txt.nar"))
		if err != nil {
			t.Fatal(err)
		}
		ca, _, err := zbstore.SourceSHA256ContentAddress(bytes.NewReader(narData), nil)
		if err != nil {
			t.Fatal(err)
		}
		objectPath, err := zbstore.FixedCAOutputPath(zbstore.DefaultUnixDirectory, "hello.txt", ca, zbstore.References{})
		if err != nil {
			t.Fatal(err)
		}
		const wantDigest = "mv4z5c5znjdnc40fvqfl1qknszgbdyxd"
		if got := objectPath.Digest(); got != wantDigest {
			t.Errorf("computed store path = %s; want digest of %s", objectPath, wantDigest)
		}
		wantNARInfo, err := os.ReadFile(testdataPath(t, wantDigest+".narinfo"))
		if err != nil {
			t.Fatal(err)
		}

		dir := t.TempDir()
		copyToDir(t, dir, "discovery.json")
		copyToDir(t, dir, "hello.txt.nar")
		copyToDir(t, dir, wantDigest+".narinfo")
		discoveryPath, err := filepath.Abs(filepath.Join(dir, "discovery.json"))
		if err != nil {
			t.Fatal(err)
		}
		discoveryURL := fileurl.FromPath(discoveryPath)
		store := &Store{
			URL: discoveryURL,
			HTTPClient: &http.Client{
				Transport: fileurl.Transport{},
			},
		}

		err = store.PutObject(ctx, &PutObjectRequest{
			StorePath:      objectPath,
			ContentAddress: ca,
			NAR:            bytes.NewReader(narData),
			NARSize:        int64(len(narData)),
		})
		if err != nil {
			t.Error("store.PutObject:", err)
		}

		if got, err := os.ReadFile(filepath.Join(dir, objectPath.Digest()+".narinfo")); err != nil {
			t.Error(err)
		} else if !bytes.Equal(got, wantNARInfo) {
			dst := filepath.Join(t.ArtifactDir(), objectPath.Digest()+".narinfo")
			t.Errorf("%s.narinfo content does not match (wrote to %s)", objectPath.Digest(), dst)
			os.WriteFile(dst, got, 0o666)
		}
		if _, err := os.Stat(filepath.Join(dir, "upload")); err == nil {
			t.Error("upload directory exists after PutObject")
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Error(err)
		}
	})
}
