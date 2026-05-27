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

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/google/go-cmp/cmp"
	"zb.256lights.llc/pkg/internal/testcontext"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/nix"
)

func TestHTTPStoreObject(t *testing.T) {
	store := httpStoreForDirectory(t, filepath.Join("testdata", "cache"))

	t.Run("File", func(t *testing.T) {
		ctx := testcontext.New(t)
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
		ctx := testcontext.New(t)
		_, err := store.Object(ctx, "/opt/zb/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bork")
		if err == nil {
			t.Error("no error returned")
		} else if !errors.Is(err, zbstore.ErrNotFound) {
			t.Error("unexpected error:", err)
		}
	})
}

func TestHTTPStoreFetchRealizations(t *testing.T) {
	store := httpStoreForDirectory(t, filepath.Join("testdata", "cache"))

	t.Run("File", func(t *testing.T) {
		ctx := testcontext.New(t)
		drvHash := mustParseHash(t, "sha256:bd172e7b837e02a672e417976696642eaabb97847f61a77cf430f515efc97b61")
		got, err := store.FetchRealizations(ctx, drvHash)
		if err != nil {
			t.Error(err)
		}
		want := zbstore.RealizationMap{
			DerivationHash: drvHash,
			Realizations: map[string][]*zbstore.Realization{
				zbstore.DefaultDerivationOutputName: {
					{
						OutputPath: "/opt/zb/store/mv4z5c5znjdnc40fvqfl1qknszgbdyxd-hello.txt",
					},
				},
			},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("realizations (-want +got):\n%s", diff)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		ctx := testcontext.New(t)
		drvHash := mustParseHash(t, "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
		got, err := store.FetchRealizations(ctx, drvHash)
		if err != nil {
			t.Error(err)
		}
		if len(got.Realizations) > 0 {
			realizationsJSON, _ := jsonv2.Marshal(got)
			t.Errorf("realizations = %s; want {}", realizationsJSON)
		}
		if !got.DerivationHash.Equal(drvHash) {
			t.Errorf("derivation hash = %v; want %v", got.DerivationHash, drvHash)
		}
	})
}

func httpStoreForDirectory(tb testing.TB, path string) *HTTPStore {
	tb.Helper()

	root, err := os.OpenRoot(path)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := root.Close(); err != nil {
			tb.Error(err)
		}
	})

	srv := httptest.NewServer(http.FileServerFS(root.FS()))
	tb.Cleanup(srv.Close)
	srvURL, err := url.Parse(srv.URL + "/discovery.json")
	if err != nil {
		tb.Fatal(err)
	}

	return &HTTPStore{
		URL:        srvURL,
		HTTPClient: srv.Client(),
	}
}
