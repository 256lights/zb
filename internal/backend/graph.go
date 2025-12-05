// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package backend

import (
	"fmt"
	"slices"
	"unique"

	"zb.256lights.llc/pkg/internal/xslices"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
)

// dependencyGraph stores indices of a set of derivations that are useful for realization.
type dependencyGraph struct {
	// nodes is a map of .drv file path to [*dependencyGraphNode].
	nodes map[zbstore.Path]*dependencyGraphNode
	// roots is the set of .drv files that have no input derivations.
	roots sets.Set[zbstore.Path]
}

// get gets or creates a node in graph.nodes for the given path.
// If created, then the node's derivation is set to drv.
func (graph *dependencyGraph) get(path zbstore.Path, drv *zbstore.Derivation) *dependencyGraphNode {
	node := graph.nodes[path]
	if node == nil {
		node = &dependencyGraphNode{derivation: drv}
		graph.nodes[path] = node
	}
	return node
}

// dependencyGraphNode stores auxiliary information about a [*zbstore.Derivation].
type dependencyGraphNode struct {
	derivation *zbstore.Derivation

	// dependents is the set of paths of derivations that depend on this one.
	dependents sets.Set[zbstore.Path]
	// usedOutputs is the set of output names that a build must have realizations for.
	usedOutputs sets.Set[unique.Handle[string]]
}

// analyze produces a [dependencyGraph] for the given set of desired outputs.
func analyze(derivations map[zbstore.Path]*zbstore.Derivation, want sets.Set[zbstore.OutputReference]) (*dependencyGraph, error) {
	result := &dependencyGraph{
		roots: make(sets.Set[zbstore.Path]),
		nodes: make(map[zbstore.Path]*dependencyGraphNode),
	}

	drvHashes := make(map[zbstore.Path]hashKey)
	used := make(map[hashKey]sets.Set[unique.Handle[string]])
	stack := slices.Collect(want.All())
	for len(stack) > 0 {
		ref := xslices.Last(stack)
		stack = xslices.Pop(stack, 1)
		if _, hashed := drvHashes[ref.DrvPath]; hashed {
			// Already visited this derivation.
			continue
		}

		drv := derivations[ref.DrvPath]
		if drv == nil {
			return result, fmt.Errorf("analyze %s: unknown derivation", ref.DrvPath)
		}
		// Ensure we have a node for every derivation.
		result.get(ref.DrvPath, drv)

		h, err := pseudoHashDrv(drv)
		if err != nil {
			return nil, fmt.Errorf("analyze %s: %v", ref.DrvPath, err)
		}
		hk := makeHashKey(h)
		drvHashes[ref.DrvPath] = hk
		addToMultiMap(used, hk, unique.Make(ref.OutputName))

		// Fill in reverse dependency graph.
		if len(drv.InputDerivations) == 0 {
			result.roots.Add(ref.DrvPath)
		} else {
			for inputDrvPath, outputNames := range drv.InputDerivations {
				inputNode := result.get(inputDrvPath, derivations[inputDrvPath])
				if inputNode.dependents == nil {
					inputNode.dependents = make(sets.Set[zbstore.Path])
				}
				inputNode.dependents.Add(ref.DrvPath)
				for outputName := range outputNames.Values() {
					stack = append(stack, zbstore.OutputReference{
						DrvPath:    inputDrvPath,
						OutputName: outputName,
					})
				}
			}
		}
	}

	// Fill in the usedOutputs as a separate pass.
	// If we had multiple derivations that are structurally the same,
	// they may use distinct output sets and we want to build the outputs.
	//
	// Multi-output derivations are particularly troublesome for us
	// because if we realize they need to be built
	// after we've already picked a realization for one of the outputs,
	// the build can invalidate the usage of other realizations.
	// (However, this can only occur if more than one output is used in the build.)
	// As long as the derivation is *mostly* deterministic,
	// then we have a good shot of being able to reuse more realizations throughout the rest of the build process
	// because of the early cutoff optimization from content-addressing.
	for drvPath, currentNode := range result.nodes {
		currentNode.usedOutputs = used[drvHashes[drvPath]]
	}

	return result, nil
}

func addToMultiMap[K comparable, V comparable, M ~map[K]sets.Set[V]](m M, k K, v V) {
	dst := m[k]
	if dst == nil {
		dst = make(sets.Set[V])
		m[k] = dst
	}
	dst.Add(v)
}
