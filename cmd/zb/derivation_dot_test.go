// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"maps"
	"slices"
	"testing"

	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
)

func TestCollectDerivationClosure(t *testing.T) {
	const (
		rootPath  = zbstore.Path("/opt/zb/store/00000000000000000000000000000000-root.drv")
		childPath = zbstore.Path("/opt/zb/store/11111111111111111111111111111111-child.drv")
		leafPath  = zbstore.Path("/opt/zb/store/22222222222222222222222222222222-leaf.drv")
	)
	all := map[zbstore.Path]*zbstore.Derivation{
		rootPath: {
			Name: "root",
			InputDerivations: map[zbstore.Path]*sets.Sorted[string]{
				childPath: sets.NewSorted("out"),
			},
		},
		childPath: {
			Name: "child",
			InputDerivations: map[zbstore.Path]*sets.Sorted[string]{
				leafPath: sets.NewSorted("out"),
			},
		},
		leafPath: {
			Name: "leaf",
			InputDerivations: map[zbstore.Path]*sets.Sorted[string]{
				rootPath: sets.NewSorted("out"),
			},
		},
	}
	var calls [][]zbstore.Path
	load := func(_ context.Context, paths []zbstore.Path) (map[zbstore.Path]*zbstore.Derivation, error) {
		calls = append(calls, slices.Clone(paths))
		result := make(map[zbstore.Path]*zbstore.Derivation, len(paths))
		for _, path := range paths {
			result[path] = all[path]
		}
		return result, nil
	}

	got, err := collectDerivationClosure(context.Background(), map[string]*zbstore.Derivation{
		string(rootPath): all[rootPath],
	}, load)
	if err != nil {
		t.Fatal(err)
	}
	if gotPaths := slices.Sorted(maps.Keys(got)); !slices.Equal(gotPaths, []string{
		string(rootPath),
		string(childPath),
		string(leafPath),
	}) {
		t.Errorf("closure paths = %q", gotPaths)
	}
	if len(calls) != 2 ||
		!slices.Equal(calls[0], []zbstore.Path{childPath}) ||
		!slices.Equal(calls[1], []zbstore.Path{leafPath}) {
		t.Errorf("load calls = %q", calls)
	}
}

func TestDerivationExportReceiver(t *testing.T) {
	const drvPath = zbstore.Path("/opt/zb/store/00000000000000000000000000000000-example.drv")
	want := &zbstore.Derivation{
		Dir:     drvPath.Dir(),
		Name:    "example",
		System:  "x86_64-windows",
		Builder: "builder",
		Outputs: map[string]*zbstore.DerivationOutputType{
			"out": zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
		},
	}
	data, err := want.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	var narData bytes.Buffer
	nw := nar.NewWriter(&narData)
	if err := nw.WriteHeader(&nar.Header{Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := nw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := nw.Close(); err != nil {
		t.Fatal(err)
	}

	receiver := &derivationExportReceiver{
		derivations: make(map[zbstore.Path]*zbstore.Derivation),
	}
	if _, err := receiver.Write(narData.Bytes()); err != nil {
		t.Fatal(err)
	}
	receiver.ReceiveNAR(&zbstore.ExportTrailer{StorePath: drvPath})
	if receiver.err != nil {
		t.Fatal(receiver.err)
	}
	got := receiver.derivations[drvPath]
	if got == nil {
		t.Fatal("receiver did not record derivation")
	}
	if got.Dir != want.Dir || got.Name != want.Name || got.System != want.System || got.Builder != want.Builder {
		t.Errorf("received derivation = %#v; want %#v", got, want)
	}
}

func TestMarshalDerivationDOT(t *testing.T) {
	const (
		rootPath  = "/opt/zb/store/00000000000000000000000000000000-root.drv"
		childPath = "/opt/zb/store/11111111111111111111111111111111-child.drv"
	)
	got := string(marshalDerivationDOT(map[string]*zbstore.Derivation{
		childPath: {Name: "child"},
		rootPath: {
			Name: `root "quoted"\name`,
			InputDerivations: map[zbstore.Path]*sets.Sorted[string]{
				childPath: sets.NewSorted("out"),
			},
		},
	}))
	want := "digraph {\n" +
		"\tgraph [ranksep=2.0];\n" +
		"\t\"" + rootPath + "\" [label=\"root \\\"quoted\\\"\\\\name\", shape=box];\n" +
		"\t\"" + childPath + "\" [label=\"child\", shape=box];\n" +
		"\t\"" + rootPath + "\" -> \"" + childPath + "\";\n" +
		"}\n"
	if got != want {
		t.Errorf("marshalDerivationDOT() =\n%s\nwant:\n%s", got, want)
	}
}
