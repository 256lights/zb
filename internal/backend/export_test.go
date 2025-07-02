// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package backend_test

import (
	"bytes"
	stdcmp "cmp"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zb.256lights.llc/pkg/internal/backend"
	"zb.256lights.llc/pkg/internal/backendtest"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/storetest"
	"zb.256lights.llc/pkg/internal/testcontext"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
)

func TestExport(t *testing.T) {
	const (
		noDepsPath             = 0
		directDependencyPath   = 1
		indirectDependencyPath = 2
		selfDependencyPath     = 3
	)
	tests := []struct {
		name              string
		paths             []int
		excludeReferences bool
		want              []int
	}{
		{
			name:  "EmptyList",
			paths: []int{},
			want:  []int{},
		},
		{
			name:  "IndependentPath",
			paths: []int{noDepsPath},
			want:  []int{noDepsPath},
		},
		{
			name:  "SelfDependencyPath",
			paths: []int{selfDependencyPath},
			want:  []int{selfDependencyPath},
		},
		{
			name:  "DirectDependencyPath",
			paths: []int{directDependencyPath},
			want:  []int{noDepsPath, directDependencyPath},
		},
		{
			name:  "IndirectDependencyPath",
			paths: []int{indirectDependencyPath},
			want:  []int{noDepsPath, directDependencyPath, indirectDependencyPath},
		},
		{
			name:              "IndirectDependencyPathExcludeReferences",
			paths:             []int{indirectDependencyPath},
			excludeReferences: true,
			want:              []int{indirectDependencyPath},
		},
		{
			name:  "Deduplicate",
			paths: []int{noDepsPath, directDependencyPath},
			want:  []int{noDepsPath, directDependencyPath},
		},
		{
			name:  "DeduplicateAndReorder",
			paths: []int{directDependencyPath, noDepsPath},
			want:  []int{noDepsPath, directDependencyPath},
		},
	}

	generateImport := func(dir zbstore.Directory) ([]narRecord, []byte, error) {
		const fileContent = "Hello, World!\n"
		exportBuffer := new(bytes.Buffer)
		exporter := zbstore.NewExportWriter(exportBuffer)
		result := make([]narRecord, 4)
		var err error
		result[noDepsPath], err = exportSourceFile(exporter, []byte(fileContent), storetest.SourceExportOptions{
			Name:      "hello.txt",
			Directory: dir,
		})
		if err != nil {
			return nil, nil, err
		}
		directDependencyContent := "Hello, " + result[noDepsPath].trailer.StorePath.Base() + "\n"
		result[directDependencyPath], err = exportSourceFile(exporter, []byte(directDependencyContent), storetest.SourceExportOptions{
			Name:      "a.txt",
			Directory: dir,
			References: zbstore.References{
				Others: *sets.NewSorted(result[noDepsPath].trailer.StorePath),
			},
		})
		if err != nil {
			return nil, nil, err
		}
		indirectDependencyContent := "Hello, " + result[directDependencyPath].trailer.StorePath.Base() + "\n"
		result[indirectDependencyPath], err = exportSourceFile(exporter, []byte(indirectDependencyContent), storetest.SourceExportOptions{
			Name:      "b.txt",
			Directory: dir,
			References: zbstore.References{
				Others: *sets.NewSorted(result[directDependencyPath].trailer.StorePath),
			},
		})
		if err != nil {
			return nil, nil, err
		}
		const tempDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		const selfDependencyContent = "I am " + tempDigest + "-self.txt\n"
		result[selfDependencyPath], err = exportSourceFile(exporter, []byte(selfDependencyContent), storetest.SourceExportOptions{
			Name:       "self.txt",
			Directory:  dir,
			TempDigest: tempDigest,
			References: zbstore.References{
				Self: true,
			},
		})
		if err != nil {
			return nil, nil, err
		}

		if err := exporter.Close(); err != nil {
			return nil, nil, err
		}
		return result, exportBuffer.Bytes(), nil
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Run("RPC", func(t *testing.T) {
				ctx, cancel := testcontext.New(t)
				defer cancel()

				dir := backendtest.NewStoreDirectory(t)
				records, importData, err := generateImport(dir)
				if err != nil {
					t.Fatal(err)
				}

				receiver := new(spyNARReceiver)
				_, client, err := backendtest.NewServer(ctx, t, dir, &backendtest.Options{
					TempDir: t.TempDir(),
					ClientOptions: zbstorerpc.CodecOptions{
						NARReceiver: receiver,
					},
				})
				if err != nil {
					t.Fatal(err)
				}

				// Import test data.
				codec, releaseCodec, err := storeCodec(ctx, client)
				if err != nil {
					t.Fatal(err)
				}
				err = codec.Export(nil, bytes.NewReader(importData))
				releaseCodec()
				if err != nil {
					t.Fatal(err)
				}

				// Call exists method.
				// Exports don't send a response, so this introduces a sync point.
				var exists bool
				lastPath := records[len(records)-1].trailer.StorePath
				err = jsonrpc.Do(ctx, client, zbstorerpc.ExistsMethod, &exists, &zbstorerpc.ExistsRequest{
					Path: string(lastPath),
				})
				if err != nil {
					t.Error(err)
				}
				if !exists {
					t.Errorf("store reports exists=false for %s", lastPath)
				}

				// Perform export.
				req := &zbstorerpc.ExportRequest{
					Paths:             make([]zbstore.Path, len(test.paths)),
					ExcludeReferences: test.excludeReferences,
				}
				for i, pathIndex := range test.paths {
					req.Paths[i] = records[pathIndex].trailer.StorePath
				}
				if err := jsonrpc.Do(ctx, client, zbstorerpc.ExportMethod, nil, req); err != nil {
					t.Error("Export:", err)
				}

				// Check contents of export.
				want := make([]narRecord, len(test.want))
				for i, pathIndex := range test.want {
					want[i] = records[pathIndex]
				}
				diff := cmp.Diff(
					want, receiver.records,
					cmpopts.EquateEmpty(),
					cmp.AllowUnexported(narRecord{}),
					transformSortedSet[zbstore.Path](),
				)
				if diff != "" {
					t.Errorf("export (-want +got):\n%s", diff)
				}
			})

			for _, mapped := range [...]bool{false, true} {
				var mapTestName string
				if mapped {
					mapTestName = "Mapped"
				} else {
					mapTestName = "Real"
				}

				t.Run(mapTestName, func(t *testing.T) {
					ctx, cancel := testcontext.New(t)
					defer cancel()

					var dir zbstore.Directory
					var realDir string
					if mapped {
						dir = zbstore.DefaultDirectory()
						realDir = t.TempDir()
					} else {
						dir = backendtest.NewStoreDirectory(t)
						realDir = string(dir)
					}
					records, importData, err := generateImport(dir)
					if err != nil {
						t.Fatal(err)
					}

					srv, client, err := backendtest.NewServer(ctx, t, dir, &backendtest.Options{
						TempDir: t.TempDir(),
						Options: backend.Options{
							RealStoreDirectory: realDir,
						},
					})
					if err != nil {
						t.Fatal(err)
					}

					// Import test data.
					codec, releaseCodec, err := storeCodec(ctx, client)
					if err != nil {
						t.Fatal(err)
					}
					err = codec.Export(nil, bytes.NewReader(importData))
					releaseCodec()
					if err != nil {
						t.Fatal(err)
					}

					// Call exists method.
					// Exports don't send a response, so this introduces a sync point.
					var exists bool
					lastPath := records[len(records)-1].trailer.StorePath
					err = jsonrpc.Do(ctx, client, zbstorerpc.ExistsMethod, &exists, &zbstorerpc.ExistsRequest{
						Path: string(lastPath),
					})
					if err != nil {
						t.Error(err)
					}
					if !exists {
						t.Errorf("store reports exists=false for %s", lastPath)
					}

					// Perform export.
					got := new(bytes.Buffer)
					req := &zbstorerpc.ExportRequest{
						Paths:             make([]zbstore.Path, len(test.paths)),
						ExcludeReferences: test.excludeReferences,
					}
					for i, pathIndex := range test.paths {
						req.Paths[i] = records[pathIndex].trailer.StorePath
					}
					if err := srv.Export(ctx, got, req); err != nil {
						t.Error("Export:", err)
					}

					// Check contents of export.
					receiver := new(spyNARReceiver)
					if err := zbstore.ReceiveExport(receiver, got); err != nil {
						t.Error("Read export:", err)
					}
					want := make([]narRecord, len(test.want))
					for i, pathIndex := range test.want {
						want[i] = records[pathIndex]
					}
					diff := cmp.Diff(
						want, receiver.records,
						cmpopts.EquateEmpty(),
						cmp.AllowUnexported(narRecord{}),
						transformSortedSet[zbstore.Path](),
					)
					if diff != "" {
						t.Errorf("export (-want +got):\n%s", diff)
					}
				})
			}
		})
	}
}

type narRecord struct {
	nar     []byte
	trailer zbstore.ExportTrailer
}

func exportSourceFile(exp *zbstore.ExportWriter, data []byte, opts storetest.SourceExportOptions) (narRecord, error) {
	narBuffer := new(bytes.Buffer)
	if err := storetest.SingleFileNAR(narBuffer, data); err != nil {
		return narRecord{}, err
	}
	path, ca, err := storetest.ExportSourceNAR(exp, narBuffer.Bytes(), opts)
	if err != nil {
		return narRecord{}, err
	}
	return narRecord{
		nar: narBuffer.Bytes(),
		trailer: zbstore.ExportTrailer{
			StorePath:      path,
			References:     *opts.References.ToSet(path),
			ContentAddress: ca,
		},
	}, nil
}

type spyNARReceiver struct {
	records []narRecord
}

func (r *spyNARReceiver) Write(p []byte) (int, error) {
	if len(r.records) == 0 || r.records[len(r.records)-1].trailer.StorePath != "" {
		r.records = append(r.records, narRecord{})
	}
	record := &r.records[len(r.records)-1]
	record.nar = append(record.nar, p...)
	return len(p), nil
}

func (r *spyNARReceiver) ReceiveNAR(t *zbstore.ExportTrailer) {
	dst := &r.records[len(r.records)-1].trailer
	*dst = *t
	dst.References = *dst.References.Clone()
}

func transformSortedSet[E stdcmp.Ordered]() cmp.Option {
	return cmp.Transformer("transformSortedSet", func(s sets.Sorted[E]) []E {
		list := make([]E, s.Len())
		for i := range list {
			list[i] = s.At(i)
		}
		return list
	})
}
