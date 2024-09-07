// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package backend

import (
	"fmt"
	"maps"
	"slices"

	"zombiezen.com/go/nix"
	"zombiezen.com/go/zb/internal/xslices"
	"zombiezen.com/go/zb/zbstore"
)

// equivalenceClass is an equivalence class of [zbstore.OutputReference] values.
// It represents a single output of equivalent derivations.
type equivalenceClass struct {
	drvHashString string
	outputName    string
}

func newEquivalenceClass(drvHash nix.Hash, outputName string) equivalenceClass {
	if drvHash.IsZero() || outputName == "" {
		panic("both equivalence class fields must be set")
	}
	return equivalenceClass{
		drvHashString: drvHash.SRI(),
		outputName:    outputName,
	}
}

func (eqClass equivalenceClass) drvHash() (nix.Hash, error) {
	if eqClass.isZero() {
		return nix.Hash{}, nil
	}
	return nix.ParseHash(eqClass.drvHashString)
}

func (eqClass equivalenceClass) isZero() bool {
	return eqClass == equivalenceClass{}
}

func (eqClass equivalenceClass) String() string {
	if eqClass.isZero() {
		return "ε"
	}
	return eqClass.drvHashString + "!" + eqClass.outputName
}

type pathAndEquivalenceClass struct {
	path             zbstore.Path
	equivalenceClass equivalenceClass
}

// hashDrvs computes the equivalence classes for the given derivations.
// hashDrvs returns an error
// if the derivations contain references to derivations not present in the map.
func hashDrvs(derivations map[zbstore.Path]*zbstore.Derivation) (map[zbstore.Path]nix.Hash, error) {
	stack := slices.Collect(maps.Keys(derivations))
	result := make(map[zbstore.Path]nix.Hash)
	for len(stack) > 0 {
		curr := xslices.Last(stack)
		if _, visited := result[curr]; visited {
			stack = xslices.Pop(stack, 1)
			continue
		}

		drv := derivations[curr]
		if drv == nil {
			return nil, fmt.Errorf("hash derivations: %s: missing", curr)
		}

		if ca, ok := drv.Outputs[zbstore.DefaultDerivationOutputName].FixedCA(); ok && len(drv.Outputs) == 1 {
			p, err := zbstore.FixedCAOutputPath(drv.Dir, drv.Name, ca, zbstore.References{})
			if err != nil {
				return nil, fmt.Errorf("hash derivations: %s: %v", curr, err)
			}
			result[curr] = hashDrvFixed(ca, p)
			stack = xslices.Pop(stack, 1)
			continue
		}

		unhashedDeps := false
		for inputDrvPath := range drv.InputDerivations {
			if _, visited := result[inputDrvPath]; !visited {
				stack = append(stack, inputDrvPath)
				unhashedDeps = true
			}
		}
		if unhashedDeps {
			continue
		}

		atermData, err := drv.Marshal(&zbstore.MarshalDerivationOptions{
			MapInputDerivation: func(p zbstore.Path) string {
				return result[p].RawBase16()
			},
		})
		if err != nil {
			return nil, fmt.Errorf("hash derivations: %s: %v", curr, err)
		}
		h := nix.NewHasher(nix.SHA256)
		h.Write(atermData)
		result[curr] = h.SumHash()
		stack = xslices.Pop(stack, 1)
	}
	return result, nil
}

// hashDrvFixed computes the equivalence class for a fixed-output derivation.
func hashDrvFixed(ca zbstore.ContentAddress, outputPath zbstore.Path) nix.Hash {
	h2 := nix.NewHasher(nix.SHA256)
	h2.WriteString("fixed:out:")
	switch {
	case ca.IsText():
		h2.WriteString("text:")
	case ca.IsRecursiveFile():
		h2.WriteString("r:")
	}
	h2.WriteString(ca.Hash().Base16())
	h2.WriteString(":")
	h2.WriteString(string(outputPath))
	return h2.SumHash()
}
