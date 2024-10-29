// Copyright 2024 The zb Authors
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
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unique"

	"zb.256lights.llc/pkg/internal/detect"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/storepath"
	"zb.256lights.llc/pkg/internal/system"
	"zb.256lights.llc/pkg/internal/xiter"
	"zb.256lights.llc/pkg/internal/xmaps"
	"zb.256lights.llc/pkg/internal/xslices"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/batchio"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

func (s *Server) realize(ctx context.Context, req *jsonrpc.Request) (_ *jsonrpc.Response, err error) {
	// Validate request.
	var args zbstore.RealizeRequest
	if err := json.Unmarshal(req.Params, &args); err != nil {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
	}
	drvPath, subPath, err := s.dir.ParsePath(string(args.DrvPath))
	if err != nil {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
	}
	if subPath != "" {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, fmt.Errorf("%s is not a store object", args.DrvPath))
	}
	if _, isDrv := drvPath.DerivationName(); !isDrv {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, fmt.Errorf("%s is not a derivation", drvPath))
	}
	log.Infof(ctx, "Requested to build %s", drvPath)

	drvCache, err := s.readDerivationClosure(ctx, []zbstore.Path{drvPath})
	if err != nil {
		return nil, fmt.Errorf("build %s: %v", drvPath, err)
	}

	conn, err := s.db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer s.db.Put(conn)

	wantOutputs := make(sets.Set[zbstore.OutputReference])
	for outputName := range drvCache[drvPath].Outputs {
		wantOutputs.Add(zbstore.OutputReference{
			DrvPath:    drvPath,
			OutputName: outputName,
		})
	}
	b := s.newBuilder(conn, drvCache)
	if err := b.realize(ctx, wantOutputs); err != nil {
		if isBuilderFailure(err) {
			return marshalResponse(b.makeRealizeResponse(drvPath))
		}
		return nil, fmt.Errorf("build %s: %v", drvPath, err)
	}
	return marshalResponse(b.makeRealizeResponse(drvPath))
}

func (s *Server) expand(ctx context.Context, req *jsonrpc.Request) (_ *jsonrpc.Response, err error) {
	// Validate request.
	var args zbstore.ExpandRequest
	if err := json.Unmarshal(req.Params, &args); err != nil {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
	}
	drvPath, subPath, err := s.dir.ParsePath(string(args.DrvPath))
	if err != nil {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
	}
	if subPath != "" {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, fmt.Errorf("%s is not a store object", args.DrvPath))
	}
	if _, isDrv := drvPath.DerivationName(); !isDrv {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, fmt.Errorf("%s is not a derivation", drvPath))
	}
	temporaryDirectory := args.TemporaryDirectory
	if temporaryDirectory == "" {
		// Provide a static per-platform fallback.
		// We don't use [os.TempDir] because that leaks environment variables
		// from the build server.
		if runtime.GOOS == "windows" {
			return nil, jsonrpc.Error(jsonrpc.InvalidParams, fmt.Errorf("missing temporary directory"))
		}
		temporaryDirectory = "/tmp"
	}
	log.Infof(ctx, "Requested to expand %s", drvPath)
	if string(s.dir) != s.realDir {
		return nil, fmt.Errorf("store cannot build derivations (unsandboxed and storage directory does not match store)")
	}

	drvCache, err := s.readDerivationClosure(ctx, []zbstore.Path{drvPath})
	if err != nil {
		return nil, fmt.Errorf("expand %s: %v", drvPath, err)
	}

	conn, err := s.db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer s.db.Put(conn)

	drv := drvCache[drvPath]
	inputs := sets.Collect(drv.InputDerivationOutputs())
	b := s.newBuilder(conn, drvCache)
	if err := b.realize(ctx, inputs); err != nil {
		if isBuilderFailure(err) {
			return marshalResponse(b.makeRealizeResponse(drvPath))
		}
		return nil, fmt.Errorf("expand %s: %v", drvPath, err)
	}

	outPaths, err := tempOutputPaths(drvPath, drv.Outputs)
	if err != nil {
		return nil, fmt.Errorf("expand %s: %v", drvPath, err)
	}
	inputRewrites, err := derivationInputRewrites(drv, b.lookup)
	if err != nil {
		return nil, fmt.Errorf("expand %s: %v", drvPath, err)
	}
	r := newReplacer(xiter.Chain2(
		outputPathRewrites(outPaths),
		maps.All(inputRewrites),
	))
	expandedDrv := expandDerivationPlaceholders(r, drv)
	fillBaseEnv(expandedDrv.Env, drv.Dir, temporaryDirectory)

	return marshalResponse(&zbstore.ExpandResponse{
		Builder: expandedDrv.Builder,
		Args:    expandedDrv.Args,
		Env:     expandedDrv.Env,
	})
}

type builder struct {
	server *Server
	conn   *sqlite.Conn

	derivations  map[zbstore.Path]*zbstore.Derivation
	drvHashes    map[zbstore.Path]nix.Hash
	realizations map[equivalenceClass]cachedRealization
}

type cachedRealization struct {
	// path is the path of the realized store object.
	path zbstore.Path

	// closure is a map of paths transitively referenced by the realization's path
	// to a set of equivalence classes that may have produced that path.
	// The zero [equivalenceClass] indicates that the path was a "source".
	closure map[zbstore.Path]sets.Set[equivalenceClass]
}

func (s *Server) newBuilder(conn *sqlite.Conn, derivations map[zbstore.Path]*zbstore.Derivation) *builder {
	return &builder{
		server:      s,
		conn:        conn,
		derivations: derivations,

		drvHashes:    make(map[zbstore.Path]nix.Hash),
		realizations: make(map[equivalenceClass]cachedRealization),
	}
}

func (b *builder) makeRealizeResponse(drvPath zbstore.Path) *zbstore.RealizeResponse {
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

func (b *builder) realize(ctx context.Context, want sets.Set[zbstore.OutputReference]) error {
	log.Debugf(ctx, "Will realize %v...", want)

	// Multi-output derivations are particularly troublesome for us
	// because if we realize they need to be built
	// after we've already picked a realization for one of the outputs,
	// the build can invalidate the usage of other realizations.
	// (However, this can only occur if more than one output is used in the build.)
	// As long as the derivation is *mostly* deterministic,
	// then we have a good shot of being able to reuse more realizations throughout the rest of the build process
	// because of the early cutoff optimization from content-addressing.
	multiOutputs, err := findMultiOutputDerivationsInBuild(b.derivations, want)
	if err != nil {
		return err
	}
	log.Debugf(ctx, "Found multi-outputs for %v: %v", want, multiOutputs)

	// TODO(soon): Find realizations we can use without requiring all build dependencies.

	log.Debugf(ctx, "Realizing %v...", want)
	drvLocks := make(map[zbstore.Path]func())
	defer func() {
		for _, unlock := range drvLocks {
			unlock()
		}
	}()
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
		drvHash := b.drvHashes[curr.DrvPath]
		if !drvHash.IsZero() {
			log.Debugf(ctx, "Resuming %s", curr.DrvPath)
		} else {
			// First visit to derivation.
			log.Debugf(ctx, "Reached %v", curr)
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
			drvHash, err := hashDrv(drv, b.lookup)
			if err != nil {
				return fmt.Errorf("realize %s: %v", curr, err)
			}
			log.Debugf(ctx, "Hashed %s to %v", curr.DrvPath, drvHash)
			b.drvHashes[curr.DrvPath] = drvHash
		}

		log.Debugf(ctx, "Waiting for build lock on %s...", curr.DrvPath)
		unlock, err := b.server.building.lock(ctx, curr.DrvPath)
		if err != nil {
			return err
		}
		drvLocks[curr.DrvPath] = unlock
		log.Debugf(ctx, "Acquired build lock on %s", curr.DrvPath)
		outputs := sets.New(curr.OutputName)
		outputs.AddSeq(multiOutputs[curr.DrvPath].All())
		if err := b.do(ctx, curr.DrvPath, outputs); err != nil {
			return err
		}
		drvLocks[curr.DrvPath]()
		delete(drvLocks, curr.DrvPath)
	}

	return nil
}

var (
	errRealizationNotFound  = errors.New("no suitable realizations exist")
	errMultipleRealizations = errors.New("multiple valid realizations")
)

// fetchRealization finds a realization to use for a derivation output
// from the store database
// that is compatible with existing realizations in the builder.
// fetchRealization returns an error that unwraps to [errRealizationNotFound]
// if no such realization could be found.
// If fetchRealization does not return an error,
// then b.realizations will have a value for eqClass.
//
// If mustExist is true, then the realization must be present in the store
// to be considered.
// Otherwise, fetchRealization will return a set of keys added to b.realizations
// that name paths not in the store.
//
// fetchRealization may add realizations for equivalence classes beyond the given one
// because selecting a realization may imply selecting realizations from its closure.
// fetchRealization will only add realizations to b.realizations
// if it does not return an error.
func (b *builder) fetchRealization(ctx context.Context, eqClass equivalenceClass, mustExist bool) (absentRealizations sets.Set[equivalenceClass], err error) {
	if _, exists := b.realizations[eqClass]; exists {
		return nil, nil
	}

	defer func() {
		// Don't add absent realizations to the realization set if we return an error.
		if err != nil {
			for eqClass := range absentRealizations.All() {
				delete(b.realizations, eqClass)
			}
			absentRealizations = nil
		}
	}()
	defer sqlitex.Save(b.conn)(&err)

	log.Debugf(ctx, "Searching for realizations for %v...", eqClass)
	presentInStore, absentFromStore, err := findPossibleRealizations(ctx, b.conn, eqClass)
	if err != nil {
		return nil, err
	}

	var r cachedRealization
	present := false
	r.path, r.closure, err = b.pickRealizationFromSet(ctx, eqClass, presentInStore)
	switch {
	case err == nil:
		present = true
	case errors.Is(err, errMultipleRealizations):
		return nil, fmt.Errorf("pick compatible realization for %v: %w", eqClass, errRealizationNotFound)
	case errors.Is(err, errRealizationNotFound):
		if mustExist {
			return nil, err
		}
		r.path, r.closure, err = b.pickRealizationFromSet(ctx, eqClass, absentFromStore)
		if errors.Is(err, errMultipleRealizations) {
			return nil, fmt.Errorf("pick compatible realization for %v: %w", eqClass, errRealizationNotFound)
		}
		if err != nil {
			return nil, err
		}
	default:
		return nil, err
	}

	// Now that we selected our realization, fill out the closures.
	log.Debugf(ctx, "Using sole viable candidate %s for %v", r.path, eqClass)
	b.realizations[eqClass] = r
	if !present {
		absentRealizations = sets.New(eqClass)
	}
	for refPath, eqClasses := range r.closure {
		refPathExists := true
		if !present {
			var err error
			refPathExists, err = objectExists(b.conn, refPath)
			if err != nil {
				return absentRealizations, fmt.Errorf("pick compatible realization for %v: %v", eqClass, err)
			}
		}

		for eqClass := range eqClasses.All() {
			if eqClass.isZero() {
				continue
			}
			if _, exists := b.realizations[eqClass]; exists {
				continue
			}
			pe := pathAndEquivalenceClass{
				path:             refPath,
				equivalenceClass: eqClass,
			}
			closureRealization := cachedRealization{
				path:    refPath,
				closure: make(map[zbstore.Path]sets.Set[equivalenceClass]),
			}
			err = closurePaths(b.conn, pe, func(pe pathAndEquivalenceClass) bool {
				addToMultiMap(closureRealization.closure, pe.path, pe.equivalenceClass)
				return true
			})
			if err != nil {
				return absentRealizations, fmt.Errorf("pick compatible realization for %v: %v", eqClass, err)
			}
			b.realizations[eqClass] = closureRealization
			if !refPathExists {
				absentRealizations.Add(eqClass)
			}
		}
	}

	return absentRealizations, nil
}

// fetchRealizationSet finds a set of realizations to use for the given set of derivation outputs
// from the store database
// that is compatible with existing realizations in the builder
// and with elements in the set.
// fetchRealizationSet returns an error that unwraps to [errRealizationNotFound]
// if no such set of realizations could be found.
// If fetchRealizationSet does not return an error,
// then b.realizations will have a value for all elements of eqClasses.
// Only realizations in the store will be considered.
//
// fetchRealizationSet may add realizations for equivalence classes beyond the given one
// because selecting a realization may imply selecting realizations from its closure.
// fetchRealizationSet will only add realizations to b.realizations
// if it does not return an error.
func (b *builder) fetchRealizationSet(ctx context.Context, eqClasses sets.Set[equivalenceClass]) (err error) {
	defer sqlitex.Save(b.conn)(&err)

	oldRealizations := maps.Clone(b.realizations)
	defer func() {
		if err != nil {
			b.realizations = oldRealizations
		}
	}()

	for eqClass := range eqClasses.All() {
		if _, err := b.fetchRealization(ctx, eqClass, true); err != nil {
			return err
		}
	}
	return nil
}

func (b *builder) pickRealizationFromSet(ctx context.Context, eqClass equivalenceClass, existing sets.Set[zbstore.Path]) (zbstore.Path, map[zbstore.Path]sets.Set[equivalenceClass], error) {
	var selectedPath zbstore.Path
	closure := make(map[zbstore.Path]sets.Set[equivalenceClass])
	remaining := existing.Clone()
	for outputPath := range existing.All() {
		log.Debugf(ctx, "Checking whether %s can be used as %v...", outputPath, eqClass)
		remaining.Delete(outputPath)

		pe := pathAndEquivalenceClass{
			path:             outputPath,
			equivalenceClass: eqClass,
		}
		clear(closure)
		canUse := true
		err := closurePaths(b.conn, pe, func(ref pathAndEquivalenceClass) bool {
			canUse = b.isCompatible(ref)
			if canUse {
				addToMultiMap(closure, ref.path, ref.equivalenceClass)
			} else {
				log.Debugf(ctx, "Cannot use %s as %v: depends on %s (need %s)",
					outputPath, eqClass, ref.path, b.realizations[ref.equivalenceClass].path)
			}
			return canUse
		})
		if err != nil {
			return "", nil, fmt.Errorf("pick compatible realization for %v: %v", eqClass, err)
		}
		if canUse {
			log.Debugf(ctx, "Found %s as a candidate for %v", outputPath, eqClass)
			selectedPath = outputPath
			break
		}
	}

	if selectedPath == "" {
		log.Debugf(ctx, "No suitable realizations exist for %v", eqClass)
		return "", nil, fmt.Errorf("pick compatible realization for %v: %w", eqClass, errRealizationNotFound)
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
			canUse = b.isCompatible(ref)
			return canUse
		})
		if err != nil {
			return "", nil, fmt.Errorf("pick compatible realization for %v: %v", eqClass, err)
		}
		if canUse {
			// Multiple realizations are compatible.
			// Return none of them.
			log.Debugf(ctx, "Both %s and %s are viable candidates for %v. Returning nothing.", outputPath, selectedPath, eqClass)
			return "", nil, fmt.Errorf("pick compatible realization for %v: %w", eqClass, errMultipleRealizations)
		}
	}

	return selectedPath, closure, nil
}

// isCompatible reports whether the given path can be used
// for the given (potentially zero) equivalence class
// with respect to the rest of the builder's realizations.
func (b *builder) isCompatible(pe pathAndEquivalenceClass) bool {
	if pe.equivalenceClass.isZero() {
		// Sources can't conflict.
		return true
	}
	used, hasExisting := b.realizations[pe.equivalenceClass]
	return !hasExisting || pe.path == used.path
}

// do ensures that a single derivation has realizations for the given set of outputs,
// either by reusing existing realizations or by building it.
// b.drvHashes must have a non-zero value for drvPath before calling do
// (which implies the caller realized all of the derivation's inputs)
// or else do returns an error.
func (b *builder) do(ctx context.Context, drvPath zbstore.Path, outputNames sets.Set[string]) (err error) {
	drv := b.derivations[drvPath]
	if drv == nil {
		return fmt.Errorf("build %s: unknown derivation", drvPath)
	}
	drvHash := b.drvHashes[drvPath]
	if drvHash.IsZero() {
		return fmt.Errorf("build %s: missing hash", drvPath)
	}
	for outputName := range outputNames.All() {
		if drv.Outputs[outputName] == nil {
			ref := zbstore.OutputReference{
				DrvPath:    drvPath,
				OutputName: outputName,
			}
			return fmt.Errorf("build %v: no such output", ref)
		}
	}

	// Search for existing realizations first.
	wantEqClasses := sets.Collect(func(yield func(equivalenceClass) bool) {
		for outputName := range outputNames.All() {
			if !yield(newEquivalenceClass(drvHash, outputName)) {
				return
			}
		}
	})
	if err := b.fetchRealizationSet(ctx, wantEqClasses); err == nil {
		return nil
	} else if !errors.Is(err, errRealizationNotFound) {
		return fmt.Errorf("build %s: %v", drvPath, err)
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

	// Verify that builder can run.
	if !canBuildLocally(drv) {
		return fmt.Errorf("build %s: a %s system is required, but host is a %v system",
			drvPath, drv.System, system.Current())
	}
	for _, input := range drv.InputSources.All() {
		log.Debugf(ctx, "Waiting for lock on %s (input to %s)...", input, drvPath)
		unlockInput, err := b.server.writing.lock(ctx, input)
		if err != nil {
			return fmt.Errorf("build %s: wait for %s: %w", drvPath, input, err)
		}
		_, err = os.Lstat(b.server.realPath(input))
		unlockInput()
		log.Debugf(ctx, "%s exists=%t (input to %s)", input, err == nil, drvPath)
		if err != nil {
			// TODO(someday): Import from substituter if not found.
			return fmt.Errorf("build %s: input %s not present (%v)", drvPath, input, err)
		}
	}
	buildUser, err := b.server.users.acquire(ctx)
	if err != nil {
		return fmt.Errorf("build %s: %v", drvPath, err)
	}
	if buildUser != nil {
		log.Debugf(ctx, "Using build user %v", buildUser)
	}
	defer b.server.users.release(buildUser)

	// Arrange for builder to run.
	var runner runnerFunc
	switch {
	case drv.System == builtinSystem:
		log.Debugf(ctx, "Runner for %s is builtin", drvPath)
		runner = runBuiltin
	case b.server.sandbox:
		log.Debugf(ctx, "Runner for %s is sandbox", drvPath)
		runner = runSandboxed
	default:
		log.Debugf(ctx, "Runner for %s is unsandboxed", drvPath)
		runner = runSubprocess
	}
	tempOutPaths, err := b.runBuilder(ctx, drvPath, buildUser, runner)
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

	log.Infof(ctx, "Built %s: %s", drvPath, formatOutputPaths(maps.Collect(func(yield func(string, zbstore.Path) bool) {
		for outputName, r := range outputs {
			if !yield(outputName, r.path) {
				return
			}
		}
	})))
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
		for refPath, refClasses := range out.closure {
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

// A runnerFunc is a function that can execute a builder.
//
// A runnerFunc should:
//   - Run the builder program
//     with the working directory
//     (and TMPDIR or equivalent environment variables)
//     set to invocation.buildDir.
//     Mapping the location is acceptable,
//     as long as files are physically stored in invocation.buildDir.
//   - Return a [builderFailure] if the builder did not run succesfully
//     (e.g. a user build failure).
//     Any other type of error is treated as an internal backend failure.
//   - Create filesystem objects in invocation.realStoreDir
//     for each output path in invocation.outputPaths.
type runnerFunc func(ctx context.Context, invocation *builderInvocation) error

type builderInvocation struct {
	// derivation is the derivation whose builder should be executed.
	// The caller is responsible for expanding any placeholders
	// in the derivation's fields.
	derivation *zbstore.Derivation
	// derivationPath is the path of the derivation whose builder is being executed.
	derivationPath zbstore.Path
	// outputPaths is the map of output name to path this builder is expected to produce.
	outputPaths map[string]zbstore.Path

	// realStoreDir is the directory where the store is located in the local filesystem.
	realStoreDir string
	// buildDir is the temporary directory created for this build.
	buildDir string
	// logWriter is where all builder output should be sent.
	logWriter io.Writer
	// lookup returns the store path for the given derivation output.
	// lookup should return paths for the inputs to the derivation the runner is building
	// at least.
	lookup func(ref zbstore.OutputReference) (zbstore.Path, bool)
	// closure calls yield for each store object
	// in the transitive closure of the store object at the given path.
	closure func(path zbstore.Path, yield func(zbstore.Path) bool) error
	// user is the Unix user to run the build as.
	// If nil, then the current process's user should be used.
	user *BuildUser
	// sandboxPaths is a map of paths inside the sandbox
	// to paths on the host machine.
	// For sandboxed runners, these paths will be made available inside the sandbox.
	sandboxPaths map[string]string
}

func (b *builder) runBuilder(ctx context.Context, drvPath zbstore.Path, buildUser *BuildUser, f runnerFunc) (map[string]zbstore.Path, error) {
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
	inputRewrites, err := derivationInputRewrites(drv, b.lookup)
	if err != nil {
		return nil, fmt.Errorf("build %s: %v", drvPath, err)
	}

	buildDir, err := os.MkdirTemp(b.server.buildDir, "zb-build-"+drvName+"*")
	if err != nil {
		return nil, fmt.Errorf("build %s: %v", drvPath, err)
	}
	defer func() {
		if err := os.RemoveAll(buildDir); err != nil {
			log.Warnf(ctx, "Failed to clean up %s: %v", buildDir, err)
		}
	}()
	if buildUser != nil {
		if err := os.Chown(buildDir, buildUser.UID, -1); err != nil {
			return nil, fmt.Errorf("build %s: %v", drvPath, err)
		}
	}

	r := newReplacer(xiter.Chain2(
		outputPathRewrites(outPaths),
		maps.All(inputRewrites),
	))
	expandedDrv := expandDerivationPlaceholders(r, drv)

	peerLogger := newRPCLogger(ctx, drvPath, peer(ctx))
	bufferedPeerLogger := batchio.NewWriter(peerLogger, 8192, 250*time.Millisecond)
	defer bufferedPeerLogger.Flush()

	log.Debugf(ctx, "Starting builder for %s...", drvPath)
	err = f(ctx, &builderInvocation{
		derivation:     expandedDrv,
		derivationPath: drvPath,
		outputPaths:    outPaths,

		realStoreDir: b.server.realDir,
		buildDir:     buildDir,
		logWriter:    bufferedPeerLogger,
		user:         buildUser,
		sandboxPaths: b.server.sandboxPaths,

		lookup: b.lookup,
		closure: func(path zbstore.Path, yield func(zbstore.Path) bool) error {
			pe := pathAndEquivalenceClass{path: path}
			return closurePaths(b.conn, pe, func(pe pathAndEquivalenceClass) bool {
				return yield(pe.path)
			})
		},
	})
	bufferedPeerLogger.Flush()
	if err != nil {
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
		return nil, fmt.Errorf("build %s: %w", drvPath, err)
	}

	log.Debugf(ctx, "Builder for %s has finished successfully", drvPath)
	return outPaths, nil
}

// runSubprocess runs a builder by running a subprocess.
// It satisfies the [runnerFunc] signature.
func runSubprocess(ctx context.Context, invocation *builderInvocation) error {
	if string(invocation.derivation.Dir) != invocation.realStoreDir {
		return fmt.Errorf("store is unsandboxed and storage directory does not match store (%s)", invocation.derivation.Dir)
	}

	c := exec.CommandContext(ctx, invocation.derivation.Builder, invocation.derivation.Args...)
	setCancelFunc(c)
	env := maps.Clone(invocation.derivation.Env)
	fillBaseEnv(env, invocation.derivation.Dir, invocation.buildDir)
	for k, v := range xmaps.Sorted(env) {
		c.Env = append(c.Env, k+"="+v)
	}
	c.Dir = invocation.buildDir
	c.Stdout = invocation.logWriter
	c.Stderr = invocation.logWriter
	c.SysProcAttr = sysProcAttrForUser(invocation.user)

	if err := c.Run(); err != nil {
		return builderFailure{err}
	}

	return nil
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
func derivationInputRewrites(drv *zbstore.Derivation, realization func(ref zbstore.OutputReference) (zbstore.Path, bool)) (map[string]zbstore.Path, error) {
	// TODO(maybe): Also rewrite transitive derivation hashes?
	result := make(map[string]zbstore.Path)
	for ref := range drv.InputDerivationOutputs() {
		placeholder := zbstore.UnknownCAOutputPlaceholder(ref)
		rpath, ok := realization(ref)
		if !ok {
			return nil, fmt.Errorf("compute input rewrites: missing realization for %v", ref)
		}
		result[placeholder] = rpath
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

		tp, err := tempPath(zbstore.OutputReference{
			DrvPath:    drvPath,
			OutputName: outName,
		})
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

	var info *zbstore.NARInfo
	var err error
	if ca, ok := outputType.FixedCA(); ok {
		if unlockBuildPath == nil {
			return nil, fmt.Errorf("post-process %v: write lock was not held", output)
		}
		defer unlockBuildPath()

		info, err = b.postprocessFixedOutput(ctx, buildPath, ca)
	} else {
		if unlockBuildPath != nil {
			unlockBuildPath()
			return nil, fmt.Errorf("post-process %v: unexpected write lock", output)
		}
		// outputType has presumably been validated with [validateOutputs].
		info, err = b.postprocessFloatingOutput(ctx, buildPath, inputs)
	}
	if info != nil {
		info.Deriver = output.DrvPath
	}
	return info, err
}

// postprocessFixedOutput computes the NAR hash of the given store path
// and verifies that it matches the content address.
func (b *builder) postprocessFixedOutput(ctx context.Context, outputPath zbstore.Path, ca zbstore.ContentAddress) (info *zbstore.NARInfo, err error) {
	log.Debugf(ctx, "Verifying fixed output %s...", outputPath)

	realOutputPath := b.server.realPath(outputPath)
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
		return nil, err
	}

	info = &zbstore.NARInfo{
		StorePath:   outputPath,
		Compression: nix.NoCompression,
		NARHash:     h.SumHash(),
		NARSize:     int64(*wc),
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
		err = fmt.Errorf("post-process %v: %v", outputPath, err)
	}

	makePublicReadOnly(ctx, realOutputPath)

	return info, nil
}

func (b *builder) postprocessFloatingOutput(ctx context.Context, buildPath zbstore.Path, inputs *sets.Sorted[zbstore.Path]) (*zbstore.NARInfo, error) {
	log.Debugf(ctx, "Processing floating output %s...", buildPath)
	realBuildPath := b.server.realPath(buildPath)
	scan, err := scanFloatingOutput(ctx, realBuildPath, buildPath.Digest(), inputs)
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

	makePublicReadOnly(ctx, realFinalPath)

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
func scanFloatingOutput(ctx context.Context, path string, digest string, closure *sets.Sorted[zbstore.Path]) (*outputScanResults, error) {
	log.Debugf(ctx, "Scanning for references in %s. Possible: %s", path, closure)
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
	log.Debugf(ctx, "Found references in %s (self=%t): %s", path, refs.Self, &refs.Others)

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
// The outputs must exist in the store.
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
		eqClass := newEquivalenceClass(drvHash, outputName)
		closure := make(map[zbstore.Path]sets.Set[equivalenceClass])
		pe := pathAndEquivalenceClass{path: output.path, equivalenceClass: eqClass}
		err := closurePaths(b.conn, pe, func(pe pathAndEquivalenceClass) bool {
			addToMultiMap(closure, pe.path, pe.equivalenceClass)
			return true
		})
		if err != nil {
			return err
		}
		b.realizations[eqClass] = cachedRealization{
			path:    output.path,
			closure: closure,
		}
	}
	return nil
}

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
func tempPath(ref zbstore.OutputReference) (zbstore.Path, error) {
	drvName, ok := ref.DrvPath.DerivationName()
	if !ok {
		return "", fmt.Errorf("make build temp path: %s is not a derivation", ref.DrvPath)
	}
	h := sha256.New()
	io.WriteString(h, "rewrite:")
	io.WriteString(h, string(ref.DrvPath))
	io.WriteString(h, ":name:")
	io.WriteString(h, ref.OutputName)
	h2 := nix.NewHash(nix.SHA256, make([]byte, nix.SHA256.Size()))
	name := drvName
	if ref.OutputName != zbstore.DefaultDerivationOutputName {
		name += "-" + ref.OutputName
	}
	dir := ref.DrvPath.Dir()
	digest := storepath.MakeDigest(h, string(dir), h2, name)
	p, err := dir.Object(digest + "-" + name)
	if err != nil {
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
