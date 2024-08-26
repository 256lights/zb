// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package backend

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

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
	// Step 1: Validate request.
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
	drvName, isDrv := drvPath.DerivationName()
	if !isDrv {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, fmt.Errorf("%s is not a derivation", drvPath))
	}
	log.Infof(ctx, "Requested to build %s", drvPath)
	if string(s.dir) != s.realDir {
		return nil, fmt.Errorf("store cannot build derivations (unsandboxed and storage directory does not match store)")
	}

	// Step 2: Parse derivation and determine whether to build it.
	// TODO(soon): Check for concurrent evaluations of path.
	conn, err := s.db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer s.db.Put(conn)
	if resp, err := findExistingRealizations(conn, drvPath); err != nil {
		return nil, err
	} else if len(resp.Outputs) > 0 {
		log.Debugf(ctx, "Found %d existing outputs for %s", len(resp.Outputs), drvPath)
		return marshalResponse(resp)
	}

	realDrvPath := filepath.Join(s.realDir, drvPath.Base())
	if info, err := os.Lstat(realDrvPath); err != nil {
		return nil, err
	} else if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", drvPath)
	}

	drvData, err := os.ReadFile(realDrvPath)
	if err != nil {
		return nil, err
	}
	drv, err := zbstore.ParseDerivation(s.dir, drvName, drvData)
	if err != nil {
		return nil, err
	}
	if !canBuildLocally(drv) {
		return nil, fmt.Errorf("build %s: a %s system is required, but host is a %v system", drvPath, drv.System, system.Current())
	}
	if err := validateOutputs(drv); err != nil {
		return nil, fmt.Errorf("build %s: %v", drvPath, err)
	}
	if len(drv.InputDerivations) > 0 {
		return nil, fmt.Errorf("TODO(soon): resolve derivation")
	}
	for i := 0; i < drv.InputSources.Len(); i++ {
		input := drv.InputSources.At(i)
		realInputPath := filepath.Join(s.realDir, input.Base())
		if _, err := os.Lstat(realInputPath); err != nil {
			// TODO(someday): Import from substituter if not found.
			return nil, fmt.Errorf("build %s: input %s not present (%v)", drvPath, input, err)
		}
	}

	// Step 3: Arrange for builder to run.
	outPaths, r, err := tempOutputPaths(drvPath, drv.Outputs)
	if err != nil {
		return nil, fmt.Errorf("build %s: %v", drvPath, err)
	}
	if log.IsEnabled(log.Debug) {
		log.Debugf(ctx, "Output map for %s: %s", drvPath, formatOutputPaths(outPaths))
	}
	// TODO(soon): Short-circuit if fixed output already exists in store.

	topTempDir, err := os.MkdirTemp(s.buildDir, "zb-build-"+drvName+"*")
	if err != nil {
		return nil, fmt.Errorf("build %s: %v", drvPath, err)
	}
	defer func() {
		if err := os.RemoveAll(topTempDir); err != nil {
			log.Warnf(ctx, "Failed to clean up %s: %v", topTempDir, err)
		}
	}()

	env := make(map[string]string)
	addBaseEnv(env, s.dir, topTempDir)
	for k, v := range drv.Env {
		env[r.Replace(k)] = r.Replace(v)
	}
	builderArgs := make([]string, 0, len(drv.Args))
	for _, arg := range drv.Args {
		builderArgs = append(builderArgs, r.Replace(arg))
	}

	c := exec.CommandContext(ctx, drv.Builder, builderArgs...)
	setCancelFunc(c)
	for _, k := range sortedKeys(env) {
		c.Env = append(c.Env, k+"="+env[k])
	}
	c.Dir = topTempDir
	// TODO(soon): Log stdout/stderr to caller.
	c.Stdout = os.Stderr
	c.Stderr = os.Stderr
	log.Debugf(ctx, "Starting builder for %s...", drvPath)
	if err := c.Run(); err != nil {
		log.Debugf(ctx, "Builder for %s has failed: %v", drvPath, err)

		// TODO(now): Clean up outputs.

		resp := new(zbstore.RealizeResponse)
		for _, outputName := range sortedKeys(drv.Outputs) {
			resp.Outputs = append(resp.Outputs, &zbstore.RealizeOutput{
				Name: outputName,
			})
		}
		return marshalResponse(resp)
	}
	log.Debugf(ctx, "Builder for %s has finished successfully", drvPath)

	// Step 3: Register outputs in database.
	endFn, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return nil, fmt.Errorf("build %s: %v", drvPath, err)
	}
	defer endFn(&err)

	for outputName, tempOutputPath := range outPaths {
		outputType := drv.Outputs[outputName]
		info, err := postProcessBuiltOutput(ctx, s.realDir, drvPath, tempOutputPath, outputType, &drv.InputSources)
		switch {
		case errors.Is(err, errFloatingOutputExists):
			// No need to register an object in the database.
			log.Debugf(ctx, "%s is the same output as %s (reusing)", tempOutputPath, info.StorePath)
		case err != nil:
			return nil, fmt.Errorf("build %s: output %s: %v", drvPath, outputName, err)
		default:
			if err := insertObject(ctx, conn, info); err != nil {
				return nil, fmt.Errorf("build %s: output %s: %v", drvPath, outputName, err)
			}
		}
		outPaths[outputName] = info.StorePath
	}
	if err := recordRealizations(ctx, conn, drvPath, outPaths); err != nil {
		return nil, fmt.Errorf("build %s: %v", drvPath, err)
	}

	resp := new(zbstore.RealizeResponse)
	for _, outputName := range sortedKeys(outPaths) {
		resp.Outputs = append(resp.Outputs, &zbstore.RealizeOutput{
			Name: outputName,
			Path: zbstore.NonNull(outPaths[outputName]),
		})
	}
	return marshalResponse(resp)
}

func findExistingRealizations(conn *sqlite.Conn, drvPath zbstore.Path) (*zbstore.RealizeResponse, error) {
	resp := new(zbstore.RealizeResponse)
	err := sqlitex.ExecuteTransientFS(conn, sqlFiles(), "find_realizations.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":drv_path": drvPath,
		},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			p, err := zbstore.ParsePath(stmt.GetText("output_path"))
			if err != nil {
				return err
			}
			resp.Outputs = append(resp.Outputs, &zbstore.RealizeOutput{
				Name: stmt.GetText("output_name"),
				Path: zbstore.NonNull(p),
			})
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("find existing realizations for %s: %v", drvPath, err)
	}
	return resp, nil
}

func validateOutputs(drv *zbstore.Derivation) error {
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

func tempOutputPaths(drvPath zbstore.Path, outputs map[string]*zbstore.DerivationOutput) (map[string]zbstore.Path, *strings.Replacer, error) {
	dir := drvPath.Dir()
	drvName, ok := drvPath.DerivationName()
	if !ok {
		return nil, nil, fmt.Errorf("compute output paths for %s: not a derivation", drvPath)
	}

	paths := make(map[string]zbstore.Path)
	var rewrites []string
	for outName, outType := range outputs {
		if !outType.IsFloating() {
			p, ok := outType.Path(dir, drvName, outName)
			if !ok {
				return nil, nil, fmt.Errorf("compute output path for %s!%s: unhandled output type", drvPath, outName)
			}
			paths[outName] = p
			continue
		}

		tp, err := tempPath(drvPath, outName)
		if err != nil {
			return nil, nil, err
		}
		placeholder := zbstore.HashPlaceholder(outName)
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
	for i := 0; i < inputs.Len(); i++ {
		inputDigests = append(inputDigests, inputs.At(i).Digest())
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
	for i := 0; i < digestsFound.Len(); i++ {
		digest := digestsFound.At(i)
		// Since all store paths have the same prefix followed by digest,
		// we can use binary search on a sorted set of store paths to find the corresponding digest.
		j, ok := sort.Find(inputs.Len(), func(j int) int {
			return strings.Compare(digest, inputs.At(j).Digest())
		})
		if !ok {
			return nil, fmt.Errorf("scan internal error: could not find digest %q in inputs", digest)
		}
		refs.Others.Add(inputs.At(j))
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

type writeCounter int64

func (wc *writeCounter) Write(p []byte) (n int, err error) {
	*wc += writeCounter(len(p))
	return len(p), nil
}

func (wc *writeCounter) WriteString(s string) (n int, err error) {
	*wc += writeCounter(len(s))
	return len(s), nil
}
