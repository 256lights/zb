// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package backend

import (
	"fmt"
	"reflect"
	"testing"
	"unique"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zb.256lights.llc/pkg/internal/system"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/nix"
)

func TestAnalyze(t *testing.T) {
	tests := []struct {
		name           string
		derivations    []*zbstore.Derivation
		desiredOutputs map[string]sets.Set[string]
		want           *dependencyGraph
	}{
		{
			name: "Empty",
			want: &dependencyGraph{},
		},
		{
			name: "SingleNode",
			derivations: []*zbstore.Derivation{
				{
					Name:   "foo.txt",
					Dir:    zbstore.DefaultUnixDirectory,
					System: system.Current().String(),
					Outputs: map[string]*zbstore.DerivationOutputType{
						"out": zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
					},
				},
			},
			desiredOutputs: map[string]sets.Set[string]{
				"foo.txt": sets.New("out"),
			},
			want: &dependencyGraph{
				roots: sets.New[zbstore.Path]("foo.txt"),
				nodes: map[zbstore.Path]*dependencyGraphNode{
					"foo.txt": {
						usedOutputs: sets.New(unique.Make("out")),
					},
				},
			},
		},
		{
			name: "TwoNodes",
			derivations: []*zbstore.Derivation{
				{
					Name:   "foo.txt",
					Dir:    zbstore.DefaultUnixDirectory,
					System: system.Current().String(),
					Outputs: map[string]*zbstore.DerivationOutputType{
						"out": zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
					},
				},
				{
					Name:   "bar.txt",
					Dir:    zbstore.DefaultUnixDirectory,
					System: system.Current().String(),
					Outputs: map[string]*zbstore.DerivationOutputType{
						"out": zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
					},
				},
			},
			desiredOutputs: map[string]sets.Set[string]{
				"foo.txt": sets.New("out"),
				"bar.txt": sets.New("out"),
			},
			want: &dependencyGraph{
				roots: sets.New[zbstore.Path]("foo.txt", "bar.txt"),
				nodes: map[zbstore.Path]*dependencyGraphNode{
					"foo.txt": {
						usedOutputs: sets.New(unique.Make("out")),
					},
					"bar.txt": {
						usedOutputs: sets.New(unique.Make("out")),
					},
				},
			},
		},
		{
			name: "TwoNodeChain",
			derivations: []*zbstore.Derivation{
				{
					Name:   "foo.txt",
					Dir:    zbstore.DefaultUnixDirectory,
					System: system.Current().String(),
					Outputs: map[string]*zbstore.DerivationOutputType{
						"out": zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
					},
				},
				{
					Name:   "bar.txt",
					Dir:    zbstore.DefaultUnixDirectory,
					System: system.Current().String(),
					InputDerivations: map[zbstore.Path]*sets.Sorted[string]{
						"foo.txt": sets.NewSorted("out"),
					},
					Outputs: map[string]*zbstore.DerivationOutputType{
						"out": zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
					},
				},
			},
			desiredOutputs: map[string]sets.Set[string]{
				"bar.txt": sets.New("out"),
			},
			want: &dependencyGraph{
				roots: sets.New[zbstore.Path]("foo.txt"),
				nodes: map[zbstore.Path]*dependencyGraphNode{
					"foo.txt": {
						dependents:  sets.New[zbstore.Path]("bar.txt"),
						usedOutputs: sets.New(unique.Make("out")),
					},
					"bar.txt": {
						usedOutputs: sets.New(unique.Make("out")),
					},
				},
			},
		},
		{
			name: "Hinge",
			derivations: []*zbstore.Derivation{
				{
					Name:   "foo.txt",
					Dir:    zbstore.DefaultUnixDirectory,
					System: system.Current().String(),
					Outputs: map[string]*zbstore.DerivationOutputType{
						"out": zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
					},
				},
				{
					Name:   "bar.txt",
					Dir:    zbstore.DefaultUnixDirectory,
					System: system.Current().String(),
					Outputs: map[string]*zbstore.DerivationOutputType{
						"out": zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
					},
				},
				{
					Name:   "baz.txt",
					Dir:    zbstore.DefaultUnixDirectory,
					System: system.Current().String(),
					InputDerivations: map[zbstore.Path]*sets.Sorted[string]{
						"foo.txt": sets.NewSorted("out"),
						"bar.txt": sets.NewSorted("out"),
					},
					Outputs: map[string]*zbstore.DerivationOutputType{
						"out": zbstore.RecursiveFileFloatingCAOutput(nix.SHA256),
					},
				},
			},
			desiredOutputs: map[string]sets.Set[string]{
				"baz.txt": sets.New("out"),
			},
			want: &dependencyGraph{
				roots: sets.New[zbstore.Path]("foo.txt", "bar.txt"),
				nodes: map[zbstore.Path]*dependencyGraphNode{
					"foo.txt": {
						dependents:  sets.New[zbstore.Path]("baz.txt"),
						usedOutputs: sets.New(unique.Make("out")),
					},
					"bar.txt": {
						dependents:  sets.New[zbstore.Path]("baz.txt"),
						usedOutputs: sets.New(unique.Make("out")),
					},
					"baz.txt": {
						usedOutputs: sets.New(unique.Make("out")),
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			derivations := make(map[zbstore.Path]*zbstore.Derivation)
			pathForDrvName := func(name string) (zbstore.Path, error) {
				for p := range derivations {
					if curr, _ := p.DerivationName(); curr == name {
						return p, nil
					}
				}
				return "", fmt.Errorf("no such derivation %s", name)
			}
			for _, drv := range test.derivations {
				rewrittenInputs, err := rewriteKeys(drv.InputDerivations, func(k zbstore.Path) (zbstore.Path, error) {
					return pathForDrvName(string(k))
				})
				if err != nil {
					t.Fatalf("%s: input derivations: %v", drv.Name, err)
				}
				drv = drv.Clone()
				drv.InputDerivations = rewrittenInputs

				_, trailer, err := drv.Export(nix.SHA256)
				if err != nil {
					t.Fatal(err)
				}
				drvPath, err := zbstore.FixedCAOutputPath(drv.Dir, drv.Name+zbstore.DerivationExt, trailer.ContentAddress, zbstore.References{
					Others: trailer.References,
				})
				if err != nil {
					t.Fatal(err)
				}
				derivations[drvPath] = drv
			}

			desiredOutputs := make(sets.Set[zbstore.OutputReference])
			for drvName, outputNames := range test.desiredOutputs {
				drvPath, err := pathForDrvName(drvName)
				if err != nil {
					t.Fatal("desired outputs:", err)
				}
				for name := range outputNames.All() {
					desiredOutputs.Add(zbstore.OutputReference{
						DrvPath:    drvPath,
						OutputName: name,
					})
				}
			}

			got, err := analyze(derivations, desiredOutputs)
			if err != nil {
				t.Fatal("analyze:", err)
			}

			for drvPath, drv := range derivations {
				node := got.nodes[drvPath]
				if node == nil {
					t.Errorf("analyze did not return a node for %s", drvPath)
					continue
				}
				if node.derivation == nil {
					t.Errorf("analysis node for %s did not set derivation", drvPath)
				} else if node.derivation != drv {
					t.Errorf("analysis node for %s does not match derivation", drvPath)
				}
			}

			want := new(dependencyGraph)
			*want = *test.want
			want.nodes = make(map[zbstore.Path]*dependencyGraphNode)
			for fakePath, node := range test.want.nodes {
				drvPath, err := pathForDrvName(string(fakePath))
				if err != nil {
					t.Fatal("want.nodes:", err)
				}
				nodeClone := new(dependencyGraphNode)
				*nodeClone = *node
				nodeClone.dependents, err = rewriteKeys(node.dependents, func(p zbstore.Path) (zbstore.Path, error) {
					return pathForDrvName(string(p))
				})
				if err != nil {
					t.Fatal("want.nodes:", err)
				}
				want.nodes[drvPath] = nodeClone
			}
			want.roots = make(sets.Set[zbstore.Path])
			for fakePath := range test.want.roots.All() {
				drvPath, err := pathForDrvName(string(fakePath))
				if err != nil {
					t.Fatal("want.roots:", err)
				}
				want.roots.Add(drvPath)
			}

			diff := cmp.Diff(
				want, got,
				cmpopts.EquateEmpty(),
				cmp.AllowUnexported(dependencyGraph{}),
				cmp.AllowUnexported(dependencyGraphNode{}),
				cmp.FilterPath(func(p cmp.Path) bool {
					return p.Index(-2).Type() == reflect.TypeFor[dependencyGraphNode]() &&
						p.Index(-1).(cmp.StructField).Name() == "derivation"
				}, cmp.Ignore()),
			)
			if diff != "" {
				t.Errorf("analysis (-want +got):\n%s", diff)
			}
		})
	}
}

func rewriteKeys[K1 comparable, K2 comparable, V any, M1 ~map[K1]V](m M1, f func(K1) (K2, error)) (map[K2]V, error) {
	m2 := make(map[K2]V, len(m))
	for k, v := range m {
		k2, err := f(k)
		if err != nil {
			return nil, err
		}
		m2[k2] = v
	}
	return m2, nil
}
