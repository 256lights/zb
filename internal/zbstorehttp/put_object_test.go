// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package zbstorehttp

import (
	"bytes"
	"cmp"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"zb.256lights.llc/pkg/internal/fileurl"
	"zb.256lights.llc/pkg/internal/testcontext"
	"zb.256lights.llc/pkg/internal/xhttp"
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
			GetNAR: func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(narData)), nil
			},
			NARSize: int64(len(narData)),
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
			GetNAR: func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(narData)), nil
			},
			NARSize: int64(len(narData)),
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

	t.Run("HighPriorityConflict", func(t *testing.T) {
		ctx := testcontext.New(t)

		narData, err := os.ReadFile(testdataPath(t, "nar/hello.txt.nar"))
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
		wantNARInfo, err := os.ReadFile(testdataPath(t, "info1/"+wantDigest+".narinfo"))
		if err != nil {
			t.Fatal(err)
		}

		dir := t.TempDir()
		copyToDir(t, dir, "discovery.json")
		copyToDir(t, dir, "nar/hello.txt.nar")
		copyToDir(t, dir, "info1/"+wantDigest+".narinfo")
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
			GetNAR: func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(narData)), nil
			},
			NARSize: int64(len(narData)),
		})
		if err == nil {
			t.Error("store.PutObject did not return an error", err)
		} else {
			t.Log("PutObject error:", err)
		}

		if got, err := os.ReadFile(filepath.Join(dir, "info1", objectPath.Digest()+".narinfo")); err != nil {
			t.Error(err)
		} else if !bytes.Equal(got, wantNARInfo) {
			dst := filepath.Join(t.ArtifactDir(), objectPath.Digest()+".narinfo")
			t.Errorf("%s.narinfo content modified (wrote to %s)", objectPath.Digest(), dst)
			os.WriteFile(dst, got, 0o666)
		}
		if _, err := os.Stat(filepath.Join(dir, "upload")); err == nil {
			t.Error("upload directory exists after PutObject")
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Error(err)
		}
	})

	t.Run("LowPriorityConflict", func(t *testing.T) {
		ctx := testcontext.New(t)

		narData, err := os.ReadFile(testdataPath(t, "nar/hello.txt.nar"))
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
		wantNARInfo, err := os.ReadFile(testdataPath(t, "info1/"+wantDigest+".narinfo"))
		if err != nil {
			t.Fatal(err)
		}

		dir := t.TempDir()
		copyToDir(t, dir, "discovery.json")
		copyToDir(t, dir, "nar/hello.txt.nar")
		copyToDir(t, dir, "info2/"+wantDigest+".narinfo")
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
			GetNAR: func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(narData)), nil
			},
			NARSize: int64(len(narData)),
		})
		if err != nil {
			t.Error("store.PutObject:", err)
		}

		if got, err := os.ReadFile(filepath.Join(dir, "info1", objectPath.Digest()+".narinfo")); err != nil {
			t.Error(err)
		} else if !bytes.Equal(got, wantNARInfo) {
			dst := filepath.Join(t.ArtifactDir(), objectPath.Digest()+".narinfo")
			t.Errorf("%s.narinfo content does not match (wrote to %s)", objectPath.Digest(), dst)
			os.WriteFile(dst, got, 0o666)
		}

		if got, err := os.ReadFile(filepath.Join(dir, "upload", objectPath.Digest()+".nar")); err != nil {
			t.Error(err)
		} else if !bytes.Equal(got, narData) {
			dst := filepath.Join(t.ArtifactDir(), "hello.txt.nar")
			t.Errorf("hello.txt.nar content does not match (wrote to %s)", dst)
			os.WriteFile(dst, got, 0o666)
		}
	})

	t.Run("ForcedGzipEncoding", func(t *testing.T) {
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
				Transport: &restrictContentEncoding{
					acceptEncoding: "gzip, *;q=0",
					roundTripper:   fileurl.Transport{},
				},
			},
		}

		err = store.PutObject(ctx, &PutObjectRequest{
			StorePath:      objectPath,
			ContentAddress: ca,
			GetNAR: func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(narData)), nil
			},
			NARSize: int64(len(narData)),
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

	t.Run("ForcedIdentityEncoding", func(t *testing.T) {
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
				Transport: &restrictContentEncoding{
					acceptEncoding: "identity, *;q=0",
					roundTripper:   fileurl.Transport{},
				},
			},
		}

		err = store.PutObject(ctx, &PutObjectRequest{
			StorePath:      objectPath,
			ContentAddress: ca,
			GetNAR: func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(narData)), nil
			},
			NARSize: int64(len(narData)),
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
}

type restrictContentEncoding struct {
	acceptEncoding string
	roundTripper   http.RoundTripper
}

func (rce *restrictContentEncoding) RoundTrip(req *http.Request) (*http.Response, error) {
	acceptEncoding := pointerToArrayPointer(&rce.acceptEncoding)[:]
	if req.Method != "" && req.Method != http.MethodGet && req.Method != http.MethodHead {
		inUse := cmp.Or(req.Header.Get("Content-Encoding"), "identity")
		if q := xhttp.EncodingQuality(acceptEncoding, inUse); q == 0 {
			req.Body.Close()
			const message = ""
			const code = http.StatusUnsupportedMediaType
			return &http.Response{
				Request:       req,
				Proto:         "HTTP/1.1",
				ProtoMajor:    1,
				ProtoMinor:    1,
				StatusCode:    code,
				Status:        http.StatusText(code),
				ContentLength: int64(len(message)),
				Header: http.Header{
					"Content-Type":           {"text/plain; charset=utf-8"},
					"X-Content-Type-Options": {"nosniff"},
					"Accept-Encoding":        {rce.acceptEncoding},
					"Content-Length":         {strconv.Itoa(len(message))},
					"Date":                   {time.Now().UTC().Format(http.TimeFormat)},
				},
				Body: io.NopCloser(strings.NewReader(message)),
			}, nil
		}
	}

	resp, err := rce.roundTripper.RoundTrip(req)
	if resp != nil && len(resp.Header.Values("Accept-Encoding")) > 0 {
		resp.Header.Set("Accept-Encoding", rce.acceptEncoding)
	}
	return resp, err
}

func pointerToArrayPointer[T any](ptr *T) *[1]T {
	return (*[1]T)(unsafe.Pointer(ptr))
}
