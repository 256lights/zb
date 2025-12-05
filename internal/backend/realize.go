// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package backend

import (
	"cmp"
	"context"
	"crypto/sha256"
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

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/google/uuid"
	"zb.256lights.llc/pkg/bytebuffer"
	"zb.256lights.llc/pkg/internal/detect"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/storepath"
	"zb.256lights.llc/pkg/internal/system"
	"zb.256lights.llc/pkg/internal/xio"
	"zb.256lights.llc/pkg/internal/xiter"
	"zb.256lights.llc/pkg/internal/xmaps"
	"zb.256lights.llc/pkg/internal/xslices"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
	"zombiezen.com/go/xcontext"
)

// Special environment variable names.
const (
	buildSystemDepsVar = "__buildSystemDeps"
	networkVar         = "__network"
)

func (s *Server) realize(ctx context.Context, req *jsonrpc.Request) (_ *jsonrpc.Response, err error) {
	// Validate request.
	var args zbstorerpc.RealizeRequest
	if err := jsonv2.Unmarshal(req.Params, &args); err != nil {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
	}
	if len(args.DrvPaths) == 0 {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, fmt.Errorf("no derivation paths given"))
	}
	var drvPaths []zbstore.Path
	for _, arg := range args.DrvPaths {
		drvPath, subPath, err := s.dir.ParsePath(string(arg))
		if err != nil {
			return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
		}
		if subPath != "" {
			return nil, jsonrpc.Error(jsonrpc.InvalidParams, fmt.Errorf("%s is not a store object", arg))
		}
		if _, isDrv := drvPath.DerivationName(); !isDrv {
			return nil, jsonrpc.Error(jsonrpc.InvalidParams, fmt.Errorf("%s is not a derivation", drvPath))
		}
		drvPaths = append(drvPaths, drvPath)
	}
	buildID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	drvPathList := joinStrings(drvPaths, ", ")
	log.Infof(ctx, "New build %v: %s", buildID, drvPathList)

	drvCache, err := s.readDerivationClosure(ctx, drvPaths)
	if err != nil {
		return nil, fmt.Errorf("build %s: %v", drvPathList, err)
	}

	conn, err := s.db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer s.db.Put(conn)

	buildCtx, cancelBuild, err := s.registerBuildID(ctx, conn, buildID)
	if err != nil {
		return nil, fmt.Errorf("build %s: %v", drvPathList, err)
	}

	s.background.Add(1)
	go func() {
		defer func() {
			cancelBuild()
			s.background.Done()
		}()

		wantOutputs := make(sets.Set[zbstore.OutputReference])
		for _, drvPath := range drvPaths {
			for outputName := range drvCache[drvPath].Outputs {
				wantOutputs.Add(zbstore.OutputReference{
					DrvPath:    drvPath,
					OutputName: outputName,
				})
			}
		}
		b := s.newBuilder(buildID, drvCache, args.Reuse)
		realizeError := b.realize(buildCtx, wantOutputs, args.KeepFailed)

		recordCtx, cancel := xcontext.KeepAlive(buildCtx, 30*time.Second)
		defer cancel()
		conn, err := s.db.Get(recordCtx)
		if err != nil {
			log.Errorf(recordCtx, "Unable to record end of build %s: %v. Original error: %v", buildID, err, realizeError)
			return
		}
		defer s.db.Put(conn)
		if err := recordBuildEnd(conn, buildID, realizeError); err != nil {
			log.Errorf(recordCtx, "Unable to record end of build %s: %v. Original error: %v", buildID, err, realizeError)
		}
	}()

	return marshalResponse(&zbstorerpc.RealizeResponse{
		BuildID: buildID.String(),
	})
}

func (s *Server) expand(ctx context.Context, req *jsonrpc.Request) (_ *jsonrpc.Response, err error) {
	// Validate request.
	var args zbstorerpc.ExpandRequest
	if err := jsonv2.Unmarshal(req.Params, &args); err != nil {
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
	buildID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("expand %s: %v", drvPath, err)
	}

	conn, err := s.db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer s.db.Put(conn)

	buildCtx, endBuild, err := s.registerBuildID(ctx, conn, buildID)
	if err != nil {
		return nil, fmt.Errorf("expand %s: %v", drvPath, err)
	}

	s.background.Add(1)
	go func() {
		defer func() {
			endBuild()
			s.background.Done()
		}()

		drv := drvCache[drvPath]
		inputs := sets.Collect(drv.InputDerivationOutputs())

		b := s.newBuilder(buildID, drvCache, args.Reuse)
		realizeError := b.realize(buildCtx, inputs, false)

		recordCtx, cancel := xcontext.KeepAlive(buildCtx, 30*time.Second)
		defer cancel()
		conn, err := s.db.Get(recordCtx)
		if err != nil {
			log.Errorf(recordCtx, "Unable to record end of build %s: %v. Original error: %v", buildID, err, realizeError)
			return
		}
		defer s.db.Put(conn)

		if realizeError != nil {
			if err := recordBuildEnd(conn, buildID, realizeError); err != nil {
				log.Errorf(recordCtx, "Unable to record end of build %s: %v. Original error: %v", buildID, err, realizeError)
			}
			return
		}

		expandedDrv, expandError := b.expand(drvPath, drv, temporaryDirectory)
		if expandError != nil {
			// Errors at this stage indicate defects in zb.
			log.Errorf(recordCtx, "Expand %s: %v", drvPath, expandError)
		}

		err = func() (err error) {
			endTx, err := sqlitex.ImmediateTransaction(conn)
			if err != nil {
				return err
			}
			defer endTx(&err)
			if err := recordBuildEnd(conn, buildID, expandError); err != nil {
				return err
			}
			if expandError == nil {
				err := recordExpandResult(conn, buildID, &zbstorerpc.ExpandResult{
					Builder: expandedDrv.Builder,
					Args:    expandedDrv.Args,
					Env:     expandedDrv.Env,
				})
				if err != nil {
					return err
				}
			}
			return nil
		}()
		if err != nil {
			log.Errorf(recordCtx, "Unable to record end of build %s: %v", buildID, err)
			return
		}
	}()

	return marshalResponse(&zbstorerpc.ExpandResponse{
		BuildID: buildID.String(),
	})
}

func (s *Server) registerBuildID(parent context.Context, conn *sqlite.Conn, buildID uuid.UUID) (_ context.Context, cleanup func(), err error) {
	if err := recordBuildStart(conn, buildID); err != nil {
		return nil, nil, err
	}
	ctx := s.buildContext(context.WithoutCancel(parent), buildID.String())
	ctx, cancel := context.WithCancel(ctx)
	s.activeBuildsMu.Lock()
	draining := s.draining
	if !draining {
		s.activeBuilds[buildID] = cancel
	}
	s.activeBuildsMu.Unlock()

	if draining {
		// If we're draining, don't worry about updating the status.
		// An unterminated build that isn't active will show up as unknown.
		cancel()
		return nil, nil, errors.New("server shutting down; not starting new builds")
	}
	return ctx, func() {
		s.activeBuildsMu.Lock()
		delete(s.activeBuilds, buildID)
		s.activeBuildsMu.Unlock()
		cancel()
	}, nil
}

type builder struct {
	id     uuid.UUID
	server *Server

	reusePolicy  *zbstorerpc.ReusePolicy
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

func (s *Server) newBuilder(id uuid.UUID, derivations map[zbstore.Path]*zbstore.Derivation, reuse *zbstorerpc.ReusePolicy) *builder {
	if reuse == nil {
		reuse = new(zbstorerpc.ReusePolicy)
	}
	return &builder{
		server:      s,
		id:          id,
		derivations: derivations,

		reusePolicy:  reuse,
		drvHashes:    make(map[zbstore.Path]nix.Hash),
		realizations: make(map[equivalenceClass]cachedRealization),
	}
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

// allRealized reports whether all the given references have realizations.
func (b *builder) allRealized(refs iter.Seq[zbstore.OutputReference]) bool {
	for ref := range refs {
		if _, ok := b.lookup(ref); !ok {
			return false
		}
	}
	return true
}

var errUnfinishedRealization = errors.New("realization did not complete")

func (b *builder) realize(ctx context.Context, want sets.Set[zbstore.OutputReference], keepFailed bool) error {
	log.Debugf(ctx, "Will realize %v...", want)

	graph, err := analyze(b.derivations, want)
	if err != nil {
		return err
	}

	// TODO(soon): Find realizations we can use without requiring all build dependencies.

	log.Debugf(ctx, "Realizing %v...", want)
	drvLocks := make(map[zbstore.Path]func())
	defer func() {
		for _, unlock := range drvLocks {
			unlock()
		}
	}()
	stack := slices.AppendSeq(make([]zbstore.Path, 0, graph.roots.Len()), graph.roots.All())
	for len(stack) > 0 {
		curr := xslices.Last(stack)
		stack = xslices.Pop(stack, 1)

		drv := b.derivations[curr]
		if drv == nil {
			return fmt.Errorf("realize %v: unknown derivation", curr)
		}
		log.Debugf(ctx, "Reached %v", curr)
		drvHash, err := drv.SHA256RealizationHash(b.lookup)
		if err != nil {
			return fmt.Errorf("realize %s: %v", curr, err)
		}
		log.Debugf(ctx, "Hashed %s to %v", curr, drvHash)
		b.drvHashes[curr] = drvHash

		log.Debugf(ctx, "Waiting for build lock on %s...", curr)
		unlock, err := b.server.building.lock(ctx, curr)
		if err != nil {
			return err
		}
		drvLocks[curr] = unlock
		log.Debugf(ctx, "Acquired build lock on %s", curr)
		graphNode := graph.nodes[curr]
		if err := b.do(ctx, curr, graphNode.usedOutputs, keepFailed); err != nil {
			// b.do already records the build failure,
			// so we don't need to report the same error at the build level.
			if !isBuilderFailure(err) {
				log.Errorf(ctx, "%v", err)
			}
			return errUnfinishedRealization
		}
		drvLocks[curr]()
		delete(drvLocks, curr)

		// Queue up new work.
		for possible := range graphNode.dependents {
			if b.allRealized(b.derivations[possible].InputDerivationOutputs()) {
				stack = append(stack, possible)
			}
		}
	}

	return nil
}

var (
	errRealizationNotFound  = errors.New("no suitable realizations exist")
	errMultipleRealizations = errors.New("multiple valid realizations")
)

func (b *builder) expand(drvPath zbstore.Path, drv *zbstore.Derivation, temporaryDirectory string) (*zbstore.Derivation, error) {
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
	expandedDrv := drv.ReplaceStrings(r)
	fillBaseEnv(expandedDrv.Env, drv.Dir, temporaryDirectory, b.server.coresPerBuild)
	return expandedDrv, nil
}

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
func (b *builder) fetchRealization(ctx context.Context, conn *sqlite.Conn, eqClass equivalenceClass, mustExist bool) (absentRealizations sets.Set[equivalenceClass], err error) {
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
	defer sqlitex.Save(conn)(&err)

	log.Debugf(ctx, "Searching for realizations for %v...", eqClass)
	presentInStore, absentFromStore, err := findPossibleRealizations(ctx, conn, eqClass, b.reusePolicy)
	if err != nil {
		return nil, err
	}

	var r cachedRealization
	present := false
	r.path, r.closure, err = b.pickRealizationFromSet(ctx, conn, eqClass, presentInStore)
	switch {
	case err == nil:
		present = true
	case errors.Is(err, errMultipleRealizations):
		return nil, fmt.Errorf("pick compatible realization for %v: %w", eqClass, errRealizationNotFound)
	case errors.Is(err, errRealizationNotFound):
		if mustExist {
			return nil, err
		}
		r.path, r.closure, err = b.pickRealizationFromSet(ctx, conn, eqClass, absentFromStore)
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
			refPathExists, err = objectExists(conn, refPath)
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
			err = closurePaths(conn, pe, func(pe pathAndEquivalenceClass) bool {
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
func (b *builder) fetchRealizationSet(ctx context.Context, conn *sqlite.Conn, eqClasses sets.Set[equivalenceClass]) (err error) {
	defer sqlitex.Save(conn)(&err)

	oldRealizations := maps.Clone(b.realizations)
	defer func() {
		if err != nil {
			b.realizations = oldRealizations
		}
	}()

	for eqClass := range eqClasses.All() {
		if _, err := b.fetchRealization(ctx, conn, eqClass, true); err != nil {
			return err
		}
	}
	return nil
}

// pickRealizationFromSet finds the only realization in the existing set
// that has closures compatible with the rest of the builder's realizations.
// pickRealizationFromSet will return an error that unwraps to [errRealizationNotFound]
// if there is no such realization,
// or an error that unwraps to [errMultipleRealizations] if there are multiple such realizations.
func (b *builder) pickRealizationFromSet(ctx context.Context, conn *sqlite.Conn, eqClass equivalenceClass, existing sets.Set[zbstore.Path]) (selectedPath zbstore.Path, closure map[zbstore.Path]sets.Set[equivalenceClass], err error) {
	closure = make(map[zbstore.Path]sets.Set[equivalenceClass])
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
		err := closurePaths(conn, pe, func(ref pathAndEquivalenceClass) bool {
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

	// In the case where there are multiple valid candidates
	// that match the client's reuse policy,
	// we don't use any of them and rebuild.
	for outputPath := range remaining.All() {
		pe := pathAndEquivalenceClass{
			path:             outputPath,
			equivalenceClass: eqClass,
		}
		canUse := true
		err := closurePaths(conn, pe, func(ref pathAndEquivalenceClass) bool {
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
func (b *builder) do(ctx context.Context, drvPath zbstore.Path, outputNames sets.Set[unique.Handle[string]], keepFailed bool) (err error) {
	startTime := time.Now()
	drv := b.derivations[drvPath]
	if drv == nil {
		return fmt.Errorf("build %s: unknown derivation", drvPath)
	}
	drvHash := b.drvHashes[drvPath]
	if drvHash.IsZero() {
		return fmt.Errorf("build %s: missing hash", drvPath)
	}
	for outputName := range outputNames.All() {
		if drv.Outputs[outputName.Value()] == nil {
			ref := zbstore.OutputReference{
				DrvPath:    drvPath,
				OutputName: outputName.Value(),
			}
			return fmt.Errorf("build %v: no such output", ref)
		}
	}

	conn, err := b.server.db.Get(ctx)
	if err != nil {
		return err
	}
	defer b.server.db.Put(conn)

	var buildResultID int64
	hasExisting := false
	err = func() (err error) {
		endFn, err := sqlitex.ImmediateTransaction(conn)
		if err != nil {
			return err
		}
		defer endFn(&err)

		buildResultID, err = insertBuildResult(conn, b.id, drvPath, drvHash, startTime)
		if err != nil {
			return fmt.Errorf("build %s: %v", drvPath, err)
		}

		// Search for existing realizations first.
		wantEqClasses := sets.Collect(func(yield func(equivalenceClass) bool) {
			for outputName := range outputNames.All() {
				if !yield(newEquivalenceClass(drvHash, outputName.Value())) {
					return
				}
			}
		})
		reuseError := b.fetchRealizationSet(ctx, conn, wantEqClasses)

		// Regardless of whether the realization search succeeded or not,
		// we want to set outputs for this build results during this transaction.
		// If the search succeeded, the outputs will have the found paths.
		// Otherwise, we set a default of nulls for all requested outputs.
		err = setBuildResultOutputs(conn, buildResultID, func(yield func(string, zbstore.Path) bool) {
			for outputName := range outputNames.All() {
				eqClass := newEquivalenceClass(drvHash, outputName.Value())
				var path zbstore.Path
				if reuseError == nil {
					path = b.realizations[eqClass].path
				}
				if !yield(outputName.Value(), path) {
					return
				}
			}
		})
		if err != nil {
			// If setting the outputs failed, then it's probably an I/O error of some sort.
			// Finalizing the build result probably won't succeed,
			// so we just return the error and bail early.
			return err
		}

		if !errors.Is(reuseError, errRealizationNotFound) {
			// If there was a realization or the search had an abnormal failure,
			// we can finalize the result inside this transaction.
			hasExisting = reuseError == nil
			err := finalizeBuildResult(ctx, conn, b.server.logDir, &buildFinalResults{
				buildID: b.id,
				drvPath: drvPath,
				id:      buildResultID,
				endTime: time.Now(),
				error:   reuseError,
			})
			if err != nil {
				log.Warnf(ctx, "For build %s: %v", drvPath, err)
			}
			return reuseError
		}

		return nil
	}()
	if err != nil {
		return fmt.Errorf("build %s: %v", drvPath, err)
	}
	if hasExisting {
		return nil
	}
	defer func() {
		endFn, txError := sqlitex.ImmediateTransaction(conn)
		if txError != nil {
			log.Warnf(ctx, "For build %s: %v", drvPath, txError)
			return
		}
		finalizeError := finalizeBuildResult(ctx, conn, b.server.logDir, &buildFinalResults{
			buildID: b.id,
			drvPath: drvPath,
			id:      buildResultID,
			endTime: time.Now(),
			error:   err,
		})
		endFn(&finalizeError)
		if finalizeError != nil {
			log.Warnf(ctx, "For build %s: %v", drvPath, finalizeError)
		}
	}()

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
			outputs := zbstore.RealizationMap{
				DerivationHash: drvHash,
				Realizations: map[string][]*zbstore.Realization{
					zbstore.DefaultDerivationOutputName: {
						{
							OutputPath: outputPath,
							// Fixed outputs don't have references.
						},
					},
				},
			}
			if err := b.recordRealizations(ctx, conn, buildResultID, outputs); err != nil {
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
	buildSystemDeps := drv.Env[buildSystemDepsVar]
	if hasPlaceholders(drv, buildSystemDeps) {
		return fmt.Errorf("build %s: %s contains placeholders", drvPath, buildSystemDeps)
	}
	for dep := range strings.FieldsSeq(buildSystemDeps) {
		if !xmaps.HasKey(b.server.sandboxPaths, dep) {
			return fmt.Errorf("build %s: system dependency %s not allowed", drvPath, buildSystemDeps)
		}
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
	tempOutPaths, err := b.runBuilder(ctx, conn, drvPath, buildResultID, keepFailed, buildUser, runner)
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
	inputs, err := b.inputs(conn, drvPath)
	if err != nil {
		return err
	}
	inputPaths := sets.CollectSorted(maps.Keys(inputs))
	outputs := zbstore.RealizationMap{
		DerivationHash: drvHash,
		Realizations:   make(map[string][]*zbstore.Realization),
	}
	for outputName, tempOutputPath := range tempOutPaths {
		ref := zbstore.OutputReference{
			DrvPath:    drvPath,
			OutputName: outputName,
		}
		info, err := b.postprocess(ctx, conn, ref, tempOutputPath, unlockFixedOutput, inputPaths)
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

		r := &zbstore.Realization{OutputPath: info.StorePath}
		for ref, eqClasses := range inputs {
			if info.References.Has(ref) {
				for eqClass := range eqClasses.All() {
					r.ReferenceClasses = append(r.ReferenceClasses, &zbstore.ReferenceClass{
						Path:        ref,
						Realization: zbstore.NonNull(eqClass.toRealizationOutputReference()),
					})
				}
			}
		}
		r.Signatures, err = b.server.keyring.Sign(zbstore.RealizationOutputReference{
			DerivationHash: drvHash,
			OutputName:     outputName,
		}, r)
		if err != nil {
			log.Warnf(ctx, "Signing built realization: %v", err)
		}
		outputs.Realizations[outputName] = []*zbstore.Realization{r}
	}

	// Record realizations.
	if err := b.recordRealizations(ctx, conn, buildResultID, outputs); err != nil {
		return fmt.Errorf("build %s: %v", drvPath, err)
	}

	log.Infof(ctx, "Built %s: %s", drvPath, formatOutputPaths(maps.Collect(func(yield func(string, zbstore.Path) bool) {
		for ref, r := range outputs.All() {
			if !yield(ref.OutputName, r.OutputPath) {
				return
			}
		}
	})))
	return nil
}

// inputs computes the closure of all inputs used by the derivation at drvPath.
func (b *builder) inputs(conn *sqlite.Conn, drvPath zbstore.Path) (map[zbstore.Path]sets.Set[equivalenceClass], error) {
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

	rollback, err := readonlySavepoint(conn)
	if err != nil {
		return nil, fmt.Errorf("input closure for %s: %v", drvPath, err)
	}
	defer rollback()

	for _, input := range drv.InputSources.All() {
		err := closurePaths(conn, pathAndEquivalenceClass{path: input}, func(pe pathAndEquivalenceClass) bool {
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
	// cores is a hint from the user to the builder
	// on the number of concurrent jobs to perform.
	cores int
	// sandboxPaths is a map of paths inside the sandbox
	// to paths on the host machine.
	// For sandboxed runners, these paths will be made available inside the sandbox.
	sandboxPaths map[string]string
}

// builderLogInterval is the maximum time between flushes of the builder log.
const builderLogInterval = 100 * time.Millisecond

func (b *builder) runBuilder(ctx context.Context, conn *sqlite.Conn, drvPath zbstore.Path, buildResultID int64, keepFailed bool, buildUser *BuildUser, f runnerFunc) (outPaths map[string]zbstore.Path, err error) {
	drvName, isDrv := drvPath.DerivationName()
	if !isDrv {
		return nil, fmt.Errorf("build %s: not a derivation", drvPath)
	}
	drv := b.derivations[drvPath]
	if drv == nil {
		return nil, fmt.Errorf("build %s: unknown derivation", drvPath)
	}

	outPaths, err = tempOutputPaths(drvPath, drv.Outputs)
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

	buildDir, err := os.MkdirTemp(b.server.buildDir, "zb-build-"+drvName+"-*")
	if err != nil {
		return nil, fmt.Errorf("build %s: %v", drvPath, err)
	}
	startedRun := false
	defer func() {
		if err != nil && startedRun && keepFailed {
			if b.server.allowKeepFailed {
				log.Infof(ctx, "Build of %s failed and user requested build directory %s be kept", drvPath, buildDir)
				if runtime.GOOS != "windows" {
					if err := os.Chmod(buildDir, 0o755); err != nil {
						log.Warnf(ctx, "Unable to make %s readable: %v", buildDir, err)
					}
				}
				return
			}
			log.Debugf(ctx, "Build of %s failed and user requested build directory be kept, but server policy is to discard.", drvPath)
		}
		if err := os.RemoveAll(buildDir); err != nil {
			log.Warnf(ctx, "Failed to clean up %s: %v", buildDir, err)
		}
	}()
	if buildUser != nil {
		if err := os.Chown(buildDir, buildUser.UID, -1); err != nil {
			return nil, fmt.Errorf("build %s: %v", drvPath, err)
		}
	}
	logFile, err := createBuilderLog(b.server.logDir, b.id, drvPath)
	if err != nil {
		return nil, fmt.Errorf("build %s: %v", drvPath, err)
	}
	defer func() {
		if err := logFile.Close(); err != nil {
			log.Warnf(ctx, "Closing build log for %s: %v", drvPath, err)
		}
	}()

	r := newReplacer(xiter.Chain2(
		outputPathRewrites(outPaths),
		maps.All(inputRewrites),
	))
	expandedDrv := drv.ReplaceStrings(r)

	log.Debugf(ctx, "Starting builder for %s...", drvPath)
	if err := recordBuilderStart(conn, buildResultID, time.Now()); err != nil {
		log.Warnf(ctx, "For %s: %v", drvPath, err)
	}
	startedRun = true
	builderError := f(ctx, &builderInvocation{
		derivation:     expandedDrv,
		derivationPath: drvPath,
		outputPaths:    outPaths,

		realStoreDir: b.server.realDir,
		buildDir:     buildDir,
		logWriter:    logFile,
		user:         buildUser,
		sandboxPaths: filterSandboxPaths(b.server.sandboxPaths, drv.Env[buildSystemDepsVar]),
		cores:        b.server.coresPerBuild,

		lookup: b.lookup,
		closure: func(path zbstore.Path, yield func(zbstore.Path) bool) error {
			pe := pathAndEquivalenceClass{path: path}
			return closurePaths(conn, pe, func(pe pathAndEquivalenceClass) bool {
				return yield(pe.path)
			})
		},
	})
	builderEndTime := time.Now()

	if builderError == nil {
		// Verify that builder produced all outputs.
		for outputName, outputPath := range outPaths {
			if _, err := os.Lstat(b.server.realPath(outputPath)); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					builderError = builderFailure{fmt.Errorf("builder failed to produce output $%s", outputName)}
				} else {
					builderError = builderFailure{fmt.Errorf("output $%s: %v", outputName, err)}
				}
				break
			}
		}
	}

	if builderError != nil {
		log.Debugf(ctx, "Builder for %s has failed: %v", drvPath, builderError)
		var buf []byte
		buf = append(buf, "*** Build failed"...)
		if isBuilderFailure(builderError) {
			// Internal errors are appended to the log during [finalizeBuildResult].
			buf = append(buf, ": "...)
			buf = append(buf, builderError.Error()...)
		}
		buf = append(buf, "\n"...)
		if keepFailed && b.server.allowKeepFailed {
			buf = append(buf, "Build directory available at "...)
			buf = append(buf, buildDir...)
			buf = append(buf, "\n"...)
		}
		if _, err := logFile.Write(buf); err != nil {
			log.Debugf(ctx, "While writing failed build directory info: %v", err)
		}
	}

	if err := recordBuilderEnd(conn, buildResultID, builderEndTime); err != nil {
		log.Warnf(ctx, "For %s: %v", drvPath, err)
	}
	if builderError != nil {
		for outName, outPath := range outPaths {
			if err := os.RemoveAll(string(outPath)); err != nil {
				ref := zbstore.OutputReference{
					DrvPath:    drvPath,
					OutputName: outName,
				}
				log.Warnf(ctx, "Clean up %v from failed build: %v", ref, err)
			}
		}
		return nil, fmt.Errorf("build %s: %w", drvPath, builderError)
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
	fillBaseEnv(env, invocation.derivation.Dir, invocation.buildDir, invocation.cores)
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

// hasPlaceholders reports whether s contains any placeholders
// that would be substituted when evaluated for drv.
func hasPlaceholders(drv *zbstore.Derivation, s string) bool {
	for outputName := range drv.Outputs {
		if strings.Contains(s, zbstore.HashPlaceholder(outputName)) {
			return true
		}
	}
	for ref := range drv.InputDerivationOutputs() {
		if strings.Contains(s, zbstore.UnknownCAOutputPlaceholder(ref)) {
			return true
		}
	}
	return false
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
func (b *builder) postprocess(ctx context.Context, conn *sqlite.Conn, output zbstore.OutputReference, buildPath zbstore.Path, unlockBuildPath func(), inputs *sets.Sorted[zbstore.Path]) (*ObjectInfo, error) {
	drv := b.derivations[output.DrvPath]
	if drv == nil {
		return nil, fmt.Errorf("post-process %v: unknown derivation", output)
	}
	outputType, hasOutput := drv.Outputs[output.OutputName]
	if !hasOutput {
		return nil, fmt.Errorf("post-process %v: no such output", output)
	}

	var info *ObjectInfo
	var err error
	if ca, ok := outputType.FixedCA(); ok {
		if unlockBuildPath == nil {
			return nil, fmt.Errorf("post-process %v: write lock was not held", output)
		}
		defer unlockBuildPath()

		info, err = b.postprocessFixedOutput(ctx, conn, buildPath, ca)
	} else {
		if unlockBuildPath != nil {
			unlockBuildPath()
			return nil, fmt.Errorf("post-process %v: unexpected write lock", output)
		}
		// outputType has presumably been validated with [validateOutputs].
		info, err = b.postprocessFloatingOutput(ctx, conn, buildPath, inputs)
	}
	return info, err
}

// postprocessFixedOutput computes the NAR hash of the given store path
// and verifies that it matches the content address.
func (b *builder) postprocessFixedOutput(ctx context.Context, conn *sqlite.Conn, outputPath zbstore.Path, ca zbstore.ContentAddress) (info *ObjectInfo, err error) {
	log.Debugf(ctx, "Verifying fixed output %s...", outputPath)

	realOutputPath := b.server.realPath(outputPath)
	wc := new(xio.WriteCounter)
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

	if _, err := verifyContentAddress(ctx, outputPath, pr, nil, ca, b.server.caCreateTemp); err != nil {
		return nil, err
	}

	info = &ObjectInfo{
		StorePath: outputPath,
		NARHash:   h.SumHash(),
		NARSize:   int64(*wc),
		CA:        ca,
	}
	err = func() (err error) {
		endFn, err := sqlitex.ImmediateTransaction(conn)
		if err != nil {
			return err
		}
		defer endFn(&err)
		return insertObject(ctx, conn, info)
	}()
	if err != nil {
		err = fmt.Errorf("post-process %v: %v", outputPath, err)
	}

	freeze(ctx, realOutputPath)

	return info, nil
}

func (b *builder) postprocessFloatingOutput(ctx context.Context, conn *sqlite.Conn, buildPath zbstore.Path, inputs *sets.Sorted[zbstore.Path]) (*ObjectInfo, error) {
	log.Debugf(ctx, "Processing floating output %s...", buildPath)
	realBuildPath := b.server.realPath(buildPath)
	scan, err := scanFloatingOutput(ctx, realBuildPath, buildPath.Digest(), inputs, b.server.caCreateTemp)
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

	info := &ObjectInfo{
		StorePath:  finalPath,
		NARSize:    scan.narSize,
		References: *scan.refs.ToSet(finalPath),
		CA:         scan.ca,
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
	err = finalizeFloatingOutput(finalPath.Dir(), realBuildPath, realFinalPath, scan.analysis)
	if err != nil {
		return nil, fmt.Errorf("post-process %s: %v", buildPath, err)
	}
	if scan.refs.Self {
		h := nix.NewHasher(nix.SHA256)
		if err := nar.DumpPath(h, realFinalPath); err != nil {
			return nil, fmt.Errorf("post-process %s: %v", buildPath, err)
		}
		info.NARHash = h.SumHash()
	}

	err = func() (err error) {
		endFn, err := sqlitex.ImmediateTransaction(conn)
		if err != nil {
			return err
		}
		defer endFn(&err)
		return insertObject(ctx, conn, info)
	}()
	if err != nil {
		return nil, fmt.Errorf("post-process %v: %v", buildPath, err)
	}

	freeze(ctx, realFinalPath)

	return info, nil
}

type outputScanResults struct {
	ca       zbstore.ContentAddress
	narHash  nix.Hash // only filled in if refs.Self is false
	narSize  int64
	analysis *zbstore.SelfReferenceAnalysis
	refs     zbstore.References
}

// scanFloatingOutput gathers information about a newly built filesystem object.
// The digest is used to detect self references.
// closure is the transitive closure of store objects the derivation depends on,
// which form the superset of all non-self-references that the scan can detect.
func scanFloatingOutput(ctx context.Context, path string, digest string, closure *sets.Sorted[zbstore.Path], createTemp bytebuffer.Creator) (*outputScanResults, error) {
	log.Debugf(ctx, "Scanning for references in %s. Possible: %s", path, closure)
	wc := new(xio.WriteCounter)
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

	ca, analysis, err := zbstore.SourceSHA256ContentAddress(pr, &zbstore.ContentAddressOptions{
		Digest:     digest,
		CreateTemp: createTemp,
		Log:        func(msg string) { log.Debugf(ctx, "%s", msg) },
	})
	if err != nil {
		return nil, err
	}

	refs := zbstore.References{
		Self: analysis.HasSelfReferences(),
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
		ca:       ca,
		narSize:  int64(*wc),
		refs:     refs,
		analysis: analysis,
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
func finalizeFloatingOutput(dir zbstore.Directory, buildPath, finalPath string, analysis *zbstore.SelfReferenceAnalysis) error {
	fakeBuildPath, err := dir.Object(filepath.Base(buildPath))
	if err != nil {
		return fmt.Errorf("move %s to %s: %v", buildPath, finalPath, err)
	}
	fakeFinalPath, err := dir.Object(filepath.Base(finalPath))
	if err != nil {
		return fmt.Errorf("move %s to %s: %v", buildPath, finalPath, err)
	}
	if fakeBuildPath.Name() != fakeFinalPath.Name() {
		return fmt.Errorf("move %s to %s: object names do not match", buildPath, finalPath)
	}

	for i := range analysis.Paths {
		hdr := &analysis.Paths[i]
		err := rewriteAtPath(
			filepath.Join(buildPath, filepath.FromSlash(hdr.Path)),
			hdr.ContentOffset,
			fakeFinalPath.Digest(),
			analysis.RewritesInRange(hdr.ContentOffset, hdr.ContentOffset+hdr.Size),
		)
		if err != nil {
			return fmt.Errorf("move %s to %s: %v", buildPath, finalPath, err)
		}
	}

	return os.Rename(buildPath, finalPath)
}

func rewriteAtPath(path string, baseOffset int64, newDigest string, rewriters []zbstore.Rewriter) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	switch info.Mode().Type() {
	case 0:
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return err
		}
		writeError := zbstore.Rewrite(f, baseOffset, newDigest, rewriters)
		closeError := f.Close()
		if writeError != nil {
			return fmt.Errorf("rewrite %s: %v", path, writeError)
		}
		if closeError != nil {
			return fmt.Errorf("rewrite %s: %v", path, closeError)
		}
	case os.ModeSymlink:
		oldTarget, err := os.Readlink(path)
		if err != nil {
			return err
		}
		buf := bytebuffer.New([]byte(oldTarget))
		if err := zbstore.Rewrite(buf, baseOffset, newDigest, rewriters); err != nil {
			return err
		}
		sb := new(strings.Builder)
		sb.Grow(int(buf.Size()))
		buf.Seek(0, io.SeekStart)
		buf.WriteTo(sb)
		newTarget := sb.String()
		if newTarget == oldTarget {
			// Nothing to do.
			return nil
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("replace symlink %s: %v", path, err)
		}
		if err := os.Symlink(newTarget, path); err != nil {
			return err
		}
	default:
		return fmt.Errorf("rewrite %s: unknown file type", path)
	}
	return nil
}

// recordRealizations calls [recordRealizations] and [recordBuildOutputs] in a transaction
// and on success, saves the realizations into b.realizations.
// The outputs must exist in the store.
func (b *builder) recordRealizations(ctx context.Context, conn *sqlite.Conn, buildResultID int64, outputs zbstore.RealizationMap) (err error) {
	if log.IsEnabled(log.Debug) {
		outputPaths := make(map[string]zbstore.Path)
		for ref, r := range outputs.All() {
			outputPaths[ref.OutputName] = r.OutputPath
		}
		log.Debugf(ctx, "Recording realizations for %v: %s", outputs.DerivationHash, formatOutputPaths(outputPaths))
	}

	endFn, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return fmt.Errorf("record realizations for %v: %v", outputs.DerivationHash, err)
	}
	defer endFn(&err)
	if err := recordRealizations(conn, outputs.All()); err != nil {
		return err
	}
	buildOutputs := func(yield func(string, zbstore.Path) bool) {
		for ref, r := range outputs.All() {
			if !yield(ref.OutputName, r.OutputPath) {
				return
			}
		}
	}
	if err := setBuildResultOutputs(conn, buildResultID, buildOutputs); err != nil {
		return err
	}

	for ref, r := range outputs.All() {
		closure := make(map[zbstore.Path]sets.Set[equivalenceClass])
		eqClass := realizationOutputReferenceKey(ref)
		pe := pathAndEquivalenceClass{path: r.OutputPath, equivalenceClass: eqClass}
		err := closurePaths(conn, pe, func(pe pathAndEquivalenceClass) bool {
			addToMultiMap(closure, pe.path, pe.equivalenceClass)
			return true
		})
		if err != nil {
			return err
		}
		b.realizations[eqClass] = cachedRealization{
			path:    r.OutputPath,
			closure: closure,
		}
	}
	return nil
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

	// If the OS/architecture pair matches exactly, don't bother with anything else.
	if host.OS == want.OS && host.Arch == want.Arch {
		return true
	}

	// Perform a fuzzy match on operating systems and architectures we know about.
	sameOS := want.OS.IsMacOS() && host.OS.IsMacOS() ||
		want.OS.IsLinux() && host.OS.IsLinux() ||
		want.OS.IsWindows() && host.OS.IsWindows()
	if !sameOS {
		return false
	}
	// TODO(someday): There's probably more subtlety to the ARM comparison.
	sameFamily := want.Arch.IsX86() && host.Arch.IsX86() ||
		want.Arch.IsARM() && host.Arch.IsARM() ||
		want.Arch.IsRISCV() && host.Arch.IsRISCV()
	if !sameFamily {
		return false
	}
	return host.Arch.Is64Bit() && (want.Arch.Is64Bit() || want.Arch.Is32Bit()) ||
		host.Arch.Is32Bit() && want.Arch.Is32Bit()
}

// filterSandboxPaths computes the final mapping of paths to make available to the sandbox
// based on the __buildSystemDeps value in the derivation.
// If a path in depsList does not exist in sandboxPaths, it is ignored.
func filterSandboxPaths(sandboxPaths map[string]SandboxPath, depsList string) map[string]string {
	if len(sandboxPaths) == 0 {
		return nil
	}
	result := make(map[string]string, len(sandboxPaths))
	for path, opts := range sandboxPaths {
		if opts.AlwaysPresent {
			result[path] = cmp.Or(opts.Path, path)
		}
	}
	for path := range strings.FieldsSeq(depsList) {
		if opts, ok := sandboxPaths[path]; ok && !xmaps.HasKey(result, path) {
			result[path] = cmp.Or(opts.Path, path)
		}
	}
	return result
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

func joinStrings[T ~string, S ~[]T](elems S, sep string) string {
	switch len(elems) {
	case 0:
		return ""
	case 1:
		return string(elems[0])
	}

	var n int
	if len(sep) > 0 {
		n += len(sep) * (len(elems) - 1)
	}
	for _, elem := range elems {
		n += len(elem)
	}

	var b strings.Builder
	b.Grow(n)
	b.WriteString(string(elems[0]))
	for _, s := range elems[1:] {
		b.WriteString(sep)
		b.WriteString(string(s))
	}
	return b.String()
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
