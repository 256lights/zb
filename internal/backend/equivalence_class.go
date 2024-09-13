// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package backend

import (
	"fmt"
	"maps"
	"unique"

	"zombiezen.com/go/nix"
	"zombiezen.com/go/zb/sets"
	"zombiezen.com/go/zb/zbstore"
)

// equivalenceClass is an equivalence class of [zbstore.OutputReference] values.
// It represents a single output of equivalent derivations.
type equivalenceClass struct {
	drvHashKey hashKey
	outputName unique.Handle[string]
}

func newEquivalenceClass(drvHash nix.Hash, outputName string) equivalenceClass {
	if drvHash.IsZero() || outputName == "" {
		panic("both equivalence class fields must be set")
	}
	return equivalenceClass{
		drvHashKey: makeHashKey(drvHash),
		outputName: unique.Make(outputName),
	}
}

func (eqClass equivalenceClass) isZero() bool {
	return eqClass == equivalenceClass{}
}

func (eqClass equivalenceClass) String() string {
	if eqClass.isZero() {
		return "ε"
	}
	return eqClass.drvHashKey.toHash().String() + "!" + eqClass.outputName.Value()
}

type pathAndEquivalenceClass struct {
	path             zbstore.Path
	equivalenceClass equivalenceClass
}

// hashDrv computes the hash for the given derivation
// based on the realizations of its input derivations.
// This hash is intended for use in [newEquivalenceClass].
func hashDrv(drv *zbstore.Derivation, realization func(ref zbstore.OutputReference) (zbstore.Path, bool)) (nix.Hash, error) {
	if drv.Outputs[zbstore.DefaultDerivationOutputName].IsFixed() {
		return hashDrvFixed(drv)
	}

	rewrites, err := derivationInputRewrites(drv, realization)
	if err != nil {
		return nix.Hash{}, fmt.Errorf("hash derivation: %v", err)
	}
	expandedDrv := expandDerivationPlaceholders(newReplacer(maps.All(rewrites)), drv)
	expandedDrv.InputDerivations = nil
	expandedDrv.InputSources.AddSeq(maps.Values(rewrites))
	return hashDrvFloating(expandedDrv)
}

// pseudoHashDrv computes a hash of a derivation
// that can be used for comparing derivations for structural similarity.
// If hashDrv(drv1) == hashDrv(drv2),
// then pseudoHashDrv(drv1) == pseudoHashDrv(drv2)
// (but the converse is not necessarily true).
func pseudoHashDrv(drv *zbstore.Derivation) (nix.Hash, error) {
	if drv.Outputs[zbstore.DefaultDerivationOutputName].IsFixed() {
		return hashDrvFixed(drv)
	}

	var pseudoInputs sets.Sorted[zbstore.Path]
	const fakeDigest = "00000000000000000000000000000000"
	for _, input := range drv.InputSources.All() {
		rewritten, err := input.Dir().Object(fakeDigest + "-" + input.Name())
		if err != nil {
			return nix.Hash{}, fmt.Errorf("hash derivation: %v", err)
		}
		pseudoInputs.Add(rewritten)
	}
	rewrites := make(map[string]zbstore.Path)
	for input := range drv.InputDerivationOutputs() {
		inputDrvName, ok := input.DrvPath.DerivationName()
		if !ok {
			return nix.Hash{}, fmt.Errorf("hash derivation: invalid input derivation %s", input.DrvPath)
		}
		base := fakeDigest + "-" + inputDrvName
		if input.OutputName != zbstore.DefaultDerivationOutputName {
			base += "-" + input.OutputName
		}
		rewritten, err := input.DrvPath.Dir().Object(base)
		if err != nil {
			return nix.Hash{}, fmt.Errorf("hash derivation: %v", err)
		}
		placeholder := zbstore.UnknownCAOutputPlaceholder(input)
		pseudoInputs.Add(rewritten)
		rewrites[placeholder] = rewritten
	}

	expandedDrv := expandDerivationPlaceholders(newReplacer(maps.All(rewrites)), drv)
	expandedDrv.InputDerivations = nil
	expandedDrv.InputSources = pseudoInputs
	return hashDrvFloating(expandedDrv)
}

// hashDrvFixed computes the equivalence class for a fixed-output derivation.
func hashDrvFixed(drv *zbstore.Derivation) (nix.Hash, error) {
	ca, isFixed := drv.Outputs[zbstore.DefaultDerivationOutputName].FixedCA()
	if !isFixed || len(drv.Outputs) != 1 {
		return nix.Hash{}, fmt.Errorf("hash derivation: not fixed")
	}
	outputPath, err := drv.OutputPath(zbstore.DefaultDerivationOutputName)
	if err != nil {
		return nix.Hash{}, fmt.Errorf("hash derivation: %v", err)
	}
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
	return h2.SumHash(), nil
}

func hashDrvFloating(expandedDrv *zbstore.Derivation) (nix.Hash, error) {
	atermData, err := expandedDrv.MarshalText()
	if err != nil {
		return nix.Hash{}, fmt.Errorf("hash derivation: %v", err)
	}
	h := nix.NewHasher(nix.SHA256)
	h.WriteString("floating:")
	h.WriteString(expandedDrv.Name)
	h.WriteString(":") // ':' guaranteed not to appear in a store object name.
	h.Write(atermData)
	return h.SumHash(), nil
}

// hashKey is a copy of a [nix.Hash] that can be efficiently compared for equality.
type hashKey unique.Handle[string]

func makeHashKey(h nix.Hash) hashKey {
	if h.IsZero() {
		return hashKey{}
	}
	return hashKey(unique.Make(h.SRI()))
}

func (hk hashKey) isZero() bool {
	return hk == hashKey{}
}

func (hk hashKey) toHash() nix.Hash {
	if hk.isZero() {
		return nix.Hash{}
	}
	h, err := nix.ParseHash(unique.Handle[string](hk).Value())
	if err != nil {
		panic(err)
	}
	return h
}
