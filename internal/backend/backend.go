// Copyright 2024 Roxy Light
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
	"os"
	"sync"

	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
	"zombiezen.com/go/zb/internal/jsonrpc"
	"zombiezen.com/go/zb/sortedset"
	"zombiezen.com/go/zb/zbstore"
)

type Options struct {
	Dir    zbstore.Directory
	DBPath string
}

type Server struct {
	dir zbstore.Directory
	db  *sqlitemigration.Pool
}

func NewServer(opts *Options) *Server {
	return &Server{
		dir: opts.Dir,
		db: sqlitemigration.NewPool(opts.DBPath, loadSchema(), sqlitemigration.Options{
			Flags:       sqlite.OpenCreate | sqlite.OpenReadWrite,
			PrepareConn: prepareConn,
			OnStartMigrate: func() {
				ctx := context.Background()
				log.Debugf(ctx, "Migrating...")
			},
			OnReady: func() {
				ctx := context.Background()
				log.Debugf(ctx, "Database ready")
			},
			OnError: func(err error) {
				ctx := context.Background()
				log.Errorf(ctx, "Migration: %v", err)
			},
		}),
	}
}

func (s *Server) Close() error {
	return s.db.Close()
}

func (s *Server) JSONRPC(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	return jsonrpc.ServeMux{
		zbstore.ExistsMethod:  jsonrpc.HandlerFunc(s.exists),
		zbstore.RealizeMethod: jsonrpc.HandlerFunc(s.realize),
	}.JSONRPC(ctx, req)
}

func (s *Server) exists(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	var args zbstore.ExistsRequest
	if err := json.Unmarshal(req.Params, &args); err != nil {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
	}
	p, sub, err := s.dir.ParsePath(args.Path)
	if err != nil {
		log.Debugf(ctx, "Queried invalid path %s", args.Path)
		return &jsonrpc.Response{
			Result: json.RawMessage("false"),
		}, nil
	}
	if _, err := os.Lstat(p.Join(sub)); err != nil {
		log.Debugf(ctx, "%s does not exist (%v)", args.Path, err)
		return &jsonrpc.Response{
			Result: json.RawMessage("false"),
		}, nil
	}
	log.Debugf(ctx, "%s exists", args.Path)
	return &jsonrpc.Response{
		Result: json.RawMessage("true"),
	}, nil
}

func (s *Server) realize(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
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

	if info, err := os.Lstat(string(drvPath)); err != nil {
		return nil, err
	} else if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", drvPath)
	}
	drvData, err := os.ReadFile(string(drvPath))
	if err != nil {
		return nil, err
	}
	drv, err := zbstore.ParseDerivation(s.dir, drvName, drvData)
	if err != nil {
		return nil, err
	}

	log.Infof(ctx, "Requested to build %s: %s %s", drvPath, drv.Builder, drv.Args)

	return nil, fmt.Errorf("TODO(soon)")
}

type NARReceiver struct {
	dir     zbstore.Directory
	dbPool  *sqlitemigration.Pool
	tmpFile *os.File

	hasher nix.Hasher
	size   int64
}

func (s *Server) NewNARReceiver() *NARReceiver {
	return &NARReceiver{
		dir:    s.dir,
		dbPool: s.db,
		hasher: *nix.NewHasher(nix.SHA256),
	}
}

func (r *NARReceiver) Write(p []byte) (n int, err error) {
	if r.tmpFile == nil {
		r.tmpFile, err = os.CreateTemp("", "zb-serve-receive-*.nar")
		if err != nil {
			return 0, err
		}
	}
	n, err = r.tmpFile.Write(p)
	r.hasher.Write(p[:n])
	r.size += int64(n)
	return n, err
}

func (r *NARReceiver) ReceiveNAR(trailer *zbstore.ExportTrailer) {
	ctx := context.TODO()
	if r.tmpFile == nil {
		// No bytes written? Not a valid NAR.
		return
	}
	if _, err := r.tmpFile.Seek(0, io.SeekStart); err != nil {
		log.Errorf(ctx, "Unable to seek in store temp file: %v", err)
		r.Cleanup(ctx)
		return
	}
	defer func() {
		if err := r.tmpFile.Truncate(0); err != nil {
			log.Warnf(ctx, "Unable to truncate store temp file: %v", err)
			r.Cleanup(ctx)
			return
		}
		if _, err := r.tmpFile.Seek(0, io.SeekStart); err != nil {
			log.Errorf(ctx, "Unable to seek in store temp file: %v", err)
			r.Cleanup(ctx)
			return
		}
		r.hasher.Reset()
		r.size = 0
	}()

	if trailer.StorePath.Dir() != r.dir {
		log.Warnf(ctx, "Rejecting %s (not in %s)", trailer.StorePath, r.dir)
		return
	}
	ca, err := verifyContentAddress(trailer.StorePath, r.tmpFile, &trailer.References, trailer.ContentAddress)
	if err != nil {
		log.Warnf(ctx, "%v", err)
		return
	}
	if _, err := r.tmpFile.Seek(0, io.SeekStart); err != nil {
		log.Errorf(ctx, "Unable to seek in store temp file: %v", err)
		return
	}

	conn, err := r.dbPool.Get(ctx)
	if err != nil {
		log.Warnf(ctx, "Connecting to store database: %v", err)
		return
	}
	defer r.dbPool.Put(conn)

	err = func() (err error) {
		endFn, err := sqlitex.ImmediateTransaction(conn)
		if err != nil {
			return err
		}
		defer endFn(&err)

		return insertObject(conn, &zbstore.NARInfo{
			StorePath:   trailer.StorePath,
			NARSize:     r.size,
			NARHash:     r.hasher.SumHash(),
			Compression: zbstore.NoCompression,
			Deriver:     trailer.Deriver,
			CA:          ca,
		})
	}()
	if errors.Is(err, errObjectExists) {
		log.Debugf(ctx, "Received NAR for %s. Exists in database, skipping...", trailer.StorePath)
		return
	}
	if err != nil {
		log.Errorf(ctx, "Starting import of %s: %v", trailer.StorePath, err)
		return
	}
	log.Debugf(ctx, "Inserted %s into database", trailer.StorePath)

	if err := importNAR(r.tmpFile, trailer); err != nil {
		log.Warnf(ctx, "Import of %s failed: %v", trailer.StorePath, err)
		if err := os.RemoveAll(string(trailer.StorePath)); err != nil {
			log.Errorf(ctx, "Failed to clean up partial import of %s: %v", trailer.StorePath, err)
		}
		return
	}
	log.Infof(ctx, "Imported %s", trailer.StorePath)
}

// verifyContentAddress validates that the content matches the given content address.
// If the content address is the zero value,
// then the content address is computed as a "source" store object.
func verifyContentAddress(path zbstore.Path, narContent io.Reader, refs *sortedset.Set[zbstore.Path], ca nix.ContentAddress) (nix.ContentAddress, error) {
	storeRefs := zbstore.MakeReferences(path, refs)
	if !ca.IsZero() {
		if err := zbstore.ValidateContentAddress(ca, storeRefs); err != nil {
			return nix.ContentAddress{}, fmt.Errorf("verify %s content address: %v", path, err)
		}
	}

	var computed nix.ContentAddress
	switch {
	case ca.IsZero() || zbstore.IsSourceContentAddress(ca) && ca.Hash().Type() == nix.SHA256:
		var digest string
		if storeRefs.Self {
			digest = path.Digest()
		}
		var err error
		computed, err = zbstore.SourceSHA256ContentAddress(digest, narContent)
		if err != nil {
			return nix.ContentAddress{}, fmt.Errorf("verify %s content address: %v", path, err)
		}
	case zbstore.IsSourceContentAddress(ca):
		// Future-proofing in case we add new algorithms but don't update backends.
		return nix.ContentAddress{}, fmt.Errorf("verify %s content address: unsupported source content address %v", path, ca.Hash().Type())
	case ca.IsRecursiveFile():
		h := nix.NewHasher(ca.Hash().Type())
		if _, err := io.Copy(h, narContent); err != nil {
			return nix.ContentAddress{}, fmt.Errorf("verify %s content address: %v", path, err)
		}
		computed = nix.RecursiveFileContentAddress(h.SumHash())
	default:
		nr := nar.NewReader(narContent)
		hdr, err := nr.Next()
		if err != nil {
			return nix.ContentAddress{}, fmt.Errorf("verify %s content address: %v", path, err)
		}
		if !hdr.Mode.IsRegular() {
			return nix.ContentAddress{}, fmt.Errorf("verify %s content address: not a flat file", path)
		}
		h := nix.NewHasher(ca.Hash().Type())
		if _, err := io.Copy(h, nr); err != nil {
			return nix.ContentAddress{}, fmt.Errorf("verify %s content address: %v", path, err)
		}
		if ca.IsText() {
			computed = nix.TextContentAddress(h.SumHash())
		} else {
			computed = nix.FlatFileContentAddress(h.SumHash())
		}
		if _, err := nr.Next(); err == nil {
			return nix.ContentAddress{}, fmt.Errorf("verify %s content address: more than a single file (bug in NAR reader?)", path)
		} else if err != io.EOF {
			return nix.ContentAddress{}, fmt.Errorf("verify %s content address: %v", path, err)
		}
	}

	if !ca.IsZero() && !ca.Equal(computed) {
		return nix.ContentAddress{}, fmt.Errorf("verify %s content address: %v does not match content (computed %v)", path, ca, computed)
	}
	computedPath, err := zbstore.FixedCAOutputPath(path.Dir(), path.Name(), computed, storeRefs)
	if err != nil {
		return nix.ContentAddress{}, fmt.Errorf("verify %s content address: %v", path, err)
	}
	if path != computedPath {
		return nix.ContentAddress{}, fmt.Errorf("verify %s content address: does not match computed path %s", path, computedPath)
	}

	return computed, nil
}

var errObjectExists = errors.New("store object exists")

func insertObject(conn *sqlite.Conn, info *zbstore.NARInfo) (err error) {
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
			":deriver":  string(info.Deriver),
			":ca":       info.CA.String(),
		},
	})
	if sqlite.ErrCode(err) == sqlite.ResultConstraintRowID {
		return fmt.Errorf("insert %s into database: %w", info.StorePath, errObjectExists)
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
	for i := 0; i < info.References.Len(); i++ {
		ref := info.References.At(i)
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

func importNAR(r io.Reader, trailer *zbstore.ExportTrailer) error {
	nr := nar.NewReader(r)
	for {
		hdr, err := nr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		p := trailer.StorePath.Join(hdr.Path)
		switch typ := hdr.Mode.Type(); typ {
		case 0:
			perm := os.FileMode(0o644)
			if hdr.Mode&0o111 != 0 {
				perm = 0o755
			}
			f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, nr)
			err2 := f.Close()
			if err != nil {
				return err
			}
			if err2 != nil {
				return err2
			}
		case fs.ModeDir:
			if err := os.Mkdir(p, 0o755); err != nil {
				return err
			}
		case fs.ModeSymlink:
			if err := os.Symlink(hdr.LinkTarget, p); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unhandled type %v", typ)
		}
	}
}

func (r *NARReceiver) Cleanup(ctx context.Context) {
	if r.tmpFile == nil {
		return
	}
	name := r.tmpFile.Name()
	r.tmpFile.Close()
	r.tmpFile = nil
	if err := os.Remove(name); err != nil {
		log.Warnf(ctx, "Unable to remove store temp file: %v", err)
	}
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