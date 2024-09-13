// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package backend

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"sync"

	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
	"zombiezen.com/go/zb/sets"
	"zombiezen.com/go/zb/zbstore"
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
		return fmt.Errorf("find closure of %s: object not in store", pe.path)
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

func prepareConn(conn *sqlite.Conn) error {
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA journal_mode = wal;", nil); err != nil {
		return err
	}
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA foreign_keys = on;", nil); err != nil {
		return err
	}
	return nil
}

//go:embed sql/*.sql
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
