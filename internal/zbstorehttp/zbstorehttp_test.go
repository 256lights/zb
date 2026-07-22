// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package zbstorehttp

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zb.256lights.llc/pkg/internal/fileurl"
	"zb.256lights.llc/pkg/internal/testcontext"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log/testlog"
	"zombiezen.com/go/nix"
)

func TestStoreObject(t *testing.T) {
	t.Run("File", func(t *testing.T) {
		ctx := testcontext.New(t)
		store := httpStoreForDirectory(t, ".")
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
		store := httpStoreForDirectory(t, ".")
		_, err := store.Object(ctx, "/opt/zb/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bork")
		if err == nil {
			t.Error("no error returned")
		} else if !errors.Is(err, zbstore.ErrNotFound) {
			t.Error("unexpected error:", err)
		}
	})
}

func TestStoreFetchRealizations(t *testing.T) {
	t.Run("File", func(t *testing.T) {
		ctx := testcontext.New(t)
		store := httpStoreForDirectory(t, ".")
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
		store := httpStoreForDirectory(t, ".")
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

func TestStorePutRealizations(t *testing.T) {
	t.Run("Create", func(t *testing.T) {
		ctx := testcontext.New(t)

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

		drvHash := mustParseHash(t, "sha256:bd172e7b837e02a672e417976696642eaabb97847f61a77cf430f515efc97b61")
		err = store.PutRealizations(ctx, zbstore.RealizationMap{
			DerivationHash: drvHash,
			Realizations: map[string][]*zbstore.Realization{
				zbstore.DefaultDerivationOutputName: {
					{
						OutputPath: "/opt/zb/store/mv4z5c5znjdnc40fvqfl1qknszgbdyxd-hello.txt",
					},
				},
			},
		})
		if err != nil {
			t.Error("PutRealizations:", err)
		}

		gotData, err := os.ReadFile(filepath.Join(dir, "realizations", "0qbvr7pibx9hyiyafqbzhjbvpaifcjb6d5qpwirac0kyhdxjw5xx.json"))
		if err != nil {
			t.Fatal(err)
		}
		var got zbstore.RealizationMap
		unmarshalers := jsonv2.UnmarshalFromFunc(zbstore.UnmarshalHashJSONFrom)
		if err := jsonv2.Unmarshal(gotData, &got, jsonv2.WithUnmarshalers(unmarshalers)); err != nil {
			t.Fatal(err)
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
		if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("realizations (-want +got):\n%s", diff)
		}
	})

	t.Run("Update", func(t *testing.T) {
		ctx := testcontext.New(t)

		dir := t.TempDir()
		copyToDir(t, dir, "discovery.json")
		copyToDir(t, dir, "realizations/0qbvr7pibx9hyiyafqbzhjbvpaifcjb6d5qpwirac0kyhdxjw5xx.json")
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

		drvHash := mustParseHash(t, "sha256:bd172e7b837e02a672e417976696642eaabb97847f61a77cf430f515efc97b61")
		err = store.PutRealizations(ctx, zbstore.RealizationMap{
			DerivationHash: drvHash,
			Realizations: map[string][]*zbstore.Realization{
				zbstore.DefaultDerivationOutputName: {
					{
						OutputPath: "/opt/zb/store/mv4z5c5znjdnc40fvqfl1qknszgbdyxd-hello.txt",
						Signatures: []*zbstore.RealizationSignature{
							{
								PublicKey: zbstore.RealizationPublicKey{
									Format: "nonsense",
									Data:   []byte{0x13, 0x37},
								},
								Signature: []byte{0xca, 0xfe},
							},
						},
					},
				},
			},
		})
		if err != nil {
			t.Error("PutRealizations:", err)
		}

		gotData, err := os.ReadFile(filepath.Join(dir, "realizations", "0qbvr7pibx9hyiyafqbzhjbvpaifcjb6d5qpwirac0kyhdxjw5xx.json"))
		if err != nil {
			t.Fatal(err)
		}
		var got zbstore.RealizationMap
		unmarshalers := jsonv2.UnmarshalFromFunc(zbstore.UnmarshalHashJSONFrom)
		if err := jsonv2.Unmarshal(gotData, &got, jsonv2.WithUnmarshalers(unmarshalers)); err != nil {
			t.Fatal(err)
		}
		want := zbstore.RealizationMap{
			DerivationHash: drvHash,
			Realizations: map[string][]*zbstore.Realization{
				zbstore.DefaultDerivationOutputName: {
					{
						OutputPath: "/opt/zb/store/mv4z5c5znjdnc40fvqfl1qknszgbdyxd-hello.txt",
						Signatures: []*zbstore.RealizationSignature{
							{
								PublicKey: zbstore.RealizationPublicKey{
									Format: "nonsense",
									Data:   []byte{0xde, 0xad, 0xbe, 0xef},
								},
								Signature: []byte{0xca, 0xfe},
							},
							{
								PublicKey: zbstore.RealizationPublicKey{
									Format: "nonsense",
									Data:   []byte{0x13, 0x37},
								},
								Signature: []byte{0xca, 0xfe},
							},
						},
					},
				},
			},
		}
		if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("realizations (-want +got):\n%s", diff)
		}
	})
}

func testdataPath(tb testing.TB, path string) string {
	tb.Helper()
	return filepath.Join("testdata", filepath.FromSlash(tb.Name()), filepath.FromSlash(path))
}

func copyToDir(tb testing.TB, dstDir string, path string) {
	tb.Helper()

	src, err := os.Open(testdataPath(tb, path))
	if err != nil {
		tb.Fatal(err)
	}
	defer src.Close()
	subpath := filepath.ToSlash(path)
	if err := os.MkdirAll(filepath.Join(dstDir, filepath.Dir(subpath)), 0o777); err != nil {
		tb.Fatal(err)
	}
	dst, err := os.Create(filepath.Join(dstDir, subpath))
	if err != nil {
		tb.Fatal(err)
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		tb.Fatalf("copy %s to %s: %v", src.Name(), dst.Name(), err)
	}
	if err := dst.Close(); err != nil {
		tb.Fatalf("write %s: %v", dst.Name(), err)
	}
}

func httpStoreForDirectory(tb testing.TB, path string) *Store {
	tb.Helper()

	root, err := os.OpenRoot(testdataPath(tb, path))
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

	return &Store{
		URL:        srvURL,
		HTTPClient: srv.Client(),
	}
}

func TestMain(m *testing.M) {
	testlog.Main(nil)
	os.Exit(m.Run())
}
