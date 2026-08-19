// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package zbstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"strings"
	"testing"

	"zb.256lights.llc/pkg/internal/testcontext"
	"zb.256lights.llc/pkg/internal/xio"
	"zb.256lights.llc/pkg/sets"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
)

func TestVerifyObject(t *testing.T) {
	t.Run("SingleSourceFile", func(t *testing.T) {
		narData := singleFileNAR(t, []byte("Hello, World!\n"))
		ca, _, err := SourceSHA256ContentAddress(bytes.NewReader(narData), nil)
		if err != nil {
			t.Fatal(err)
		}
		path, err := FixedCAOutputPath(DefaultUnixDirectory, "hello.txt", ca, References{})
		if err != nil {
			t.Fatal(err)
		}

		ctx := testcontext.New(t)
		obj := &fakeObject{
			nar: narData,
			trailer: ExportTrailer{
				StorePath:      path,
				ContentAddress: ca,
			},
		}
		if err := VerifyObject(ctx, obj, nil); err != nil {
			t.Error("VerifyObject(...):", err)
		}
	})

	t.Run("SelfReference", func(t *testing.T) {
		content := func(digest string) []byte {
			return []byte("It's " + digest + "-hello.txt!\n")
		}

		fakeDigest := strings.Repeat("a", objectNameDigestLength)
		hashNARData := singleFileNAR(t, content(fakeDigest))
		ca, _, err := SourceSHA256ContentAddress(bytes.NewReader(hashNARData), &ContentAddressOptions{
			Digest: fakeDigest,
		})
		if err != nil {
			t.Fatal(err)
		}
		path, err := FixedCAOutputPath(DefaultUnixDirectory, "hello.txt", ca, References{
			Self: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		narData := singleFileNAR(t, content(path.Digest()))

		ctx := testcontext.New(t)
		obj := &fakeObject{
			nar: narData,
			trailer: ExportTrailer{
				StorePath:      path,
				ContentAddress: ca,
				References:     *sets.NewSorted(path),
			},
		}
		if err := VerifyObject(ctx, obj, nil); err != nil {
			t.Error("VerifyObject(...):", err)
		}
	})

	t.Run("FixedHashFile", func(t *testing.T) {
		const content = "Hello, World!\n"
		narData := singleFileNAR(t, []byte(content))
		sum := sha256.Sum256([]byte(content))
		ca := nix.FlatFileContentAddress(nix.NewHash(nix.SHA256, sum[:]))
		path, err := FixedCAOutputPath(DefaultUnixDirectory, "hello.txt", ca, References{})
		if err != nil {
			t.Fatal(err)
		}

		ctx := testcontext.New(t)
		obj := &fakeObject{
			nar: narData,
			trailer: ExportTrailer{
				StorePath:      path,
				ContentAddress: ca,
			},
		}
		if err := VerifyObject(ctx, obj, nil); err != nil {
			t.Error("VerifyObject(...):", err)
		}
	})

	t.Run("TextFile", func(t *testing.T) {
		const content = "Hello, World!\n"
		narData := singleFileNAR(t, []byte(content))
		sum := sha256.Sum256([]byte(content))
		ca := nix.TextContentAddress(nix.NewHash(nix.SHA256, sum[:]))
		path, err := FixedCAOutputPath(DefaultUnixDirectory, "hello.txt", ca, References{})
		if err != nil {
			t.Fatal(err)
		}

		ctx := testcontext.New(t)
		obj := &fakeObject{
			nar: narData,
			trailer: ExportTrailer{
				StorePath:      path,
				ContentAddress: ca,
			},
		}
		if err := VerifyObject(ctx, obj, nil); err != nil {
			t.Error("VerifyObject(...):", err)
		}
	})

	t.Run("MismatchedCA", func(t *testing.T) {
		narData := singleFileNAR(t, []byte("Hello, World!\n"))
		ca, _, err := SourceSHA256ContentAddress(bytes.NewReader(narData), nil)
		if err != nil {
			t.Fatal(err)
		}
		badCA, _, err := SourceSHA256ContentAddress(xio.Null(), nil)
		if err != nil {
			t.Fatal(err)
		}
		path, err := FixedCAOutputPath(DefaultUnixDirectory, "hello.txt", ca, References{})
		if err != nil {
			t.Fatal(err)
		}

		ctx := testcontext.New(t)
		obj := &fakeObject{
			nar: narData,
			trailer: ExportTrailer{
				StorePath:      path,
				ContentAddress: badCA,
			},
		}
		if err := VerifyObject(ctx, obj, nil); err == nil {
			t.Error("VerifyObject(...) = <nil>; want <error>")
		} else {
			t.Log("VerifyObject(...):", err)
		}
	})

	t.Run("MismatchedPath", func(t *testing.T) {
		narData := singleFileNAR(t, []byte("Hello, World!\n"))
		ca, _, err := SourceSHA256ContentAddress(bytes.NewReader(narData), nil)
		if err != nil {
			t.Fatal(err)
		}
		badCA, _, err := SourceSHA256ContentAddress(xio.Null(), nil)
		if err != nil {
			t.Fatal(err)
		}
		badPath, err := FixedCAOutputPath(DefaultUnixDirectory, "hello.txt", badCA, References{})
		if err != nil {
			t.Fatal(err)
		}

		ctx := testcontext.New(t)
		obj := &fakeObject{
			nar: narData,
			trailer: ExportTrailer{
				StorePath:      badPath,
				ContentAddress: ca,
			},
		}
		if err := VerifyObject(ctx, obj, nil); err == nil {
			t.Error("VerifyObject(...) = <nil>; want <error>")
		} else {
			t.Log("VerifyObject(...):", err)
		}
	})
}

func singleFileNAR(tb testing.TB, data []byte) []byte {
	tb.Helper()

	buf := new(bytes.Buffer)
	nw := nar.NewWriter(buf)
	if err := nw.WriteHeader(&nar.Header{Size: int64(len(data))}); err != nil {
		tb.Fatal(err)
	}
	if _, err := nw.Write(data); err != nil {
		tb.Fatal(err)
	}
	if err := nw.Close(); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

type fakeObject struct {
	trailer ExportTrailer
	nar     []byte
}

func (obj *fakeObject) Trailer() *ExportTrailer {
	return &obj.trailer
}

func (obj *fakeObject) WriteNAR(ctx context.Context, w io.Writer) error {
	_, err := w.Write(obj.nar)
	return err
}
