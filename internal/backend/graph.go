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

// findMultiOutputDerivationsInBuild identifies the set of derivations required to build the want set
// that have more than one used output.
func findMultiOutputDerivationsInBuild(derivations map[zbstore.Path]*zbstore.Derivation, want sets.Set[zbstore.OutputReference]) (map[zbstore.Path]sets.Set[string], error) {
	drvHashes := make(map[zbstore.Path]hashKey)
	used := make(map[hashKey]sets.Set[unique.Handle[string]])
	drvPathMap := make(map[hashKey]sets.Set[zbstore.Path])
	stack := slices.Collect(want.All())
	for len(stack) > 0 {
		curr := xslices.Last(stack)
		stack = xslices.Pop(stack, 1)

		drv := derivations[curr.DrvPath]
		if drv == nil {
			return nil, fmt.Errorf("%s: unknown derivation", curr.DrvPath)
		}

		hk, hashed := drvHashes[curr.DrvPath]
		if !hashed {
			h, err := pseudoHashDrv(drv)
			if err != nil {
				return nil, fmt.Errorf("%s: %v", curr.DrvPath, err)
			}
			hk = makeHashKey(h)
			drvHashes[curr.DrvPath] = hk
		}
		if used[hk].Len() == 0 {
			used[hk] = make(sets.Set[unique.Handle[string]])
			stack = slices.AppendSeq(stack, derivations[curr.DrvPath].InputDerivationOutputs())
		}
		addToMultiMap(used, hk, unique.Make(curr.OutputName))
		addToMultiMap(drvPathMap, hk, curr.DrvPath)
	}
	result := make(map[zbstore.Path]sets.Set[string])
	for drvHash, usedOutputNames := range used {
		if usedOutputNames.Len() > 1 {
			for drvPath := range drvPathMap[drvHash].All() {
				for outputName := range usedOutputNames.All() {
					addToMultiMap(result, drvPath, outputName.Value())
				}
			}
		}
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
