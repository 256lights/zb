// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package backend

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	"zombiezen.com/go/zb/sortedset"
	"zombiezen.com/go/zb/zbstore"
)

func (s *Server) realize(ctx context.Context, req *jsonrpc.Request) (_ *jsonrpc.Response, err error) {
	type stackFrame struct {
		drvPath     zbstore.Path
		origDrvPath zbstore.Path
		drv         *zbstore.Derivation
		unlock      func()
	}

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

	conn, err := s.db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer s.db.Put(conn)

	usedRealizations := make(map[zbstore.Path]map[string]zbstore.Path)
	stack := []stackFrame{{
		drvPath: ultimateDrvPath,
	}}
	defer func() {
		for i := range stack {
			frame := &stack[i]
			if frame.unlock != nil {
				frame.unlock()
				frame.unlock = nil
			}
		}
	}()

	for len(stack) > 0 {
		curr := stack[len(stack)-1]
		stack[len(stack)-1] = stackFrame{}
		stack = stack[:len(stack)-1]

		if len(usedRealizations[curr.drvPath]) > 0 {
			continue
		}

		if curr.unlock != nil {
			log.Debugf(ctx, "Resuming %s", curr.drvPath)
		} else {
			// First visit to path.
			log.Debugf(ctx, "Reached %s", curr.drvPath)
			var err error
			curr.unlock, err = s.inProgress.lock(ctx, curr.drvPath)
			if err != nil {
				return nil, err
			}
			log.Debugf(ctx, "Acquired lock on %s", curr.drvPath)
			var existingOutputs map[string]zbstore.Path
			var unlockFixedOutput func()
			curr.drv, existingOutputs, unlockFixedOutput, err = s.preProcessDerivation(ctx, conn, curr.drvPath)
			if unlockFixedOutput != nil {
				prevUnlock := curr.unlock
				curr.unlock = func() {
					prevUnlock()
					unlockFixedOutput()
				}
			}
			if err != nil {
				curr.unlock()
				return nil, err
			}
			if len(existingOutputs) > 0 {
				if log.IsEnabled(log.Debug) {
					log.Debugf(ctx, "Found existing outputs for %s: %s", curr.drvPath, formatOutputPaths(existingOutputs))
				}
				curr.unlock()
				usedRealizations[curr.drvPath] = existingOutputs
				continue
			}
			unmetDependencies := false
			for inputDrv, inputOutputNames := range curr.drv.InputDerivations {
				if inputOutputNames.Len() == 0 {
					continue
				}
				existingOutputs := usedRealizations[inputDrv]
				if len(existingOutputs) == 0 {
					if !unmetDependencies {
						stack = append(stack, curr)
						unmetDependencies = true
					}
					log.Debugf(ctx, "Enqueuing %s to be built (dependency of %s)", inputDrv, curr.drvPath)
					stack = append(stack, stackFrame{
						drvPath: inputDrv,
					})
				} else {
					for _, outName := range inputOutputNames.All() {
						if existingOutputs[outName] == "" {
							// As usual, if we bail, we need to unlock curr.
							// However, if we have unmet dependencies,
							// we've already added curr to the stack
							// and it will be unlocked by the catch-all defer.
							if !unmetDependencies {
								curr.unlock()
							}

							return nil, fmt.Errorf("build %s: no output named %q known for input %s", curr.drvPath, outName, inputDrv)
						}
					}
				}
			}
			if unmetDependencies {
				log.Debugf(ctx, "Pausing %s to build dependencies", curr.drvPath)
				continue
			}
		}

		// Resolve the derivation into one that uses the realized outputs.
		if len(curr.drv.InputDerivations) > 0 {
			resolvedPath, resolvedDrv, unlockResolved, err := s.resolveDerivation(ctx, conn, curr.drv, usedRealizations)
			if err != nil {
				curr.unlock()
				return nil, fmt.Errorf("resolve %s: %v", curr.drvPath, err)
			}
			log.Infof(ctx, "Resolved %s -> %s", curr.drvPath, resolvedPath)
			unlockOrig := curr.unlock
			stack = append(stack, stackFrame{
				drvPath:     resolvedPath,
				drv:         resolvedDrv,
				origDrvPath: curr.drvPath,
				unlock: func() {
					unlockResolved()
					unlockOrig()
				},
			})
			continue
		}

		// Arrange for builder to run.
		outPaths, err := runBuilderUnsandboxed(ctx, curr.drvPath, curr.drv, s.buildDir)
		if err != nil {
			curr.unlock()
			resp := new(zbstore.RealizeResponse)
			for _, outputName := range sortedKeys(curr.drv.Outputs) {
				resp.Outputs = append(resp.Outputs, &zbstore.RealizeOutput{
					Name: outputName,
				})
			}
			return marshalResponse(resp)
		}

		// Register outputs.
		err = func() (err error) {
			endFn, err := sqlitex.ImmediateTransaction(conn)
			if err != nil {
				return err
			}
			defer endFn(&err)

			for outputName, tempOutputPath := range outPaths {
				outputType := curr.drv.Outputs[outputName]
				info, err := postProcessBuiltOutput(ctx, s.realDir, curr.drvPath, tempOutputPath, outputType, &curr.drv.InputSources)
				switch {
				case errors.Is(err, errFloatingOutputExists):
					// No need to register an object in the database.
					log.Debugf(ctx, "%s is the same output as %s (reusing)", tempOutputPath, info.StorePath)
				case err != nil:
					return fmt.Errorf("output %s: %v", outputName, err)
				default:
					if err := insertObject(ctx, conn, info); err != nil {
						return fmt.Errorf("output %s: %v", outputName, err)
					}
				}
				outPaths[outputName] = info.StorePath
			}
			if err := recordRealizations(ctx, conn, curr.drvPath, outPaths); err != nil {
				return err
			}
			usedRealizations[curr.drvPath] = outPaths
			if curr.origDrvPath != "" {
				if err := recordRealizations(ctx, conn, curr.origDrvPath, outPaths); err != nil {
					return err
				}
				usedRealizations[curr.origDrvPath] = outPaths
			}
			return nil
		}()
		curr.unlock()
		if err != nil {
			return nil, fmt.Errorf("build %s: %v", curr.drvPath, err)
		}
	}

	resp := new(zbstore.RealizeResponse)
	outPaths := usedRealizations[ultimateDrvPath]
	for outputName, outputPath := range sortedMap(outPaths) {
		resp.Outputs = append(resp.Outputs, &zbstore.RealizeOutput{
			Name: outputName,
			Path: zbstore.NonNull(outputPath),
		})
	}
	return marshalResponse(resp)
}

// preProcessDerivation checks whether the derivation has existing realizations
// or otherwise reads the derivation and ensures it is suitable for realizing.
// If the derivation is fixed-output and the output does not exist,
// preProcessDerivation will acquire the s.inProgress lock for it.
func (s *Server) preProcessDerivation(ctx context.Context, conn *sqlite.Conn, drvPath zbstore.Path) (_ *zbstore.Derivation, _ map[string]zbstore.Path, unlockFixedOutput func(), err error) {
	existing, err := findExistingRealizations(ctx, conn, drvPath)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(existing) > 0 {
		// TODO(soon): Check whether realization paths exist on disk.

		// TODO(maybe): This may not match the set of outputs present in the derivation.
		// Should we always read the derivation?
		flattened := make(map[string]zbstore.Path, len(existing))
		for outputName, outputPaths := range existing {
			// TODO(someday): We should use a heuristic and consult the client's trust settings.
			// Or maybe even error.
			// But until then, we just pick the first path in sorted order.
			flattened[outputName] = outputPaths.At(0)
		}
		return nil, flattened, nil, nil
	}

	drv, err := s.readDerivation(drvPath)
	if err != nil {
		return nil, nil, nil, err
	}

	if outputPath, ok := fixedOutputPath(drv); ok {
		log.Debugf(ctx, "%s has fixed output %s. Waiting for lock to check for reuse...", drvPath, outputPath)
		unlockFixedOutput, err = s.inProgress.lock(ctx, outputPath)
		if err != nil {
			return drv, nil, nil, fmt.Errorf("build %s: wait for %s: %w", drvPath, outputPath, err)
		}
		capturedUnlockFixedOutput := unlockFixedOutput
		defer func() {
			if err != nil {
				capturedUnlockFixedOutput()
			}
		}()

		realOutputPath := filepath.Join(s.realDir, outputPath.Base())
		_, err = os.Lstat(realOutputPath)
		log.Debugf(ctx, "%s exists=%t (output of %s)", outputPath, err == nil, drvPath)
		if err == nil {
			outputPaths := map[string]zbstore.Path{
				zbstore.DefaultDerivationOutputName: outputPath,
			}
			if err := recordRealizations(ctx, conn, drvPath, outputPaths); err != nil {
				return drv, outputPaths, nil, fmt.Errorf("build %s: %v", drvPath, err)
			}
			unlockFixedOutput()
			return drv, outputPaths, nil, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return drv, nil, nil, err
		}
	}

	if !canBuildLocally(drv) {
		unlockFixedOutput()
		return drv, nil, nil, fmt.Errorf("build %s: a %s system is required, but host is a %v system", drvPath, drv.System, system.Current())
	}
	for _, input := range drv.InputSources.All() {
		log.Debugf(ctx, "Waiting for lock on %s (input to %s)...", input, drvPath)
		unlockInput, err := s.inProgress.lock(ctx, input)
		if err != nil {
			unlockFixedOutput()
			return drv, nil, nil, fmt.Errorf("build %s: wait for %s: %w", drvPath, input, err)
		}
		realInputPath := filepath.Join(s.realDir, input.Base())
		_, err = os.Lstat(realInputPath)
		unlockInput()
		log.Debugf(ctx, "%s exists=%t (input to %s)", input, err == nil, drvPath)
		if err != nil {
			// TODO(someday): Import from substituter if not found.
			unlockFixedOutput()
			return drv, nil, nil, fmt.Errorf("build %s: input %s not present (%v)", drvPath, input, err)
		}
	}
	return drv, nil, unlockFixedOutput, nil
}

func findExistingRealizations(ctx context.Context, conn *sqlite.Conn, drvPath zbstore.Path) (map[string]*sortedset.Set[zbstore.Path], error) {
	result := make(map[string]*sortedset.Set[zbstore.Path])
	err := sqlitex.ExecuteTransientFS(conn, sqlFiles(), "find_realizations.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":drv_path": drvPath,
		},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			outName := stmt.GetText("output_name")
			rawPath := stmt.GetText("output_path")
			outPath, err := zbstore.ParsePath(rawPath)
			if err != nil {
				log.Warnf(ctx, "Database contains realization with invalid path %q for %s!%s (%v)",
					rawPath, drvPath, outName, err)
				return nil
			}
			if outPath.Dir() != drvPath.Dir() {
				log.Warnf(ctx, "Database contains realization %s for %s!%s (wrong directory!)",
					outPath, drvPath, outName)
				return nil
			}
			outSet := result[outName]
			if outSet == nil {
				outSet = new(sortedset.Set[zbstore.Path])
				result[outName] = outSet
			}
			outSet.Add(outPath)
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("find existing realizations for %s: %v", drvPath, err)
	}
	return result, nil
}

func (s *Server) readDerivation(drvPath zbstore.Path) (*zbstore.Derivation, error) {
	drvName, isDrv := drvPath.DerivationName()
	if !isDrv {
		return nil, fmt.Errorf("read derivation %s: not a %s file", drvPath, zbstore.DerivationExt)
	}
	realDrvPath := filepath.Join(s.realDir, drvPath.Base())
	if info, err := os.Lstat(realDrvPath); err != nil {
		return nil, err
	} else if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("read derivation %s: not a regular file", drvPath)
	}
	drvData, err := os.ReadFile(realDrvPath)
	if err != nil {
		return nil, fmt.Errorf("read derivation %s: %v", drvPath, err)
	}
	drv, err := zbstore.ParseDerivation(s.dir, drvName, drvData)
	if err != nil {
		return nil, fmt.Errorf("read derivation %s: %v", drvPath, err)
	}
	if err := validateOutputs(drv); err != nil {
		return nil, fmt.Errorf("read derivation %s: %v", drvPath, err)
	}
	return drv, nil
}

func validateOutputs(drv *zbstore.Derivation) error {
	if len(drv.Outputs) == 0 {
		return fmt.Errorf("derivation must have at least one output")
	}
	for outputName, outputType := range drv.Outputs {
		switch {
		case outputType.IsFixed():
			if outputName != zbstore.DefaultDerivationOutputName {
				return fmt.Errorf("output %s is fixed, but only %s is permitted to be fixed", outputName, zbstore.DefaultDerivationOutputName)
			}
			if len(drv.Outputs) != 1 {
				return fmt.Errorf("fixed-output derivations can only have a single output")
			}
		case outputType.IsFloating():
			if t, ok := outputType.HashType(); !ok || t != nix.SHA256 || !outputType.IsRecursiveFile() {
				return fmt.Errorf("floating output %s must use %v and be recursive (uses %v and recursive=%t)",
					outputName, nix.SHA256, t, outputType.IsRecursiveFile())
			}
		default:
			return fmt.Errorf("output %s is neither fixed nor floating", outputName)
		}
	}
	return nil
}

func fixedOutputPath(drv *zbstore.Derivation) (zbstore.Path, bool) {
	if len(drv.Outputs) != 1 {
		return "", false
	}
	out := drv.Outputs[zbstore.DefaultDerivationOutputName]
	if !out.IsFixed() {
		return "", false
	}
	return out.Path(drv.Dir, drv.Name, zbstore.DefaultDerivationOutputName)
}

// resolveDerivation rewrites a derivation with input derivations
// into one that uses the provided realizations as input sources,
// then writes the derivation to the store.
func (s *Server) resolveDerivation(ctx context.Context, conn *sqlite.Conn, drv *zbstore.Derivation, realizations map[zbstore.Path]map[string]zbstore.Path) (resolvedDrvPath zbstore.Path, resolvedDrv *zbstore.Derivation, unlock func(), err error) {
	var rewrites []string
	newInputs := new(sortedset.Set[zbstore.Path])
	for inputDrvPath, inputOutputNames := range drv.InputDerivations {
		for _, outputName := range inputOutputNames.All() {
			placeholder := zbstore.UnknownCAOutputPlaceholder(inputDrvPath, outputName)
			actualPath := realizations[inputDrvPath][outputName]
			if actualPath == "" {
				return "", nil, nil, fmt.Errorf("resolve derivation: missing realization for %s!%s", inputDrvPath, outputName)
			}
			newInputs.Add(actualPath)
			rewrites = append(rewrites, placeholder, string(actualPath))
		}
	}
	resolvedDrv = expandDerivationPlaceholders(strings.NewReplacer(rewrites...), drv)
	resolvedDrv.InputSources.AddSet(newInputs)

	resolvedDrvInfo, _, resolvedDrvData, err := resolvedDrv.Export(nix.SHA256)
	if err != nil {
		return "", nil, nil, fmt.Errorf("resolve derivation: %v", err)
	}
	resolvedDrvPath = resolvedDrvInfo.StorePath
	log.Debugf(ctx, "Intending to write derivation %s", resolvedDrvPath)
	unlock, err = s.inProgress.lock(ctx, resolvedDrvPath)
	if err != nil {
		return "", nil, nil, fmt.Errorf("resolve derivation: wait for %s: %v", resolvedDrvPath, err)
	}
	log.Debugf(ctx, "Acquired lock on derivation %s", resolvedDrvPath)
	capturedUnlock := unlock
	defer func() {
		if err != nil {
			capturedUnlock()
		}
	}()
	realDrvPath := filepath.Join(s.buildDir, resolvedDrvPath.Base())
	f, err := os.OpenFile(realDrvPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return "", nil, nil, fmt.Errorf("resolve derivation: write %s: %v", resolvedDrvPath, err)
	}
	created, err := ensureFileContent(f, resolvedDrvData)
	if err != nil {
		return "", nil, nil, fmt.Errorf("resolve derivation: write %s: %v", resolvedDrvPath, err)
	}
	if created {
		endFn, err := sqlitex.ImmediateTransaction(conn)
		if err != nil {
			if err := os.Remove(realDrvPath); err != nil {
				log.Errorf(ctx, "Failed to clean up after failed database insert: %v", err)
			}
			return "", nil, nil, fmt.Errorf("resolve derivation: %v", err)
		}
		defer endFn(&err)
		if err := insertObject(ctx, conn, resolvedDrvInfo); err != nil {
			if err := os.Remove(realDrvPath); err != nil {
				log.Errorf(ctx, "Failed to clean up after failed database insert: %v", err)
			}
			return "", nil, nil, fmt.Errorf("resolve derivation: %v", err)
		}
	}
	return resolvedDrvInfo.StorePath, resolvedDrv, unlock, nil
}

type replacer interface {
	Replace(s string) string
}

// expandDerivationPlaceholders returns a copy of drv
// with r.Replace applied to its builder, builder arguments, and environment variables.
// The returned derivation always has InputDerivations set to nil.
func expandDerivationPlaceholders(r replacer, drv *zbstore.Derivation) *zbstore.Derivation {
	drvCopy := &zbstore.Derivation{
		Dir:          drv.Dir,
		Name:         drv.Name,
		InputSources: *drv.InputSources.Clone(),
		Outputs:      maps.Clone(drv.Outputs),
		System:       drv.System,
		Builder:      r.Replace(drv.Builder),
	}
	if len(drv.Args) > 0 {
		drvCopy.Args = make([]string, len(drv.Args))
		for i, arg := range drv.Args {
			drvCopy.Args[i] = r.Replace(arg)
		}
	}
	if len(drv.Env) > 0 {
		drvCopy.Env = make(map[string]string, len(drv.Env))
		for k, v := range drv.Env {
			drvCopy.Env[r.Replace(k)] = r.Replace(v)
		}
	}
	return drvCopy
}

type fileWriter interface {
	fs.File
	io.Writer
}

// ensureFileContent writes data to f it is empty,
// or verifies that the existing content is equal to data otherwise.
// ensureFileContent always closes f.
func ensureFileContent(f fileWriter, data []byte) (created bool, err error) {
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()

	info, err := f.Stat()
	if err != nil {
		return false, err
	}

	if gotSize := info.Size(); gotSize != 0 {
		if gotSize != int64(len(data)) {
			return false, fmt.Errorf("existing file content differs")
		}
		got, err := io.ReadAll(f)
		if err != nil {
			return false, fmt.Errorf("read existing content: %v", err)
		}
		if !bytes.Equal(got, data) {
			return false, fmt.Errorf("existing file content differs")
		}
		return false, nil
	}

	_, err = f.Write(data)
	return true, err
}

func runBuilderUnsandboxed(ctx context.Context, drvPath zbstore.Path, drv *zbstore.Derivation, buildDir string) (outPaths map[string]zbstore.Path, err error) {
	drvName, isDrv := drvPath.DerivationName()
	if !isDrv {
		return nil, fmt.Errorf("build %s: not a derivation", drvPath)
	}

	outPaths, r, err := tempOutputPaths(drvPath, drv.Outputs)
	if err != nil {
		return nil, fmt.Errorf("build %s: %v", drvPath, err)
	}
	if log.IsEnabled(log.Debug) {
		log.Debugf(ctx, "Output map for %s: %s", drvPath, formatOutputPaths(outPaths))
	}

	topTempDir, err := os.MkdirTemp(buildDir, "zb-build-"+drvName+"*")
	if err != nil {
		return nil, fmt.Errorf("build %s: %v", drvPath, err)
	}
	defer func() {
		if err := os.RemoveAll(topTempDir); err != nil {
			log.Warnf(ctx, "Failed to clean up %s: %v", topTempDir, err)
		}
	}()

	expandedDrv := expandDerivationPlaceholders(r, drv)
	baseEnv := make(map[string]string)
	addBaseEnv(baseEnv, drv.Dir, topTempDir)
	for k, v := range baseEnv {
		if _, overridden := expandedDrv.Env[k]; !overridden {
			expandedDrv.Env[k] = v
		}
	}

	c := exec.CommandContext(ctx, expandedDrv.Builder, expandedDrv.Args...)
	setCancelFunc(c)
	for k, v := range sortedMap(expandedDrv.Env) {
		c.Env = append(c.Env, k+"="+v)
	}
	c.Dir = topTempDir

	peerLogger := newRPCLogger(ctx, drvPath, peer(ctx))
	bufferedPeerLogger := batchio.NewWriter(peerLogger, 8192, 1*time.Second)
	defer bufferedPeerLogger.Flush()
	c.Stdout = bufferedPeerLogger
	c.Stderr = bufferedPeerLogger

	log.Debugf(ctx, "Starting builder for %s...", drvPath)
	if err := c.Run(); err != nil {
		log.Debugf(ctx, "Builder for %s has failed: %v", drvPath, err)
		// TODO(soon): Clean up outputs.
		return nil, fmt.Errorf("build %s: %w", drvPath, err)
	}

	log.Debugf(ctx, "Builder for %s has finished successfully", drvPath)
	return outPaths, nil
}

func tempOutputPaths(drvPath zbstore.Path, outputs map[string]*zbstore.DerivationOutput) (map[string]zbstore.Path, *strings.Replacer, error) {
	dir := drvPath.Dir()
	drvName, ok := drvPath.DerivationName()
	if !ok {
		return nil, nil, fmt.Errorf("compute output paths for %s: not a derivation", drvPath)
	}

	paths := make(map[string]zbstore.Path)
	var rewrites []string
	for outName, outType := range outputs {
		placeholder := zbstore.HashPlaceholder(outName)

		if !outType.IsFloating() {
			p, ok := outType.Path(dir, drvName, outName)
			if !ok {
				return nil, nil, fmt.Errorf("compute output path for %s!%s: unhandled output type", drvPath, outName)
			}
			paths[outName] = p
			rewrites = append(rewrites, placeholder, string(p))
			continue
		}

		tp, err := tempPath(drvPath, outName)
		if err != nil {
			return nil, nil, err
		}
		paths[outName] = tp
		rewrites = append(rewrites, placeholder, string(tp))
	}
	return paths, strings.NewReplacer(rewrites...), nil
}

// postProcessBuiltOutput computes the metadata for a realized output.
// drvPath is the store path of the ".drv" file that was realized.
// buildPath is the path of the store object created during realization.
// If outputType is fixed, then buildPath must be the store path computed by [zbstore.DerivationOutput.Path].
// inputs is the set of store paths that were inputs for the realized derivation.
//
// If postProcessBuiltOutput does not return an error,
// it guarantees that the store object at the returned info's path exists
// and has the hash and content address in the returned info.
// If the outputType is floating,
// then postProcessBuiltOutput likely will have moved the build artifact to its computed path.
func postProcessBuiltOutput(ctx context.Context, realStoreDir string, drvPath, buildPath zbstore.Path, outputType *zbstore.DerivationOutput, inputs *sortedset.Set[zbstore.Path]) (*zbstore.NARInfo, error) {
	if ca, ok := outputType.FixedCA(); ok {
		log.Debugf(ctx, "Verifying fixed output %s...", buildPath)
		narHash, narSize, err := postProcessFixedOutput(realStoreDir, buildPath, ca)
		if err != nil {
			return nil, err
		}
		return &zbstore.NARInfo{
			StorePath:   buildPath,
			Deriver:     drvPath,
			Compression: nix.NoCompression,
			NARHash:     narHash,
			NARSize:     narSize,
			CA:          ca,
		}, nil
	}

	// outputType has presumably been validated with [validateOutputs].
	info, err := postProcessFloatingOutput(ctx, realStoreDir, buildPath, inputs)
	if info != nil {
		info.Deriver = drvPath
	}
	return info, err
}

// postProcessFixedOutput computes the NAR hash of the given store path
// and verifies that it matches the content address.
func postProcessFixedOutput(realStoreDir string, outputPath zbstore.Path, ca zbstore.ContentAddress) (narHash nix.Hash, narSize int64, err error) {
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

var errFloatingOutputExists = errors.New("floating output resolved to existing store object")

func postProcessFloatingOutput(ctx context.Context, realStoreDir string, buildPath zbstore.Path, inputs *sortedset.Set[zbstore.Path]) (*zbstore.NARInfo, error) {
	log.Debugf(ctx, "Processing floating output %s...", buildPath)
	realBuildPath := filepath.Join(realStoreDir, buildPath.Base())
	scan, err := scanFloatingOutput(realBuildPath, buildPath.Digest(), inputs)
	if err != nil {
		return nil, fmt.Errorf("post-process %s: %v", buildPath, err)
	}

	finalPath, err := zbstore.FixedCAOutputPath(buildPath.Dir(), buildPath.Name(), scan.ca, scan.refs)
	if err != nil {
		return nil, fmt.Errorf("post-process %s: %v", buildPath, err)
	}
	log.Debugf(ctx, "Determined %s hashes to %s", buildPath, finalPath)

	// Bail early if this output exists in the store already.
	// TODO(maybe): Should this read the database instead?
	realFinalPath := filepath.Join(realStoreDir, finalPath.Base())
	if _, err := os.Lstat(realFinalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("post-process %s: %v", buildPath, err)
	} else if err == nil {
		err = fmt.Errorf("post-process %s (resolved to %s): %w", buildPath, finalPath, errFloatingOutputExists)
		return &zbstore.NARInfo{StorePath: finalPath}, err
	}

	var narHash nix.Hash
	if scan.refs.Self {
		var err error
		narHash, err = finalizeFloatingOutput(finalPath.Dir(), realBuildPath, realFinalPath)
		if err != nil {
			return nil, fmt.Errorf("post-process %s: %v", buildPath, err)
		}
	} else {
		// If there are no self references, we can do a simple rename.
		if err := os.Rename(realBuildPath, realFinalPath); err != nil {
			return nil, fmt.Errorf("post-process %s: %v", buildPath, err)
		}
		narHash = scan.narHash
	}

	return &zbstore.NARInfo{
		StorePath:   finalPath,
		Compression: zbstore.NoCompression,
		NARHash:     narHash,
		NARSize:     scan.narSize,
		References:  *scan.refs.ToSet(finalPath),
		CA:          scan.ca,
	}, nil
}

type outputScanResults struct {
	ca      zbstore.ContentAddress
	narHash nix.Hash // only filled in if refs.Self is false
	narSize int64
	refs    zbstore.References
}

// scanFloatingOutput gathers information about a newly built filesystem object.
// The digest is used to detect self references.
// inputs are other store objects the derivation depends on,
// which form the superset of all non-self-references that the scan can detect.
func scanFloatingOutput(path string, digest string, inputs *sortedset.Set[zbstore.Path]) (*outputScanResults, error) {
	inputDigests := make([]string, 0, inputs.Len())
	for _, input := range inputs.All() {
		inputDigests = append(inputDigests, input.Digest())
	}

	wc := new(writeCounter)
	h := nix.NewHasher(nix.SHA256)
	refFinder := detect.NewRefFinder(inputDigests)
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
		i, ok := sort.Find(inputs.Len(), func(i int) int {
			return strings.Compare(digest, inputs.At(i).Digest())
		})
		if !ok {
			return nil, fmt.Errorf("scan internal error: could not find digest %q in inputs", digest)
		}
		refs.Others.Add(inputs.At(i))
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
		return nix.Hash{}, fmt.Errorf("move %s to %s: %v", buildPath, finalPath, err)
	}
	if err := os.RemoveAll(buildPath); err != nil {
		return nix.Hash{}, fmt.Errorf("move %s to %s: %v", buildPath, finalPath, err)
	}
	if err := os.Rename(tempDestination, finalPath); err != nil {
		return nix.Hash{}, fmt.Errorf("move %s to %s: %v", buildPath, finalPath, err)
	}
	return h.SumHash(), nil
}

func canBuildLocally(drv *zbstore.Derivation) bool {
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
		return "", fmt.Errorf("make build temp path for %s!%s: %v", drvPath, outputName, err)
	}
	return p, nil
}

func recordRealizations(ctx context.Context, conn *sqlite.Conn, drvPath zbstore.Path, outputPaths map[string]zbstore.Path) (err error) {
	if log.IsEnabled(log.Debug) {
		log.Debugf(ctx, "Recording realizations for %s: %s", drvPath, formatOutputPaths(outputPaths))
	}

	defer sqlitex.Save(conn)(&err)

	if err := upsertPath(conn, drvPath); err != nil {
		return fmt.Errorf("record realizations for %s: %v", drvPath, err)
	}
	for _, p := range outputPaths {
		if err := upsertPath(conn, p); err != nil {
			return fmt.Errorf("record realizations for %s: %v", drvPath, err)
		}
	}

	stmt, err := sqlitex.PrepareTransientFS(conn, sqlFiles(), "insert_realization.sql")
	if err != nil {
		return fmt.Errorf("record realizations for %s: %v", drvPath, err)
	}
	defer stmt.Finalize()

	stmt.SetText(":drv_path", string(drvPath))
	for outputName, outputPath := range outputPaths {
		stmt.SetText(":output_name", outputName)
		stmt.SetText(":output_path", string(outputPath))
		if _, err := stmt.Step(); err != nil {
			return fmt.Errorf("record realizations for %s: %v", drvPath, err)
		}
		if err := stmt.Reset(); err != nil {
			return fmt.Errorf("record realizations for %s: %v", drvPath, err)
		}
	}

	return nil
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

// A mutexMap is a map of mutexes.
// The zero value is an empty map.
type mutexMap[T comparable] struct {
	mu sync.Mutex
	m  map[T]<-chan struct{}
}

// lock waits until it can either acquire the mutex for k
// or ctx.Done is closed.
// If lock acquires the mutex, it returns a function that will unlock the mutex and a nil error.
// Otherwise, lock returns a nil unlock function and the result of ctx.Err().
// Until unlock is called, all calls to mm.lock(k) for the same k will block.
// Multiple goroutines can call lock simultaneously.
func (mm *mutexMap[T]) lock(ctx context.Context, k T) (unlock func(), err error) {
	for {
		mm.mu.Lock()
		workDone := mm.m[k]
		if workDone == nil {
			c := make(chan struct{})
			if mm.m == nil {
				mm.m = make(map[T]<-chan struct{})
			}
			mm.m[k] = c
			mm.mu.Unlock()
			return func() {
				mm.mu.Lock()
				delete(mm.m, k)
				close(c)
				mm.mu.Unlock()
			}, nil
		}
		mm.mu.Unlock()

		select {
		case <-workDone:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func formatOutputPaths(m map[string]zbstore.Path) string {
	sb := new(strings.Builder)
	for i, outputName := range sortedKeys(m) {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(outputName)
		sb.WriteString(" -> ")
		sb.WriteString(string(m[outputName]))
	}
	return sb.String()
}

func sortedKeys[M ~map[K]V, K cmp.Ordered, V any](m M) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func sortedMap[M ~map[K]V, K cmp.Ordered, V any](m M) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for _, k := range sortedKeys(m) {
			if !yield(k, m[k]) {
				return
			}
		}
	}
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
