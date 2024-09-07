// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package backend

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"zombiezen.com/go/batchio"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
	"zombiezen.com/go/zb/internal/detect"
	"zombiezen.com/go/zb/internal/jsonrpc"
	"zombiezen.com/go/zb/internal/storepath"
	"zombiezen.com/go/zb/internal/system"
	"zombiezen.com/go/zb/internal/xiter"
	"zombiezen.com/go/zb/internal/xmaps"
	"zombiezen.com/go/zb/internal/xslices"
	"zombiezen.com/go/zb/sets"
	"zombiezen.com/go/zb/zbstore"
)

func (s *Server) realize(ctx context.Context, req *jsonrpc.Request) (_ *jsonrpc.Response, err error) {
	// Validate request.
	var args zbstore.RealizeRequest
	if err := json.Unmarshal(req.Params, &args); err != nil {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
	}
	ultimateDrvPath, subPath, err := s.dir.ParsePath(string(args.DrvPath))
	if err != nil {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
	}
	if subPath != "" {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, fmt.Errorf("%s is not a store object", args.DrvPath))
	}
	if _, isDrv := ultimateDrvPath.DerivationName(); !isDrv {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, fmt.Errorf("%s is not a derivation", ultimateDrvPath))
	}
	log.Infof(ctx, "Requested to build %s", ultimateDrvPath)
	if string(s.dir) != s.realDir {
		return nil, fmt.Errorf("store cannot build derivations (unsandboxed and storage directory does not match store)")
	}

	drvCache, err := s.readDerivationClosure(ctx, []zbstore.Path{ultimateDrvPath})
	if err != nil {
		return nil, fmt.Errorf("build %s: %v", ultimateDrvPath, err)
	}
	drvHashes, err := hashDrvs(drvCache)
	if err != nil {
		return nil, fmt.Errorf("build %s: %v", ultimateDrvPath, err)
	}

	conn, err := s.db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer s.db.Put(conn)

	b := &builder{
		server:      s,
		conn:        conn,
		derivations: drvCache,
		drvHashes:   drvHashes,

		realizations: make(map[equivalenceClass]realizationOutput),
	}
	wantOutputs := make(sets.Set[zbstore.OutputReference])
	for outputName := range b.derivations[ultimateDrvPath].Outputs {
		wantOutputs.Add(zbstore.OutputReference{
			DrvPath:    ultimateDrvPath,
			OutputName: outputName,
		})
	}

	// First, see if there is a compatible set of usable realizations for those requested.
	// If so, we can use those directly.
	mustBuild := false
	for ref := range wantOutputs.All() {
		eqClass, ok := b.toEquivalenceClass(ref)
		if !ok {
			return nil, fmt.Errorf("build %s: missing hash for %v", ultimateDrvPath, ref)
		}
		newRealizations, err := b.pickRealizationsToReuse(ctx, eqClass)
		if err != nil {
			return nil, fmt.Errorf("build %s: %v", ultimateDrvPath, err)
		}
		if len(newRealizations) == 0 {
			mustBuild = true
			break
		}
		maps.Copy(b.realizations, newRealizations)
	}

	if mustBuild {
		// We know we're building *something*.
		// Multi-output derivations are particularly troublesome for us
		// because if they need to be rebuilt, they can invalidate the usage of other realizations.
		// We only allow realizations to be used for multi-output derivations
		// if only one output from the derivation is used in the graph.
		//
		// Here's the reasoning:
		// If a multi-output derivation's closure is deterministic,
		// then we should only have a single known realization for each output.
		// If the closure is not deterministic, we want to pick a set of outputs that were built together.
		// If the derivation is *mostly* deterministic,
		// then we have a good shot of being able to reuse more realizations throughout the rest of the build process
		// because of the early cutoff optimization from content-addressing.

		clear(b.realizations)
		multiOutputs, err := findMultiOutputDerivationsInBuild(b.derivations, b.drvHashes, wantOutputs)
		if err != nil {
			return nil, fmt.Errorf("build %s: %v", ultimateDrvPath, err)
		}

		// We perform a two-phase build.
		// In the first phase, we realize any multi-output derivations
		// and forbid usage of realizations for those targets specifically.
		if err := b.realize(ctx, multiOutputs, false); err != nil {
			if isBuilderFailure(err) {
				return marshalResponse(b.makeResponse(ultimateDrvPath))
			}
			return nil, fmt.Errorf("build %s: %v", ultimateDrvPath, err)
		}
		// In the second phase, we realize our originally requested outputs.
		// This will reuse the realizations from the previous phase.
		if err := b.realize(ctx, wantOutputs, true); err != nil {
			if isBuilderFailure(err) {
				return marshalResponse(b.makeResponse(ultimateDrvPath))
			}
			return nil, fmt.Errorf("build %s: %v", ultimateDrvPath, err)
		}
	}

	return marshalResponse(b.makeResponse(ultimateDrvPath))
}

type builder struct {
	server *Server
	conn   *sqlite.Conn

	derivations  map[zbstore.Path]*zbstore.Derivation
	drvHashes    map[zbstore.Path]nix.Hash
	realizations map[equivalenceClass]realizationOutput
}

func (b *builder) makeResponse(drvPath zbstore.Path) *zbstore.RealizeResponse {
	resp := new(zbstore.RealizeResponse)
	for _, outputName := range xmaps.SortedKeys(b.derivations[drvPath].Outputs) {
		var p zbstore.Nullable[zbstore.Path]
		p.X, p.Valid = b.lookup(zbstore.OutputReference{
			DrvPath:    drvPath,
			OutputName: outputName,
		})
		resp.Outputs = append(resp.Outputs, &zbstore.RealizeOutput{
			Name: outputName,
			Path: p,
		})
	}
	return resp
}

func (b *builder) toEquivalenceClass(ref zbstore.OutputReference) (_ equivalenceClass, ok bool) {
	if ref.OutputName == "" {
		return equivalenceClass{}, false
	}
	h := b.drvHashes[ref.DrvPath]
	if h.IsZero() {
		return equivalenceClass{}, false
	}
	return newEquivalenceClass(h, ref.OutputName), true
}

// lookup returns the realized path for the given output if the builder realized one.
func (b *builder) lookup(ref zbstore.OutputReference) (_ zbstore.Path, ok bool) {
	eqClassRef, ok := b.toEquivalenceClass(ref)
	if !ok {
		return "", false
	}
	r, ok := b.realizations[eqClassRef]
	return r.path, ok
}

func (b *builder) realize(ctx context.Context, want sets.Set[zbstore.OutputReference], useExistingForWant bool) error {
	drvLocks := make(map[zbstore.Path]func())
	defer func() {
		for _, unlock := range drvLocks {
			unlock()
		}
	}()

	var ignoreExistingDrvSet sets.Set[string]
	if !useExistingForWant {
		ignoreExistingDrvSet = make(sets.Set[string])
		for output := range want.All() {
			drvHash := b.drvHashes[output.DrvPath]
			if drvHash.IsZero() {
				return fmt.Errorf("realize %v: missing hash", output)
			}
			ignoreExistingDrvSet.Add(drvHash.SRI())
		}
	}

	stack := slices.Collect(want.All())
	for len(stack) > 0 {
		curr := xslices.Last(stack)
		stack = xslices.Pop(stack, 1)

		if _, realized := b.lookup(curr); realized {
			continue
		}
		drv := b.derivations[curr.DrvPath]
		if drv == nil {
			return fmt.Errorf("realize %v: unknown derivation", curr)
		}
		if _, ok := drv.Outputs[curr.OutputName]; !ok {
			return fmt.Errorf("realize %v: unknown output %q", curr, curr.OutputName)
		}

		if _, intendToBuild := drvLocks[curr.DrvPath]; intendToBuild {
			log.Debugf(ctx, "Resuming %s", curr.DrvPath)
		} else {
			// First visit to derivation.
			log.Debugf(ctx, "Reached %v", curr)
			drvHash := b.drvHashes[curr.DrvPath]
			if drvHash.IsZero() {
				return fmt.Errorf("realize %v: missing hash", curr)
			}
			unlock, err := b.server.building.lock(ctx, curr.DrvPath)
			if err != nil {
				return err
			}
			drvLocks[curr.DrvPath] = unlock
			log.Debugf(ctx, "Acquired lock on %s", curr.DrvPath)
			if err := b.preprocess(ctx, curr, !ignoreExistingDrvSet.Has(drvHash.SRI())); err != nil {
				return err
			}
			if _, realized := b.lookup(curr); realized {
				drvLocks[curr.DrvPath]()
				delete(drvLocks, curr.DrvPath)
				continue
			}
			unmetDependencies := false
			for ref := range drv.InputDerivationOutputs() {
				if _, realized := b.lookup(ref); !realized {
					log.Debugf(ctx, "Enqueuing %s (dependency of %s)", ref, curr.DrvPath)
					if !unmetDependencies {
						stack = append(stack, curr)
						unmetDependencies = true
					}
					stack = append(stack, ref)
				}
			}
			if unmetDependencies {
				log.Debugf(ctx, "Pausing %s to build dependencies", curr.DrvPath)
				continue
			}
		}

		if err := b.do(ctx, curr.DrvPath); err != nil {
			return err
		}
		drvLocks[curr.DrvPath]()
		delete(drvLocks, curr.DrvPath)
	}

	return nil
}

// preprocess checks whether the given output has existing realizations
// or otherwise ensures the derivation is suitable for realizing.
func (b *builder) preprocess(ctx context.Context, output zbstore.OutputReference, useExisting bool) error {
	eqClass, ok := b.toEquivalenceClass(output)
	if !ok {
		return fmt.Errorf("%s: unknown derivation", output.DrvPath)
	}

	if useExisting {
		newRealizations, err := b.pickRealizationsToReuse(ctx, eqClass)
		if err != nil {
			return err
		}
		if len(newRealizations) > 0 {
			maps.Copy(b.realizations, newRealizations)
			return nil
		}
	}

	drv := b.derivations[output.DrvPath]
	if drv == nil {
		return fmt.Errorf("build %s: unknown derivation", output.DrvPath)
	}
	if _, hasOutput := drv.Outputs[output.OutputName]; !hasOutput {
		return fmt.Errorf("build %s: unknown output %q", output.DrvPath, output.OutputName)
	}
	if outputPath, err := drv.OutputPath(output.OutputName); err == nil {
		// Fixed output.
		log.Debugf(ctx, "%s has fixed output %s. Waiting for lock to check for reuse...", output.DrvPath, outputPath)
		unlockFixedOutput, err := b.server.writing.lock(ctx, outputPath)
		if err != nil {
			return fmt.Errorf("build %s: wait for %s: %w", output.DrvPath, outputPath, err)
		}
		_, err = os.Lstat(b.server.realPath(outputPath))
		unlockFixedOutput()
		log.Debugf(ctx, "%s exists=%t (output of %s)", outputPath, err == nil, output.DrvPath)

		if err == nil {
			drvHash := b.drvHashes[output.DrvPath]
			// Fixed output derivations have a single output
			// and their outputs must contain no references.
			rout := realizationOutput{path: outputPath}
			outputPaths := map[string]realizationOutput{
				output.OutputName: rout,
			}
			// TODO(soon): Wrap in immediate transaction.
			if err := recordRealizations(ctx, b.conn, drvHash, outputPaths); err != nil {
				return fmt.Errorf("build %s: %v", output.DrvPath, err)
			}
			b.realizations[eqClass] = rout
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	if !canBuildLocally(drv) {
		return fmt.Errorf("build %s: a %s system is required, but host is a %v system",
			output.DrvPath, drv.System, system.Current())
	}
	for _, input := range drv.InputSources.All() {
		log.Debugf(ctx, "Waiting for lock on %s (input to %s)...", input, output.DrvPath)
		unlockInput, err := b.server.writing.lock(ctx, input)
		if err != nil {
			return fmt.Errorf("build %s: wait for %s: %w", output.DrvPath, input, err)
		}
		_, err = os.Lstat(b.server.realPath(input))
		unlockInput()
		log.Debugf(ctx, "%s exists=%t (input to %s)", input, err == nil, output.DrvPath)
		if err != nil {
			// TODO(someday): Import from substituter if not found.
			return fmt.Errorf("build %s: input %s not present (%v)", output.DrvPath, input, err)
		}
	}
	return nil
}

// pickRealizationsToReuse picks a realization to use for a derivation output
// from the given set of existing store paths
// that is compatible with existing realizations in the builder.
// pickRealizationsToReuse can return realizations for multiple equivalence classes
// because selecting a realization may imply selecting realizations from its closure.
// If the returned map is empty,
// then no compatible realizations were found.
func (b *builder) pickRealizationsToReuse(ctx context.Context, eqClass equivalenceClass) (newRealizations map[equivalenceClass]realizationOutput, err error) {
	isCompatible := func(ref pathAndEquivalenceClass) bool {
		if ref.equivalenceClass.isZero() {
			// Sources can't conflict.
			return true
		}
		used, hasExisting := b.realizations[ref.equivalenceClass]
		return !hasExisting || ref.path == used.path
	}

	existing, err := findPossibleRealizations(ctx, b.conn, eqClass)
	if err != nil {
		return nil, err
	}
	rout := realizationOutput{
		references: make(map[zbstore.Path]sets.Set[equivalenceClass]),
	}
	remaining := existing.Clone()
	for outputPath := range existing.All() {
		log.Debugf(ctx, "Checking whether %s can be used as %v...", outputPath, eqClass)
		remaining.Delete(outputPath)

		pe := pathAndEquivalenceClass{
			path:             outputPath,
			equivalenceClass: eqClass,
		}
		clear(rout.references)
		canUse := true
		err := closurePaths(b.conn, pe, func(ref pathAndEquivalenceClass) bool {
			canUse = isCompatible(ref)
			if canUse {
				addToMultiMap(rout.references, ref.path, ref.equivalenceClass)
			} else {
				log.Debugf(ctx, "Cannot use %s as %v: depends on %s (need %s)",
					outputPath, eqClass, ref.path, b.realizations[ref.equivalenceClass].path)
			}
			return canUse
		})
		if err != nil {
			return nil, fmt.Errorf("pick compatible realization for %v: %v", eqClass, err)
		}
		if canUse {
			rout.path = outputPath
			break
		}
	}

	if rout.path == "" {
		return nil, nil
	}

	// TODO(someday): In the case where there are multiple valid candidates,
	// we should use a heuristic and/or consult the client's trust settings.
	// Until then, we bias towards rebuilding.
	for outputPath := range remaining.All() {
		pe := pathAndEquivalenceClass{
			path:             outputPath,
			equivalenceClass: eqClass,
		}
		canUse := true
		err := closurePaths(b.conn, pe, func(ref pathAndEquivalenceClass) bool {
			canUse = isCompatible(ref)
			return canUse
		})
		if err != nil {
			return nil, fmt.Errorf("pick compatible realization for %v: %v", eqClass, err)
		}
		if canUse {
			// Multiple realizations are compatible.
			// Return none of them.
			return nil, nil
		}
	}

	// Now that we have a candidate, fill out the closures.
	newRealizations = map[equivalenceClass]realizationOutput{
		eqClass: rout,
	}
	for refPath, eqClasses := range rout.references {
		for eqClass := range eqClasses.All() {
			if _, exists := b.realizations[eqClass]; exists {
				continue
			}
			if _, alreadyLookedUp := newRealizations[eqClass]; alreadyLookedUp {
				continue
			}
			pe := pathAndEquivalenceClass{
				path:             refPath,
				equivalenceClass: eqClass,
			}
			closureOutput := realizationOutput{
				path:       refPath,
				references: make(map[zbstore.Path]sets.Set[equivalenceClass]),
			}
			err := closurePaths(b.conn, pe, func(pe pathAndEquivalenceClass) bool {
				addToMultiMap(closureOutput.references, pe.path, pe.equivalenceClass)
				return true
			})
			if err != nil {
				return nil, fmt.Errorf("pick compatible realization for %v: %v", eqClass, err)
			}
			newRealizations[eqClass] = closureOutput
		}
	}

	return newRealizations, nil
}

// do runs a derivation's builder and records the realizations.
// The caller must have realized all of the derivation's inputs before calling do.
func (b *builder) do(ctx context.Context, drvPath zbstore.Path) (err error) {
	drv := b.derivations[drvPath]
	if drv == nil {
		return fmt.Errorf("build %s: unknown derivation", drvPath)
	}
	drvHash := b.drvHashes[drvPath]
	if drvHash.IsZero() {
		return fmt.Errorf("build %s: derivation not hashed", drvPath)
	}

	// No-op if the derivation has already been realized.
	fullyRealized := xiter.All(maps.Keys(drv.Outputs), func(outputName string) bool {
		_, realized := b.realizations[newEquivalenceClass(drvHash, outputName)]
		return realized
	})
	if fullyRealized {
		return nil
	}

	// If fixed output, acquire write lock on output path.
	var unlockFixedOutput func()
	if outputPath, err := drv.OutputPath(zbstore.DefaultDerivationOutputName); err == nil {
		log.Debugf(ctx, "%s has fixed output %s. Waiting for lock to check for reuse...", drvPath, outputPath)
		unlock, err := b.server.writing.lock(ctx, outputPath)
		if err != nil {
			return fmt.Errorf("build %s: wait for %s: %w", drvPath, outputPath, err)
		}
		var unlockOnce sync.Once
		unlockFixedOutput = func() {
			unlockOnce.Do(func() {
				unlock()
			})
		}
		defer unlockFixedOutput()

		// It's possible that another build produced our fixed output
		// since pre-processing.
		_, err = os.Lstat(b.server.realPath(outputPath))
		log.Debugf(ctx, "%s exists=%t (output of %s)", outputPath, err == nil, drvPath)
		if err == nil {
			outputs := map[string]realizationOutput{
				zbstore.DefaultDerivationOutputName: {
					path: outputPath,
					// Fixed outputs don't have references.
				},
			}
			if err := b.recordRealizations(ctx, drvHash, outputs); err != nil {
				return fmt.Errorf("build %s: %v", drvPath, err)
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("build %s: %v", drvPath, err)
		}
	}

	// Arrange for builder to run.
	runFunc := runnerFunc(runSubprocess)
	if drv.System == builtinSystem {
		runFunc = runBuiltin
	}
	tempOutPaths, err := b.runUnsandboxed(ctx, drvPath, runFunc)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			for _, outPath := range tempOutPaths {
				realOutPath := b.server.realPath(outPath)
				if err := os.RemoveAll(realOutPath); err != nil {
					log.Warnf(ctx, "Cleanup failure: %v", err)
				}
			}
		}
	}()

	// Save outputs as store objects.
	inputs, err := b.inputs(ctx, drvPath)
	if err != nil {
		return err
	}
	inputPaths := sets.CollectSorted(maps.Keys(inputs))
	outputs := make(map[string]realizationOutput)
	for outputName, tempOutputPath := range tempOutPaths {
		ref := zbstore.OutputReference{
			DrvPath:    drvPath,
			OutputName: outputName,
		}
		info, err := b.postprocess(ctx, ref, tempOutputPath, unlockFixedOutput, inputPaths)
		if err != nil {
			return fmt.Errorf("build %s: %v", drvPath, err)
		}
		delete(tempOutPaths, outputName) // No longer needs cleanup if we fail.

		prev, previouslyRealized := b.realizations[newEquivalenceClass(drvHash, outputName)]
		if previouslyRealized && info.StorePath != prev.path {
			// This should have been prevented at a higher level,
			// but we do a safety check here anyway.
			return fmt.Errorf("build %s: output %s: new path %s conflicts with existing %s",
				drvPath, outputName, info.StorePath, prev.path)
		}

		outputs[outputName] = realizationOutput{
			path: info.StorePath,
			references: maps.Collect(func(yield func(zbstore.Path, sets.Set[equivalenceClass]) bool) {
				for ref, eqClasses := range inputs {
					if info.References.Has(ref) {
						if !yield(ref, eqClasses) {
							return
						}
					}
				}
			}),
		}
	}

	// Record realizations.
	if err := b.recordRealizations(ctx, drvHash, outputs); err != nil {
		return fmt.Errorf("build %s: %v", drvPath, err)
	}

	return nil
}

// inputs computes the closure of all inputs used by the derivation at drvPath.
func (b *builder) inputs(ctx context.Context, drvPath zbstore.Path) (map[zbstore.Path]sets.Set[equivalenceClass], error) {
	drv := b.derivations[drvPath]
	if drv == nil {
		return nil, fmt.Errorf("input closure for %s: unknown derivation", drvPath)
	}
	result := make(map[zbstore.Path]sets.Set[equivalenceClass])
	for input := range drv.InputDerivationOutputs() {
		eqClass, ok := b.toEquivalenceClass(input)
		if !ok {
			return nil, fmt.Errorf("input closure for %s: missing derivation hash for %v", drvPath, input)
		}
		out, ok := b.realizations[eqClass]
		if !ok {
			return nil, fmt.Errorf("input closure for %s: missing realization for %v", drvPath, input)
		}
		for refPath, refClasses := range out.references {
			dst := result[refPath]
			if dst == nil {
				dst = make(sets.Set[equivalenceClass])
				result[refPath] = dst
			}
			dst.AddSeq(refClasses.All())
		}
	}

	startedTransaction := b.conn.AutocommitEnabled()
	if err := sqlitex.Execute(b.conn, "SAVEPOINT inputs;", nil); err != nil {
		return nil, fmt.Errorf("input closure for %s: %v", drvPath, err)
	}
	defer func() {
		var sql string
		if startedTransaction {
			sql = "ROLLBACK TRANSACTION;"
		} else {
			sql = "ROLLBACK TRANSACTION TO SAVEPOINT inputs;"
		}
		if err := sqlitex.Execute(b.conn, sql, nil); err != nil {
			log.Errorf(ctx, "Rollback read-only savepoint: %v", err)
		}
	}()

	for _, input := range drv.InputSources.All() {
		err := closurePaths(b.conn, pathAndEquivalenceClass{path: input}, func(pe pathAndEquivalenceClass) bool {
			addToMultiMap(result, pe.path, pe.equivalenceClass)
			return true
		})
		if err != nil {
			return nil, fmt.Errorf("input closure for %s: %v", drvPath, err)
		}
	}

	return result, nil
}

type runnerFunc func(ctx context.Context, drv *zbstore.Derivation, dir string, logWriter io.Writer) error

func (b *builder) runUnsandboxed(ctx context.Context, drvPath zbstore.Path, f runnerFunc) (map[string]zbstore.Path, error) {
	drvName, isDrv := drvPath.DerivationName()
	if !isDrv {
		return nil, fmt.Errorf("build %s: not a derivation", drvPath)
	}
	drv := b.derivations[drvPath]
	if drv == nil {
		return nil, fmt.Errorf("build %s: unknown derivation", drvPath)
	}

	outPaths, err := tempOutputPaths(drvPath, drv.Outputs)
	if err != nil {
		return nil, fmt.Errorf("build %s: %v", drvPath, err)
	}
	if log.IsEnabled(log.Debug) {
		log.Debugf(ctx, "Output map for %s: %s", drvPath, formatOutputPaths(outPaths))
	}

	topTempDir, err := os.MkdirTemp(b.server.buildDir, "zb-build-"+drvName+"*")
	if err != nil {
		return nil, fmt.Errorf("build %s: %v", drvPath, err)
	}
	defer func() {
		if err := os.RemoveAll(topTempDir); err != nil {
			log.Warnf(ctx, "Failed to clean up %s: %v", topTempDir, err)
		}
	}()

	inputRewrites, err := derivationInputRewrites(b.drvHashes, b.realizations, drv)
	if err != nil {
		return nil, fmt.Errorf("build %s: %v", drvPath, err)
	}
	r := newReplacer(xiter.Chain2(
		outputPathRewrites(outPaths),
		maps.All(inputRewrites),
	))
	expandedDrv := expandDerivationPlaceholders(r, drv)
	baseEnv := make(map[string]string)
	addBaseEnv(baseEnv, drv.Dir, topTempDir)
	for k, v := range baseEnv {
		if _, overridden := expandedDrv.Env[k]; !overridden {
			expandedDrv.Env[k] = v
		}
	}

	peerLogger := newRPCLogger(ctx, drvPath, peer(ctx))
	bufferedPeerLogger := batchio.NewWriter(peerLogger, 8192, 1*time.Second)
	defer bufferedPeerLogger.Flush()

	log.Debugf(ctx, "Starting builder for %s...", drvPath)
	if err := f(ctx, expandedDrv, topTempDir, bufferedPeerLogger); err != nil {
		log.Debugf(ctx, "Builder for %s has failed: %v", drvPath, err)
		for outName, outPath := range outPaths {
			if err := os.RemoveAll(string(outPath)); err != nil {
				ref := zbstore.OutputReference{
					DrvPath:    drvPath,
					OutputName: outName,
				}
				log.Warnf(ctx, "Clean up %v from failed build: %v", ref, err)
			}
		}
		return nil, builderFailure{fmt.Errorf("build %s: %w", drvPath, err)}
	}

	log.Debugf(ctx, "Builder for %s has finished successfully", drvPath)
	return outPaths, nil
}

// runSubprocess runs a builder by running a subprocess.
// It satisfies the [runnerFunc] signature.
func runSubprocess(ctx context.Context, drv *zbstore.Derivation, dir string, logWriter io.Writer) error {
	c := exec.CommandContext(ctx, drv.Builder, drv.Args...)
	setCancelFunc(c)
	for k, v := range xmaps.Sorted(drv.Env) {
		c.Env = append(c.Env, k+"="+v)
	}
	c.Dir = dir
	c.Stdout = logWriter
	c.Stderr = logWriter

	return c.Run()
}

// outputPathRewrites returns an iterator of mappings of output placeholders
// to store paths in outputMap.
func outputPathRewrites(outputMap map[string]zbstore.Path) iter.Seq2[string, zbstore.Path] {
	return func(yield func(string, zbstore.Path) bool) {
		for outputName, outputPath := range outputMap {
			placeholder := zbstore.HashPlaceholder(outputName)
			if !yield(placeholder, outputPath) {
				return
			}
		}
	}
}

// derivationInputRewrites returns a substitution map
// of output placeholders to realization paths.
func derivationInputRewrites(drvHashes map[zbstore.Path]nix.Hash, realizations map[equivalenceClass]realizationOutput, drv *zbstore.Derivation) (map[string]zbstore.Path, error) {
	// TODO(maybe): Also rewrite transitive derivation hashes?
	result := make(map[string]zbstore.Path)
	for ref := range drv.InputDerivationOutputs() {
		placeholder := zbstore.UnknownCAOutputPlaceholder(ref)
		h := drvHashes[ref.DrvPath]
		if h.IsZero() {
			return nil, fmt.Errorf("compute input rewrites: %s: missing hash", ref.DrvPath)
		}
		eqClass := newEquivalenceClass(h, ref.OutputName)
		r := realizations[eqClass]
		if r.path == "" {
			return nil, fmt.Errorf("compute input rewrites: %v: missing realization", ref)
		}
		result[placeholder] = r.path
	}
	return result, nil
}

type replacer interface {
	Replace(s string) string
}

// expandDerivationPlaceholders returns a copy of drv
// with r.Replace applied to its builder, builder arguments, and environment variables.
func expandDerivationPlaceholders(r replacer, drv *zbstore.Derivation) *zbstore.Derivation {
	drv = drv.Clone()
	drv.Builder = r.Replace(drv.Builder)
	if len(drv.Args) > 0 {
		for i, arg := range drv.Args {
			drv.Args[i] = r.Replace(arg)
		}
	}
	oldEnv := drv.Env
	drv.Env = make(map[string]string, len(oldEnv))
	for k, v := range oldEnv {
		drv.Env[r.Replace(k)] = r.Replace(v)
	}
	return drv
}

func tempOutputPaths(drvPath zbstore.Path, outputs map[string]*zbstore.DerivationOutputType) (map[string]zbstore.Path, error) {
	fakeDrv := &zbstore.Derivation{
		Dir:     drvPath.Dir(),
		Outputs: outputs,
	}
	var ok bool
	fakeDrv.Name, ok = drvPath.DerivationName()
	if !ok {
		return nil, fmt.Errorf("compute output paths for %s: not a derivation", drvPath)
	}

	paths := make(map[string]zbstore.Path)
	for outName := range outputs {
		if p, err := fakeDrv.OutputPath(outName); err == nil {
			paths[outName] = p
			continue
		}

		tp, err := tempPath(drvPath, outName)
		if err != nil {
			return nil, err
		}
		paths[outName] = tp
	}
	return paths, nil
}

// postprocess computes the metadata for a realized output
// and ensures that it is recorded in the store.
// inputs is the set of store paths that were inputs for the realized derivation.
// buildPath is the path of the store object created during realization.
// If the output is fixed, then buildPath must be the store path computed by [zbstore.Derivation.OutputPath]
// and unlockBuildPath must be the unlock function obtained from b.server.writing.
// If the outputType is floating,
// then postprocess will move the store object at buildPath to its computed path.
func (b *builder) postprocess(ctx context.Context, output zbstore.OutputReference, buildPath zbstore.Path, unlockBuildPath func(), inputs *sets.Sorted[zbstore.Path]) (*zbstore.NARInfo, error) {
	drv := b.derivations[output.DrvPath]
	if drv == nil {
		return nil, fmt.Errorf("post-process %v: unknown derivation", output)
	}
	outputType, hasOutput := drv.Outputs[output.OutputName]
	if !hasOutput {
		return nil, fmt.Errorf("post-process %v: no such output", output)
	}

	if ca, ok := outputType.FixedCA(); ok {
		if unlockBuildPath == nil {
			return nil, fmt.Errorf("post-process %v: write lock was not held", output)
		}
		defer unlockBuildPath()

		log.Debugf(ctx, "Verifying fixed output %s...", buildPath)
		narHash, narSize, err := postprocessFixedOutput(b.server.realDir, buildPath, ca)
		if err != nil {
			return nil, err
		}
		info := &zbstore.NARInfo{
			StorePath:   buildPath,
			Deriver:     output.DrvPath,
			Compression: nix.NoCompression,
			NARHash:     narHash,
			NARSize:     narSize,
			CA:          ca,
		}
		err = func() (err error) {
			endFn, err := sqlitex.ImmediateTransaction(b.conn)
			if err != nil {
				return err
			}
			defer endFn(&err)
			return insertObject(ctx, b.conn, info)
		}()
		if err != nil {
			err = fmt.Errorf("post-process %v: %v", output, err)
		}
		return info, err
	}

	if unlockBuildPath != nil {
		return nil, fmt.Errorf("post-process %v: unexpected write lock", output)
	}

	// outputType has presumably been validated with [validateOutputs].
	info, err := b.postprocessFloatingOutput(ctx, buildPath, inputs)
	if info != nil {
		info.Deriver = output.DrvPath
	}
	return info, err
}

// postprocessFixedOutput computes the NAR hash of the given store path
// and verifies that it matches the content address.
func postprocessFixedOutput(realStoreDir string, outputPath zbstore.Path, ca zbstore.ContentAddress) (narHash nix.Hash, narSize int64, err error) {
	realOutputPath := filepath.Join(realStoreDir, outputPath.Base())
	wc := new(writeCounter)
	h := nix.NewHasher(nix.SHA256)
	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := nar.DumpPath(io.MultiWriter(wc, h, pw), realOutputPath); err != nil {
			pw.CloseWithError(err)
		} else {
			pw.Close()
		}
	}()
	defer func() {
		pr.Close()
		<-done
	}()

	if _, err := verifyContentAddress(outputPath, pr, nil, ca); err != nil {
		return nix.Hash{}, 0, err
	}
	return h.SumHash(), int64(*wc), nil
}

func (b *builder) postprocessFloatingOutput(ctx context.Context, buildPath zbstore.Path, inputs *sets.Sorted[zbstore.Path]) (*zbstore.NARInfo, error) {
	log.Debugf(ctx, "Processing floating output %s...", buildPath)
	realBuildPath := b.server.realPath(buildPath)
	scan, err := scanFloatingOutput(realBuildPath, buildPath.Digest(), inputs)
	if err != nil {
		return nil, fmt.Errorf("post-process %s: %v", buildPath, err)
	}

	finalPath, err := zbstore.FixedCAOutputPath(buildPath.Dir(), buildPath.Name(), scan.ca, scan.refs)
	if err != nil {
		return nil, fmt.Errorf("post-process %s: %v", buildPath, err)
	}
	log.Debugf(ctx, "Determined %s hashes to %s, acquring lock...", buildPath, finalPath)
	unlock, err := b.server.writing.lock(ctx, finalPath)
	if err != nil {
		return nil, fmt.Errorf("post-process %s: waiting for lock: %w", buildPath, err)
	}
	defer unlock()
	log.Debugf(ctx, "Acquired lock on %s", finalPath)

	info := &zbstore.NARInfo{
		StorePath:   finalPath,
		Compression: zbstore.NoCompression,
		NARSize:     scan.narSize,
		References:  *scan.refs.ToSet(finalPath),
		CA:          scan.ca,
	}
	if !scan.refs.Self {
		info.NARHash = scan.narHash
	}

	// Bail early if this output exists in the store already.
	realFinalPath := b.server.realPath(finalPath)
	if _, err := os.Lstat(realFinalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("post-process %s: %v", buildPath, err)
	} else if err == nil {
		log.Debugf(ctx, "%s is the same output as %s (reusing)", buildPath, finalPath)
		if err := os.RemoveAll(realBuildPath); err != nil {
			log.Warnf(ctx, "Cleanup failure: %v", err)
		}
		return info, nil
	}

	log.Debugf(ctx, "Moving %s to %s (self-references=%t)", realBuildPath, realFinalPath, scan.refs.Self)
	if scan.refs.Self {
		var err error
		info.NARHash, err = finalizeFloatingOutput(finalPath.Dir(), realBuildPath, realFinalPath)
		if err != nil {
			return nil, fmt.Errorf("post-process %s: %v", buildPath, err)
		}
	} else {
		// If there are no self references, we can do a simple rename.
		if err := os.Rename(realBuildPath, realFinalPath); err != nil {
			return nil, fmt.Errorf("post-process %s: %v", buildPath, err)
		}
	}

	err = func() (err error) {
		endFn, err := sqlitex.ImmediateTransaction(b.conn)
		if err != nil {
			return err
		}
		defer endFn(&err)
		return insertObject(ctx, b.conn, info)
	}()
	if err != nil {
		return nil, fmt.Errorf("post-process %v: %v", buildPath, err)
	}

	return info, nil
}

type outputScanResults struct {
	ca      zbstore.ContentAddress
	narHash nix.Hash // only filled in if refs.Self is false
	narSize int64
	refs    zbstore.References
}

// scanFloatingOutput gathers information about a newly built filesystem object.
// The digest is used to detect self references.
// closure is the transitive closure of store objects the derivation depends on,
// which form the superset of all non-self-references that the scan can detect.
func scanFloatingOutput(path string, digest string, closure *sets.Sorted[zbstore.Path]) (*outputScanResults, error) {
	wc := new(writeCounter)
	h := nix.NewHasher(nix.SHA256)
	refFinder := detect.NewRefFinder(func(yield func(string) bool) {
		for _, input := range closure.All() {
			if !yield(input.Digest()) {
				return
			}
		}
	})
	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := nar.DumpPath(io.MultiWriter(wc, h, refFinder, pw), path); err != nil {
			pw.CloseWithError(err)
		} else {
			pw.Close()
		}
	}()
	defer func() {
		pr.Close()
		<-done
	}()

	ca, digestOffsets, err := zbstore.SourceSHA256ContentAddress(digest, pr)
	if err != nil {
		return nil, err
	}

	refs := zbstore.References{
		Self: len(digestOffsets) > 0,
	}
	digestsFound := refFinder.Found()
	for _, digest := range digestsFound.All() {
		// Since all store paths have the same prefix followed by digest,
		// we can use binary search on a sorted set of store paths to find the corresponding digest.
		i, ok := sort.Find(closure.Len(), func(i int) int {
			return strings.Compare(digest, closure.At(i).Digest())
		})
		if !ok {
			return nil, fmt.Errorf("scan internal error: could not find digest %q in inputs", digest)
		}
		refs.Others.Add(closure.At(i))
	}

	result := &outputScanResults{
		ca:      ca,
		narSize: int64(*wc),
		refs:    refs,
	}
	if !refs.Self {
		result.narHash = h.SumHash()
	}
	return result, nil
}

// finalizeFloatingOutput moves a store object on the local filesystem to its final location,
// rewriting any self references as needed.
// The last path element of each path must be a valid store path name,
// the object names must be identical.
// dir is used purely for error messages.
func finalizeFloatingOutput(dir zbstore.Directory, buildPath, finalPath string) (narHash nix.Hash, err error) {
	// TODO(someday): Walk buildPath, renaming files as we go, construct the NAR manually,
	// and rewrite files in place to avoid doubling disk space.

	fakeBuildPath, err := dir.Object(filepath.Base(buildPath))
	if err != nil {
		return nix.Hash{}, fmt.Errorf("move %s to %s: %v", buildPath, finalPath, err)
	}
	fakeFinalPath, err := dir.Object(filepath.Base(finalPath))
	if err != nil {
		return nix.Hash{}, fmt.Errorf("move %s to %s: %v", buildPath, finalPath, err)
	}
	if fakeBuildPath.Name() != fakeFinalPath.Name() {
		return nix.Hash{}, fmt.Errorf("move %s to %s: object names do not match", buildPath, finalPath)
	}
	h := nix.NewHasher(nix.SHA256)
	if filepath.Clean(buildPath) == filepath.Clean(finalPath) {
		// This case shouldn't occur in practice,
		// but make an effort to avoid destroying data if we're renaming to the same location.
		if err := nar.DumpPath(h, buildPath); err != nil {
			return nix.Hash{}, fmt.Errorf("move %s to %s: %v", buildPath, finalPath, err)
		}
		return h.SumHash(), nil
	}

	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := nar.DumpPath(pw, buildPath); err != nil {
			pw.CloseWithError(err)
		} else {
			pw.Close()
		}
	}()
	defer func() {
		pr.Close()
		<-done
	}()
	hmr := detect.NewHashModuloReader(fakeBuildPath.Digest(), fakeFinalPath.Digest(), pr)
	tempDestination := finalPath + ".tmp"
	if err := extractNAR(tempDestination, io.TeeReader(hmr, h)); err != nil {
		os.RemoveAll(tempDestination)
		return nix.Hash{}, fmt.Errorf("move %s to %s: %v", buildPath, finalPath, err)
	}
	if err := os.RemoveAll(buildPath); err != nil {
		os.RemoveAll(tempDestination)
		return nix.Hash{}, fmt.Errorf("move %s to %s: %v", buildPath, finalPath, err)
	}
	if err := os.Rename(tempDestination, finalPath); err != nil {
		os.RemoveAll(tempDestination)
		return nix.Hash{}, fmt.Errorf("move %s to %s: %v", buildPath, finalPath, err)
	}
	return h.SumHash(), nil
}

// recordRealizations calls [recordRealizations] in a transaction
// and on success, saves the realizations into b.realizations.
func (b *builder) recordRealizations(ctx context.Context, drvHash nix.Hash, outputs map[string]realizationOutput) (err error) {
	endFn, err := sqlitex.ImmediateTransaction(b.conn)
	if err != nil {
		return fmt.Errorf("record realizations for %v: %v", drvHash, err)
	}
	defer endFn(&err)
	if err := recordRealizations(ctx, b.conn, drvHash, outputs); err != nil {
		return err
	}
	for outputName, output := range outputs {
		b.realizations[newEquivalenceClass(drvHash, outputName)] = output
	}
	return nil
}

// findMultiOutputDerivationsInBuild identifies the set of derivations required to build the want set
// that have more than one used output.
func findMultiOutputDerivationsInBuild(derivations map[zbstore.Path]*zbstore.Derivation, drvHashes map[zbstore.Path]nix.Hash, want sets.Set[zbstore.OutputReference]) (sets.Set[zbstore.OutputReference], error) {
	used := make(map[string]sets.Set[string])
	drvPathMap := make(map[string]sets.Set[zbstore.Path])
	stack := slices.Collect(want.All())
	for len(stack) > 0 {
		curr := xslices.Last(stack)
		stack = xslices.Pop(stack, 1)

		h := drvHashes[curr.DrvPath]
		if h.IsZero() {
			return nil, fmt.Errorf("missing derivation hash for %s", curr.DrvPath)
		}
		sri := h.SRI()
		if used[sri].Len() == 0 {
			used[sri] = make(sets.Set[string])
			stack = slices.AppendSeq(stack, derivations[curr.DrvPath].InputDerivationOutputs())
		}
		addToMultiMap(used, sri, curr.OutputName)
		addToMultiMap(drvPathMap, sri, curr.DrvPath)
	}
	result := make(sets.Set[zbstore.OutputReference])
	for drvHash, usedOutputNames := range used {
		if usedOutputNames.Len() > 1 {
			for drvPath := range drvPathMap[drvHash].All() {
				for outputName := range usedOutputNames.All() {
					result.Add(zbstore.OutputReference{
						DrvPath:    drvPath,
						OutputName: outputName,
					})
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

func canBuildLocally(drv *zbstore.Derivation) bool {
	if drv.System == builtinSystem {
		return true
	}
	host := system.Current()
	want, err := system.Parse(drv.System)
	if err != nil {
		return false
	}
	if host.OS != want.OS || host.ABI != want.ABI {
		return false
	}
	return want.Arch == host.Arch ||
		want.IsIntel32() && host.IsIntel64() ||
		want.IsARM32() && host.IsARM64()
}

// tempPath generates a [zbstore.Path] that can be used as a temporary build path
// for the given derivation output.
// The path will be unique across the store,
// assuming SHA-256 hash collisions cannot occur.
// tempPath is deterministic:
// given the same drvPath and outputName,
// it will return the same path.
func tempPath(drvPath zbstore.Path, outputName string) (zbstore.Path, error) {
	drvName, ok := drvPath.DerivationName()
	if !ok {
		return "", fmt.Errorf("make build temp path: %s is not a derivation", drvPath)
	}
	h := sha256.New()
	io.WriteString(h, "rewrite:")
	io.WriteString(h, string(drvPath))
	io.WriteString(h, ":name:")
	io.WriteString(h, outputName)
	h2 := nix.NewHash(nix.SHA256, make([]byte, nix.SHA256.Size()))
	name := drvName
	if outputName != zbstore.DefaultDerivationOutputName {
		name += "-" + outputName
	}
	dir := drvPath.Dir()
	digest := storepath.MakeDigest(h, string(dir), h2, name)
	p, err := dir.Object(digest + "-" + name)
	if err != nil {
		ref := zbstore.OutputReference{
			DrvPath:    drvPath,
			OutputName: outputName,
		}
		return "", fmt.Errorf("make build temp path for %v: %v", ref, err)
	}
	return p, nil
}

// rpcLogger is an [io.Writer] that sends its data as [zbstore.LogMethod] RPCs.
// It has no buffering: callers should introduce buffering.
type rpcLogger struct {
	ctx     context.Context
	notif   zbstore.LogNotification
	handler jsonrpc.Handler
}

func newRPCLogger(ctx context.Context, drvPath zbstore.Path, handler jsonrpc.Handler) *rpcLogger {
	return &rpcLogger{
		ctx:     ctx,
		notif:   zbstore.LogNotification{DrvPath: drvPath},
		handler: handler,
	}
}

func (logger *rpcLogger) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	logger.notif.SetPayload(p)
	err = jsonrpc.Notify(logger.ctx, logger.handler, zbstore.LogMethod, &logger.notif)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func formatOutputPaths(m map[string]zbstore.Path) string {
	sb := new(strings.Builder)
	for i, outputName := range xmaps.SortedKeys(m) {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(outputName)
		sb.WriteString(" -> ")
		sb.WriteString(string(m[outputName]))
	}
	return sb.String()
}

func newReplacer[K, V ~string](rewrites iter.Seq2[K, V]) *strings.Replacer {
	var args []string
	for k, v := range rewrites {
		args = append(args, string(k), string(v))
	}
	return strings.NewReplacer(args...)
}

type builderFailure struct {
	err error
}

func (bf builderFailure) Error() string { return bf.err.Error() }
func (bf builderFailure) Unwrap() error { return bf.err }

func isBuilderFailure(err error) bool {
	return errors.As(err, new(builderFailure))
}

type writeCounter int64

func (wc *writeCounter) Write(p []byte) (n int, err error) {
	*wc += writeCounter(len(p))
	return len(p), nil
}

func (wc *writeCounter) WriteString(s string) (n int, err error) {
	*wc += writeCounter(len(s))
	return len(s), nil
}
