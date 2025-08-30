// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package remotestore

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"zb.256lights.llc/pkg/internal/testcontext"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/nix"
)

func TestHTTPStore(t *testing.T) {
	ctx, cancel := testcontext.New(t)
	defer cancel()

	cacheFileRoot, err := os.OpenRoot(filepath.Join("testdata", "cache"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cacheFileRoot.Close(); err != nil {
			t.Error(err)
		}
	})

	srv := httptest.NewServer(http.FileServerFS(cacheFileRoot.FS()))
	t.Cleanup(srv.Close)
	srvURL, err := url.Parse(srv.URL + "/discovery.json")
	if err != nil {
		t.Fatal(err)
	}

	store := &HTTPStore{
		URL:        srvURL,
		HTTPClient: srv.Client(),
	}

	t.Run("File", func(t *testing.T) {
		caHash, err := nix.ParseHash("sha256:1qicirpsz48j7a2r5h9lj04kipdyvxanwglv9ymfq0qsv7isywdf")
		if err != nil {
			t.Fatal(err)
		}
		wantHash, err := nix.ParseHash("sha256:0xmvxmsmmc6n79sk2h3r6db3yp8drmxps61mdk7iqnvc6vcsww60")
		if err != nil {
			t.Fatal(err)
		}

		obj, err := store.Object(ctx, "/opt/zb/store/mv4z5c5znjdnc40fvqfl1qknszgbdyxd-hello.txt")
		if err != nil {
			t.Fatal(err)
		}
		wantTrailer := &zbstore.ExportTrailer{
			StorePath:      "/opt/zb/store/mv4z5c5znjdnc40fvqfl1qknszgbdyxd-hello.txt",
			ContentAddress: nix.RecursiveFileContentAddress(caHash),
		}
		if diff := cmp.Diff(wantTrailer, obj.Trailer(), transformSortedSet[zbstore.Path]()); diff != "" {
			t.Errorf("trailer (-want +got):\n%s", diff)
		}
		h := nix.NewHasher(nix.SHA256)
		if err := obj.WriteNAR(ctx, h); err != nil {
			t.Error("write nar:", err)
		} else if gotHash := h.SumHash(); !gotHash.Equal(wantHash) {
			t.Errorf("written nar hash = %v; want %v", gotHash, wantHash)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		_, err := store.Object(ctx, "/opt/zb/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bork")
		if err == nil {
			t.Error("no error returned")
		} else if !errors.Is(err, zbstore.ErrNotFound) {
			t.Error("unexpected error:", err)
		}
	})
}
