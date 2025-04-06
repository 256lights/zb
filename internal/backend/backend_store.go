// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package backend

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
	"zombiezen.com/go/xcontext"
)

/*
This file contains querying and manipulating functions
for the store directory and the store database.
*/

// readDerivationClosure reads the given derivations from the store
// and the transitive closure of derivations those derivations depend on.
func (s *Server) readDerivationClosure(ctx context.Context, drvPaths []zbstore.Path) (map[zbstore.Path]*zbstore.Derivation, error) {
	stack := slices.Clone(drvPaths)
	result := make(map[zbstore.Path]*zbstore.Derivation)
	for len(stack) > 0 {
		curr := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if result[curr] != nil {
			continue
		}
		drv, err := s.readDerivation(ctx, curr)
		if err != nil {
			return nil, err
		}
		result[curr] = drv
		for inputDrvPath := range drv.InputDerivations {
			stack = append(stack, inputDrvPath)
		}
	}

	// Walk through closure to ensure that every named output exists.
	for drvPath, drv := range result {
		for ref := range drv.InputDerivationOutputs() {
			if _, ok := result[ref.DrvPath].Outputs[ref.OutputName]; !ok {
				return result, fmt.Errorf("derivation %s depends on non-existent output %v", drvPath, ref)
			}
		}
	}

	return result, nil
}

// readDerivation reads a derivation file from the store
// and validates that it fits the constraints that this backend imposes on derivations.
// As a side effect, if readDerivation succeeds,
// callers can assume that all inputs are present in the store without acquiring the writing lock.
func (s *Server) readDerivation(ctx context.Context, drvPath zbstore.Path) (*zbstore.Derivation, error) {
	drvName, isDrv := drvPath.DerivationName()
	if !isDrv {
		return nil, fmt.Errorf("read derivation %s: not a %s file", drvPath, zbstore.DerivationExt)
	}
	log.Debugf(ctx, "Waiting for lock on %s to read derivation...", drvPath)
	unlock, err := s.writing.lock(ctx, drvPath)
	if err != nil {
		return nil, fmt.Errorf("read derivation %s: waiting for lock: %w", drvPath, err)
	}
	defer unlock()
	log.Debugf(ctx, "Reading derivation %s (lock acquired)", drvPath)
	realDrvPath := s.realPath(drvPath)
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

func findPossibleRealizations(ctx context.Context, conn *sqlite.Conn, eqClass equivalenceClass) (presentInStore, absentFromStore sets.Set[zbstore.Path], err error) {
	drvHash := eqClass.drvHashKey.toHash()
	presentInStore = make(sets.Set[zbstore.Path])
	absentFromStore = make(sets.Set[zbstore.Path])
	err = sqlitex.ExecuteTransientFS(conn, sqlFiles(), "find_realizations.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":drv_hash_algorithm": drvHash.Type().String(),
			":drv_hash_bits":      drvHash.Bytes(nil),
			":output_name":        eqClass.outputName.Value(),
		},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			rawPath := stmt.GetText("output_path")
			outPath, err := zbstore.ParsePath(rawPath)
			if err != nil {
				log.Warnf(ctx, "Database contains realization with invalid path %q for %v (%v)", rawPath, eqClass, err)
				return nil
			}
			if stmt.GetBool("present_in_store") {
				presentInStore.Add(outPath)
			} else {
				absentFromStore.Add(outPath)
			}
			return nil
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("find existing realizations for %v: %v", eqClass, err)
	}
	return presentInStore, absentFromStore, nil
}

type realizationOutput struct {
	path       zbstore.Path
	references map[zbstore.Path]sets.Set[equivalenceClass]
}

func recordRealizations(ctx context.Context, conn *sqlite.Conn, drvHash nix.Hash, outputs map[string]realizationOutput) (err error) {
	if log.IsEnabled(log.Debug) {
		outputPaths := make(map[string]zbstore.Path)
		for outputName, out := range outputs {
			outputPaths[outputName] = out.path
		}
		log.Debugf(ctx, "Recording realizations for %v: %s", drvHash, formatOutputPaths(outputPaths))
	}

	defer sqlitex.Save(conn)(&err)

	if err := upsertDrvHash(conn, drvHash); err != nil {
		return fmt.Errorf("record realizations for %v: %v", drvHash, err)
	}
	for _, output := range outputs {
		if err := upsertPath(conn, output.path); err != nil {
			return fmt.Errorf("record realizations for %v: %v", drvHash, err)
		}
		for path, eqClasses := range output.references {
			if err := upsertPath(conn, path); err != nil {
				return fmt.Errorf("record realizations for %v: %v", drvHash, err)
			}
			for eqClass := range eqClasses.All() {
				h := eqClass.drvHashKey.toHash()
				if err := upsertDrvHash(conn, h); err != nil {
					return fmt.Errorf("record realizations for %v: %v", drvHash, err)
				}
			}
		}
	}

	realizationStmt, err := sqlitex.PrepareTransientFS(conn, sqlFiles(), "insert_realization.sql")
	if err != nil {
		return fmt.Errorf("record realizations for %s: %v", drvHash, err)
	}
	defer realizationStmt.Finalize()
	refClassStmt, err := sqlitex.PrepareTransientFS(conn, sqlFiles(), "insert_reference_class.sql")
	if err != nil {
		return fmt.Errorf("record realizations for %s: %v", drvHash, err)
	}
	defer refClassStmt.Finalize()

	realizationStmt.SetText(":drv_hash_algorithm", drvHash.Type().String())
	realizationStmt.SetBytes(":drv_hash_bits", drvHash.Bytes(nil))
	refClassStmt.SetText(":referrer_drv_hash_algorithm", drvHash.Type().String())
	refClassStmt.SetBytes(":referrer_drv_hash_bits", drvHash.Bytes(nil))
	for outputName, output := range outputs {
		realizationStmt.SetText(":output_name", outputName)
		realizationStmt.SetText(":output_path", string(output.path))
		if _, err := realizationStmt.Step(); err != nil {
			return fmt.Errorf("record realizations for %s: output %s: %v", drvHash, outputName, err)
		}
		if err := realizationStmt.Reset(); err != nil {
			return fmt.Errorf("record realizations for %s: output %s: %v", drvHash, outputName, err)
		}

		refClassStmt.SetText(":referrer_path", string(output.path))
		refClassStmt.SetText(":referrer_output_name", outputName)
		for path, eqClasses := range output.references {
			refClassStmt.SetText(":reference_path", string(path))
			for eqClass := range eqClasses.All() {
				if eqClass.isZero() {
					refClassStmt.SetNull(":reference_drv_hash_algorithm")
					refClassStmt.SetNull(":reference_drv_hash_bits")
					refClassStmt.SetNull(":reference_output_name")
				} else {
					h := eqClass.drvHashKey.toHash()
					refClassStmt.SetText(":reference_drv_hash_algorithm", h.Type().String())
					refClassStmt.SetBytes(":reference_drv_hash_bits", h.Bytes(nil))
					refClassStmt.SetText(":reference_output_name", eqClass.outputName.Value())
				}

				if _, err := refClassStmt.Step(); err != nil {
					return fmt.Errorf("record realizations for %s: output %s: reference %s: %v", drvHash, outputName, path, err)
				}
				if err := refClassStmt.Reset(); err != nil {
					return fmt.Errorf("record realizations for %s: output %s: reference %s: %v", drvHash, outputName, path, err)
				}
			}
		}
	}

	return nil
}

// pathInfo returns basic information about an object in the store.
func pathInfo(conn *sqlite.Conn, path zbstore.Path) (_ *zbstorerpc.ObjectInfo, err error) {
	defer sqlitex.Save(conn)(&err)

	var info *zbstorerpc.ObjectInfo
	err = sqlitex.ExecuteTransientFS(conn, sqlFiles(), "info.sql", &sqlitex.ExecOptions{
		Named: map[string]any{":path": string(path)},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			info = new(zbstorerpc.ObjectInfo)
			info.NARSize = stmt.GetInt64("nar_size")
			var err error
			info.NARHash, err = nix.ParseHash(stmt.GetText("nar_hash"))
			if err != nil {
				return err
			}
			info.CA, err = nix.ParseContentAddress(stmt.GetText("ca"))
			if err != nil {
				return err
			}
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("path info for %s: %v", path, err)
	}
	if info == nil {
		return nil, fmt.Errorf("path info for %s: %w", path, errObjectNotExist)
	}

	err = sqlitex.ExecuteTransientFS(conn, sqlFiles(), "references.sql", &sqlitex.ExecOptions{
		Named: map[string]any{":path": string(path)},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			ref, err := zbstore.ParsePath(stmt.GetText("path"))
			if err != nil {
				return err
			}
			info.References = append(info.References, ref)
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("path info for %s: references: %v", path, err)
	}

	return info, nil
}

var errObjectNotExist = errors.New("object not in store")

// closurePaths finds all store paths that the given path transitively refers to
// and calls the yield function with each path,
// including the original path itself.
// If an equivalence class is given,
// then any given path may have zero or more non-zero equivalence classes associated with it,
// indicating which equivalence class produced the path
// during evaluation of the given equivalence class.
// If closurePaths does not return an error,
// closurePaths is guaranteed to have called yield at least once.
//
// closurePaths uses information from both the references table and the reference classes table.
// closurePaths may return an incomplete closure for paths that don't exist on the disk.
func closurePaths(conn *sqlite.Conn, pe pathAndEquivalenceClass, yield func(pathAndEquivalenceClass) bool) error {
	errStop := errors.New("stop iteration")

	args := map[string]any{
		":path":               string(pe.path),
		":drv_hash_algorithm": nil,
		":drv_hash_bits":      nil,
		":output_name":        nil,
	}
	if !pe.equivalenceClass.isZero() {
		h := pe.equivalenceClass.drvHashKey.toHash()
		args[":drv_hash_algorithm"] = h.Type().String()
		args[":drv_hash_bits"] = h.Bytes(nil)
		args[":output_name"] = pe.equivalenceClass.outputName.Value()
	}

	dir := pe.path.Dir()
	calledYield := false
	err := sqlitex.ExecuteTransientFS(conn, sqlFiles(), "closure.sql", &sqlitex.ExecOptions{
		Named: args,
		ResultFunc: func(stmt *sqlite.Stmt) error {
			rawPath := stmt.GetText("path")
			var row pathAndEquivalenceClass
			var sub string
			var err error
			row.path, sub, err = dir.ParsePath(rawPath)
			if err != nil {
				return fmt.Errorf("path: %v", err)
			}
			if sub != "" {
				return fmt.Errorf("path %s: must not contain a sub-path", rawPath)
			}
			if hashTypeName := stmt.GetText("drv_hash_algorithm"); hashTypeName != "" {
				ht, err := nix.ParseHashType(hashTypeName)
				if err != nil {
					return fmt.Errorf("path %s: derivation hash: %v", row.path, err)
				}
				bitsLength := stmt.GetLen("drv_hash_bits")
				if bitsLength != ht.Size() {
					return fmt.Errorf("path %s: derivation hash: incorrect size for %v (found %d instead of %d)",
						row.path, ht, bitsLength, ht.Size())
				}
				bits := make([]byte, bitsLength)
				stmt.GetBytes("drv_hash_bits", bits)
				outputName := stmt.GetText("output_name")
				if outputName != "" && !zbstore.IsValidOutputName(outputName) {
					return fmt.Errorf("path %s: output name %q is not valid", row.path, outputName)
				}
				row.equivalenceClass = newEquivalenceClass(nix.NewHash(ht, bits), outputName)
			}
			calledYield = true
			if !yield(row) {
				return errStop
			}
			return nil
		},
	})
	if err != nil && !errors.Is(err, errStop) {
		return fmt.Errorf("find closure of %s: %v", pe.path, err)
	}
	if !calledYield {
		return fmt.Errorf("find closure of %s: %w", pe.path, errObjectNotExist)
	}
	return nil
}

// objectExists checks for the existence of a store object in the store database.
func objectExists(conn *sqlite.Conn, path zbstore.Path) (bool, error) {
	var exists bool
	err := sqlitex.ExecuteFS(conn, sqlFiles(), "object_exists.sql", &sqlitex.ExecOptions{
		Named: map[string]any{":path": string(path)},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			exists = stmt.ColumnBool(0)
			return nil
		},
	})
	if err != nil {
		return false, fmt.Errorf("check existence of %s: %v", path, err)
	}
	return exists, nil
}

func insertObject(ctx context.Context, conn *sqlite.Conn, info *zbstore.NARInfo) (err error) {
	log.Debugf(ctx, "Registering metadata for %s", info.StorePath)

	defer sqlitex.Save(conn)(&err)

	if err := upsertPath(conn, zbstore.Path(info.StorePath)); err != nil {
		return fmt.Errorf("insert %s into database: %v", info.StorePath, err)
	}
	if err := upsertPath(conn, zbstore.Path(info.Deriver)); err != nil {
		return fmt.Errorf("insert %s into database: %v", info.StorePath, err)
	}
	err = sqlitex.ExecuteTransientFS(conn, sqlFiles(), "insert_object.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":path":     string(info.StorePath),
			":nar_size": info.NARSize,
			":nar_hash": info.NARHash.SRI(),
			":ca":       info.CA.String(),
		},
	})
	if sqlite.ErrCode(err) == sqlite.ResultConstraintRowID {
		return fmt.Errorf("insert %s into database: store object exists", info.StorePath)
	}
	if err != nil {
		return fmt.Errorf("insert %s into database: %v", info.StorePath, err)
	}

	addRefStmt, err := sqlitex.PrepareTransientFS(conn, sqlFiles(), "add_reference.sql")
	if err != nil {
		return fmt.Errorf("insert %s into database: %v", info.StorePath, err)
	}
	defer addRefStmt.Finalize()

	addRefStmt.SetText(":referrer", string(info.StorePath))
	for _, ref := range info.References.All() {
		if err := upsertPath(conn, ref); err != nil {
			return fmt.Errorf("insert %s into database: %v", info.StorePath, err)
		}
		addRefStmt.SetText(":reference", string(ref))
		if _, err := addRefStmt.Step(); err != nil {
			return fmt.Errorf("insert %s into database: add reference %s: %v", info.StorePath, ref, err)
		}
		if err := addRefStmt.Reset(); err != nil {
			return fmt.Errorf("insert %s into database: add reference %s: %v", info.StorePath, ref, err)
		}
	}

	return nil
}

func upsertPath(conn *sqlite.Conn, path zbstore.Path) error {
	if path == "" {
		return nil
	}
	err := sqlitex.ExecuteFS(conn, sqlFiles(), "upsert_path.sql", &sqlitex.ExecOptions{
		Named: map[string]any{":path": string(path)},
	})
	if err != nil {
		return fmt.Errorf("upsert path %s: %v", path, err)
	}
	return nil
}

func upsertDrvHash(conn *sqlite.Conn, h nix.Hash) error {
	if h.IsZero() {
		return nil
	}
	err := sqlitex.ExecuteFS(conn, sqlFiles(), "upsert_drv_hash.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":algorithm": h.Type().String(),
			":bits":      h.Bytes(nil),
		},
	})
	if err != nil {
		return fmt.Errorf("upsert derivation hash %v: %v", h, err)
	}
	return nil
}

func recordBuildStart(conn *sqlite.Conn, buildID uuid.UUID) error {
	now := time.Now()
	err := sqlitex.ExecuteTransientFS(conn, sqlFiles(), "build/start.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":build_id":         buildID.String(),
			":timestamp_millis": now.UnixMilli(),
		},
	})
	if err != nil {
		return fmt.Errorf("create new build record for %s: %v", buildID, err)
	}
	return nil
}

func recordBuildEnd(conn *sqlite.Conn, buildID uuid.UUID, buildError error) error {
	now := time.Now()
	var buildErrorArg any = nil
	if buildError != nil && !errors.Is(buildError, errUnfinishedRealization) {
		buildErrorArg = buildError.Error()
	}
	err := sqlitex.ExecuteTransientFS(conn, sqlFiles(), "build/end.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":build_id":         buildID.String(),
			":build_error":      buildErrorArg,
			":timestamp_millis": now.UnixMilli(),
		},
	})
	if err != nil {
		return fmt.Errorf("record build end for %s: %v", buildID, err)
	}
	return nil
}

// deleteOldBuilds deletes builds that occurred before the time cutoff.
// Any build IDs yielded by the keep sequence will be retained.
func deleteOldBuilds(conn *sqlite.Conn, cutoff time.Time, keep iter.Seq[uuid.UUID]) (numDeleted int64, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("delete old builds: %v", err)
		}
	}()
	defer sqlitex.Save(conn)(&err)

	err = sqlitex.ExecuteTransient(conn, `create temp table "active_builds" ("uuid" blob not null unique);`, nil)
	if err != nil {
		return 0, err
	}
	stmt, _, err := conn.PrepareTransient(`insert into temp."active_builds" values (?);`)
	if err != nil {
		return 0, err
	}
	defer stmt.Finalize()
	for id := range keep {
		stmt.BindBytes(1, id[:])
		var stmtErrors [2]error
		_, stmtErrors[0] = stmt.Step()
		stmtErrors[1] = stmt.Reset()
		for _, err := range stmtErrors {
			if err != nil {
				return 0, err
			}
		}
	}

	err = sqlitex.ExecuteTransientFS(conn, sqlFiles(), "build/gc.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":cutoff_millis": cutoff.UnixMilli(),
		},
	})
	if err != nil {
		return 0, err
	}
	err = sqlitex.ExecuteTransient(conn, `drop table temp."active_builds";`, nil)
	if err != nil {
		return 0, err
	}
	return int64(conn.Changes()), nil
}

func recordExpandResult(conn *sqlite.Conn, buildID uuid.UUID, result *zbstorerpc.ExpandResult) error {
	argsJSON := "[]"
	if len(result.Args) > 0 {
		var err error
		argsJSON, err = marshalJSONString(result.Args)
		if err != nil {
			return fmt.Errorf("record build end for %s: %v", buildID, err)
		}
	}
	envJSON := "{}"
	if len(result.Env) > 0 {
		var err error
		envJSON, err = marshalJSONString(result.Env)
		if err != nil {
			return fmt.Errorf("record build end for %s: %v", buildID, err)
		}
	}
	err := sqlitex.ExecuteTransientFS(conn, sqlFiles(), "build/set_extract.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":build_id": buildID.String(),
			":builder":  result.Builder,
			":args":     argsJSON,
			":env":      envJSON,
		},
	})
	if err != nil {
		return fmt.Errorf("record build end for %s: %v", buildID, err)
	}
	return nil
}

func insertBuildResult(conn *sqlite.Conn, buildID uuid.UUID, drvPath zbstore.Path, t time.Time) (buildResultID int64, err error) {
	defer sqlitex.Save(conn)(&err)
	if err := upsertPath(conn, drvPath); err != nil {
		return -1, fmt.Errorf("record build result for %s in %v: %v", drvPath, buildID, err)
	}
	err = sqlitex.ExecuteTransientFS(conn, sqlFiles(), "build/insert_result.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":build_id":         buildID.String(),
			":drv_path":         string(drvPath),
			":timestamp_millis": t.UnixMilli(),
		},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			buildResultID = stmt.ColumnInt64(0)
			return nil
		},
	})
	if err != nil {
		return -1, fmt.Errorf("record build result for %s in %v: %v", drvPath, buildID, err)
	}
	return buildResultID, nil
}

// findBuildResults appends the build results in the build with the given ID to dst.
// If drvPath is not empty, then only the result for drvPath is appended (if it exists).
func findBuildResults(dst []*zbstorerpc.BuildResult, conn *sqlite.Conn, buildID uuid.UUID, drvPath zbstore.Path) ([]*zbstorerpc.BuildResult, error) {
	initDstLen := len(dst)
	err := sqlitex.ExecuteTransientFS(conn, sqlFiles(), "build/results.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":build_id": buildID.String(),
			":drv_path": string(drvPath),
		},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			drvPath, err := zbstore.ParsePath(stmt.GetText("drv_path"))
			if err != nil {
				return err
			}
			var curr *zbstorerpc.BuildResult
			if len(dst) > initDstLen && dst[len(dst)-1].DrvPath == drvPath {
				curr = dst[len(dst)-1]
			} else {
				curr = &zbstorerpc.BuildResult{
					DrvPath: drvPath,
					Status:  zbstorerpc.BuildStatus(stmt.GetText("status")),
					Outputs: []*zbstorerpc.RealizeOutput{},
					LogSize: stmt.GetInt64("log_size"),
				}
				dst = append(dst, curr)
			}

			if outputName := stmt.GetText("output_name"); outputName != "" {
				newOutput := &zbstorerpc.RealizeOutput{
					Name: outputName,
				}
				if s := stmt.GetText("output_path"); s != "" {
					p, err := zbstore.ParsePath(s)
					if err != nil {
						return fmt.Errorf("output %s: %v", outputName, err)
					}
					newOutput.Path = zbstorerpc.NonNull(p)
				}
				curr.Outputs = append(curr.Outputs, newOutput)
			}

			return nil
		},
	})
	if err != nil {
		if drvPath == "" {
			return dst, fmt.Errorf("list build results for %v: %v", buildID, err)
		}
		return dst, fmt.Errorf("fetch build result for %s in build %v: %v", drvPath, buildID, err)
	}
	return dst, nil
}

func recordBuilderStart(conn *sqlite.Conn, buildResultID int64, t time.Time) error {
	err := sqlitex.ExecuteTransientFS(conn, sqlFiles(), "build/set_builder_start.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":id":               buildResultID,
			":timestamp_millis": t.UnixMilli(),
		},
	})
	if err != nil {
		return fmt.Errorf("record builder start: %v", err)
	}
	return nil
}

// setBuildResultOutputs sets the outputs for the build result with the given ID.
// If a path is empty, then the output's path will be null.
func setBuildResultOutputs(conn *sqlite.Conn, buildResultID int64, outputs iter.Seq2[string, zbstore.Path]) (err error) {
	defer sqlitex.Save(conn)(&err)

	err = sqlitex.ExecuteTransientFS(conn, sqlFiles(), "build/clear_outputs.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":id": buildResultID,
		},
	})
	if err != nil {
		return fmt.Errorf("record build result outputs: %v", err)
	}

	stmt, err := sqlitex.PrepareTransientFS(conn, sqlFiles(), "build/insert_output.sql")
	if err != nil {
		return fmt.Errorf("record build result outputs: %v", err)
	}
	defer stmt.Finalize()

	stmt.SetInt64(":id", buildResultID)
	for outputName, outputPath := range outputs {
		if outputPath != "" {
			if err := upsertPath(conn, outputPath); err != nil {
				return fmt.Errorf("record build result %s -> %s: %v", outputName, outputPath, err)
			}
		}

		stmt.SetText(":output_name", outputName)
		stmt.SetText(":output_path", string(outputPath))
		var execErrors [2]error
		_, execErrors[0] = stmt.Step()
		execErrors[1] = stmt.Reset()
		for _, err := range execErrors {
			if err != nil {
				return fmt.Errorf("record build result %s -> %s: %v", outputName, outputPath, err)
			}
		}
	}

	return nil
}

func recordBuilderEnd(conn *sqlite.Conn, buildResultID int64, t time.Time) error {
	err := sqlitex.ExecuteTransientFS(conn, sqlFiles(), "build/set_builder_end.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":id":               buildResultID,
			":timestamp_millis": t.UnixMilli(),
		},
	})
	if err != nil {
		return fmt.Errorf("record builder end: %v", err)
	}
	return nil
}

func finalizeBuildResult(ctx context.Context, conn *sqlite.Conn, buildResultID int64, t time.Time, buildError error) (err error) {
	// If build is being cancelled, allow some amount of time to write.
	ctx, cancel := xcontext.KeepAlive(ctx, 30*time.Second)
	defer cancel()
	oldDone := conn.SetInterrupt(ctx.Done())
	defer conn.SetInterrupt(oldDone)

	defer sqlitex.Save(conn)(&err)

	status := zbstorerpc.BuildSuccess
	if buildError != nil {
		if isBuilderFailure(buildError) {
			status = zbstorerpc.BuildFail
		} else {
			status = zbstorerpc.BuildError
			logger := newBuildLogger(ctx, conn, buildResultID)
			var buf []byte
			buf = append(buf, "zb internal error: "...)
			buf = append(buf, buildError.Error()...)
			buf = append(buf, '\n')
			if _, err := logger.Write(buf); err != nil {
				log.Warnf(ctx, "Failed to write build error to log: %v", err)
			}
			if err := logger.Close(); err != nil {
				log.Debugf(ctx, "Failed to write close build logger: %v", err)
			}
		}
	}
	err = sqlitex.ExecuteTransientFS(conn, sqlFiles(), "build/end_result.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":id":               buildResultID,
			":status":           string(status),
			":timestamp_millis": t.UnixMilli(),
		},
	})
	if err != nil {
		return fmt.Errorf("record final build result: %v", err)
	}
	return nil
}

var (
	errBuildLogNotFound = errors.New("not found")
	errBuildLogPending  = errors.New("log not finished")
)

// readBuildLogAt reads len(p) bytes into p starting at offset off
// in the builder log for the derivation path drvPath from build with ID buildID.
// It returns the number of bytes read (0 <= n <= len(p))
// and any error encountered.
// When readBuildLogAt returns n < len(p),
// it returns a non-nil error explaining why more bytes were not returned.
//
// If the n < len(p) bytes returned by readBuildLogAt are at the end of the build log
// and the builder has not finished,
// readBuildLogAt will return an error that wraps [errBuildLogPending].
// readBuildLogAt will not block until more data is written.
// If the n = len(p) bytes returned by readBuildLogAt are at the end of the build log
// and the builder has already finished,
// readBuildLogAt returns err == [io.EOF].
//
// If the build log does not exist in the database,
// then readBuildLogAt will return an error that wraps [errBuildLogNotFound].
func readBuildLogAt(conn *sqlite.Conn, buildID uuid.UUID, drvPath zbstore.Path, p []byte, off int64) (n int, err error) {
	defer sqlitex.Save(conn)(&err)

	var buildResultID int64
	found := false
	finished := false
	err = sqlitex.ExecuteTransientFS(conn, sqlFiles(), "build/find_result.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":build_id": buildID.String(),
			":drv_path": drvPath,
		},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			found = true
			buildResultID = stmt.GetInt64("id")
			status := zbstorerpc.BuildStatus(stmt.GetText("status"))
			finished = status != zbstorerpc.BuildActive
			return nil
		},
	})
	if err != nil {
		return 0, fmt.Errorf("read log for %s in build %v: %v", drvPath, buildID, err)
	}
	if !found {
		return 0, fmt.Errorf("read log for %s in build %v: %w", drvPath, buildID, errBuildLogNotFound)
	}
	end := off + int64(len(p))
	if finished {
		// Read an additional byte to check for EOF.
		end++
	}
	maxSeen := off
	err = sqlitex.ExecuteTransientFS(conn, sqlFiles(), "build/read_log.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":build_result_id": buildResultID,
			":start":           off,
			":end":             end,
		},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			start := stmt.GetInt64("start")
			i := stmt.ColumnIndex("data")
			stmt.ColumnBytes(i, p[start-off:])
			maxSeen = max(maxSeen, start+int64(stmt.ColumnLen(i)))
			return nil
		},
	})
	if err != nil {
		return int(maxSeen - off), fmt.Errorf("read log for %s in build %v: %v", drvPath, buildID, err)
	}
	switch want := off + int64(len(p)); {
	case finished && maxSeen <= want:
		return int(maxSeen - off), io.EOF
	case !finished && maxSeen < want:
		return int(maxSeen - off), errBuildLogPending
	default:
		return len(p), nil
	}
}

// buildLogger is an [io.Writer] that inserts log chunks into the store database.
// It has no buffering: callers should introduce buffering as needed.
type buildLogger struct {
	ctx  context.Context
	conn *sqlite.Conn
	stmt *sqlite.Stmt
	err  error
}

// newBuildLogger returns a logger that appends to the log for the given build result.
// The caller is responsible for calling [*buildLogger.Close] when it is done writing to the logger.
func newBuildLogger(ctx context.Context, conn *sqlite.Conn, buildResultID int64) *buildLogger {
	stmt, err := sqlitex.PrepareTransientFS(conn, sqlFiles(), "build/insert_log_chunk.sql")
	if err != nil {
		return &buildLogger{err: err}
	}
	stmt.SetInt64(":build_result_id", buildResultID)
	return &buildLogger{
		ctx:  ctx,
		conn: conn,
		stmt: stmt,
	}
}

// Write writes the bytes as a new log chunk in the log.
func (logger *buildLogger) Write(p []byte) (n int, err error) {
	now := time.Now()
	if logger.err != nil {
		return 0, logger.err
	}
	if len(p) == 0 {
		return 0, nil
	}

	// Avoid interrupting writes if the overall build is being cancelled.
	ctx, cancel := xcontext.KeepAlive(logger.ctx, 30*time.Second)
	defer cancel()
	oldDone := logger.conn.SetInterrupt(ctx.Done())
	defer logger.conn.SetInterrupt(oldDone)

	logger.stmt.SetInt64(":received_at", now.UnixMilli())
	logger.stmt.SetBytes(":data", p)
	var stmtErrors [2]error
	_, stmtErrors[0] = logger.stmt.Step()
	stmtErrors[1] = logger.stmt.Reset()
	for _, err := range stmtErrors {
		if err != nil {
			logger.err = fmt.Errorf("write to build log: %v", err)
			return 0, logger.err
		}
	}
	return len(p), nil
}

// Close releases the resources associated with the logger.
func (logger *buildLogger) Close() error {
	logger.err = errors.New("logger closed")
	var err error
	if logger.stmt != nil {
		err = logger.stmt.Finalize()
		logger.stmt = nil
	}
	logger.conn = nil
	logger.ctx = nil
	return err
}

func prepareConn(conn *sqlite.Conn) error {
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA journal_mode = wal;", nil); err != nil {
		return err
	}
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA foreign_keys = on;", nil); err != nil {
		return err
	}
	// uuid(TEXT) -> BLOB | NULL
	// Parse UUID, returning NULL if it does not represent a valid UUID.
	err := conn.CreateFunction("uuid", &sqlite.FunctionImpl{
		NArgs:         1,
		Deterministic: true,
		AllowIndirect: true,
		Scalar: func(ctx sqlite.Context, args []sqlite.Value) (sqlite.Value, error) {
			u, err := uuid.Parse(args[0].Text())
			if err != nil {
				return sqlite.Value{}, nil
			}
			return sqlite.BlobValue(u[:]), nil
		},
	})
	if err != nil {
		return err
	}
	// uuidhex(any) -> TEXT | NULL
	// Format UUID in canonical dash-separated lower hex format.
	// If argument is not a BLOB, it is converted to TEXT and parsing is attempted.
	// If parsing fails or the argument is a BLOB with a length other than 16,
	// then uuidhex returns NULL.
	err = conn.CreateFunction("uuidhex", &sqlite.FunctionImpl{
		NArgs:         1,
		Deterministic: true,
		AllowIndirect: true,
		Scalar: func(ctx sqlite.Context, args []sqlite.Value) (sqlite.Value, error) {
			var u uuid.UUID
			switch args[0].Type() {
			case sqlite.TypeBlob:
				b := args[0].Blob()
				if len(b) != len(u) {
					return sqlite.Value{}, nil
				}
				copy(u[:], b)
			default:
				var err error
				u, err = uuid.Parse(args[0].Text())
				if err != nil {
					return sqlite.Value{}, nil
				}
			}
			return sqlite.TextValue(u.String()), nil
		},
	})
	if err != nil {
		return err
	}
	return nil
}

//go:embed sql/*.sql
//go:embed sql/build/*.sql
//go:embed sql/schema/*.sql
var rawSQLFiles embed.FS

func sqlFiles() fs.FS {
	sub, err := fs.Sub(rawSQLFiles, "sql")
	if err != nil {
		panic(err)
	}
	return sub
}

var schemaState struct {
	init   sync.Once
	schema sqlitemigration.Schema
	err    error
}

func loadSchema() sqlitemigration.Schema {
	schemaState.init.Do(func() {
		for i := 1; ; i++ {
			migration, err := fs.ReadFile(sqlFiles(), fmt.Sprintf("schema/%02d.sql", i))
			if errors.Is(err, fs.ErrNotExist) {
				break
			}
			if err != nil {
				schemaState.err = err
				return
			}
			schemaState.schema.Migrations = append(schemaState.schema.Migrations, string(migration))
		}
	})

	if schemaState.err != nil {
		panic(schemaState.err)
	}
	return schemaState.schema
}

func marshalJSONString(v any) (string, error) {
	sb := new(strings.Builder)
	enc := json.NewEncoder(sb)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "")
	err := enc.Encode(v)
	return strings.TrimSuffix(sb.String(), "\n"), err
}

func unmarshalJSONString(data string, v any) error {
	dec := json.NewDecoder(strings.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return err
	}
	var buf [1]byte
	n, _ := io.ReadFull(dec.Buffered(), buf[:])
	if n > 0 {
		return errors.New("unmarshal json: trailing data")
	}
	return nil
}
