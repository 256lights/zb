// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

// Package backend provides a [zbstore] implementation backed by local compute resources.
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
	"path/filepath"
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

// Options is the set of optional parameters to [NewServer].
type Options struct {
	// RealDir is where the store objects are located physically on disk.
	// If empty, defaults to the store directory.
	RealDir string
	// BuildDir is where realizations' working directories will be placed.
	// If empty, defaults to [os.TempDir].
	BuildDir string
}

// Server is a local store.
// Server implements [jsonrpc.Handler] and is intended to be used with [jsonrpc.Serve].
type Server struct {
	dir      zbstore.Directory
	realDir  string
	buildDir string
	db       *sqlitemigration.Pool

	inProgress mutexMap[zbstore.Path]
}

// NewServer returns a new [Server] for the given store directory and database path.
// Callers are responsible for calling [Server.Close] on the returned server.
// NewServer will panic if given a store directory that is not native
func NewServer(dir zbstore.Directory, dbPath string, opts *Options) *Server {
	srv := &Server{
		dir:      dir,
		realDir:  opts.RealDir,
		buildDir: opts.BuildDir,

		db: sqlitemigration.NewPool(dbPath, loadSchema(), sqlitemigration.Options{
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
	if srv.realDir == "" {
		srv.realDir = string(srv.dir)
	}
	if srv.buildDir == "" {
		srv.buildDir = os.TempDir()
	}
	return srv
}

// Close releases any resources associated with the server.
func (s *Server) Close() error {
	return s.db.Close()
}

// JSONRPC implements the [jsonrpc.Handler] interface
// and serves the [zbstore] API.
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
	unlock, err := s.inProgress.lock(ctx, p)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err := os.Lstat(filepath.Join(s.realDir, p.Base(), filepath.FromSlash(sub))); err != nil {
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

// NARReceiver is a per-connection [zbstore.NARReceiver].
type NARReceiver struct {
	ctx        context.Context
	dir        zbstore.Directory
	realDir    string
	dbPool     *sqlitemigration.Pool
	inProgress *mutexMap[zbstore.Path]
	tmpFile    *os.File

	hasher nix.Hasher
	size   int64
}

// NewNARReceiver returns a new [NARReceiver] that is attached to the server.
// Callers are responsible for calling [NARReceiver.Cleanup] after the receiver is no longer in use.
func (s *Server) NewNARReceiver(ctx context.Context) *NARReceiver {
	if ctx == nil {
		// Easier to catch at this point on the stack than later.
		panic("nil context passed to NewNARReceiver")
	}
	return &NARReceiver{
		ctx:        ctx,
		dir:        s.dir,
		realDir:    s.realDir,
		dbPool:     s.db,
		inProgress: &s.inProgress,
		hasher:     *nix.NewHasher(nix.SHA256),
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
	ctx := r.ctx
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

	unlock, err := r.inProgress.lock(ctx, trailer.StorePath)
	if err != nil {
		log.Errorf(ctx, "Failed to lock %s: %v", trailer.StorePath, err)
		return
	}
	defer unlock()

	realPath := filepath.Join(r.realDir, trailer.StorePath.Base())
	if _, err := os.Lstat(realPath); err == nil {
		log.Debugf(ctx, "Received NAR for %s. Exists in store, skipping...", trailer.StorePath)
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Errorf(ctx, "Received NAR for %s. Failed to check for existence: %v", trailer.StorePath, err)
		return
	}

	log.Debugf(ctx, "Extracting %s.nar to %s...", trailer.StorePath, realPath)
	if err := extractNAR(realPath, r.tmpFile); err != nil {
		log.Warnf(ctx, "Import of %s failed: %v", trailer.StorePath, err)
		if err := os.RemoveAll(realPath); err != nil {
			log.Errorf(ctx, "Failed to clean up partial import of %s: %v", trailer.StorePath, err)
		}
		return
	}

	log.Debugf(ctx, "Recording import of %s...", trailer.StorePath)
	conn, err := r.dbPool.Get(ctx)
	if err != nil {
		log.Warnf(ctx, "Connecting to store database: %v", err)
		if err := os.RemoveAll(realPath); err != nil {
			log.Errorf(ctx, "Failed to clean up partial import of %s: %v", trailer.StorePath, err)
		}
		return
	}
	defer r.dbPool.Put(conn)
	err = func() (err error) {
		endFn, err := sqlitex.ImmediateTransaction(conn)
		if err != nil {
			return err
		}
		defer endFn(&err)

		return insertObject(ctx, conn, &zbstore.NARInfo{
			StorePath:   trailer.StorePath,
			NARSize:     r.size,
			NARHash:     r.hasher.SumHash(),
			Compression: zbstore.NoCompression,
			Deriver:     trailer.Deriver,
			CA:          ca,
		})
	}()
	if err != nil {
		log.Errorf(ctx, "Recording import of %s: %v", trailer.StorePath, err)
		if err := os.RemoveAll(realPath); err != nil {
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
		computed, _, err = zbstore.SourceSHA256ContentAddress(digest, narContent)
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
		if hdr.Mode&0o111 != 0 {
			return nix.ContentAddress{}, fmt.Errorf("verify %s content address: must not be executable", path)
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

// extractNAR extracts a NAR file to the local filesystem at the given path.
func extractNAR(dst string, r io.Reader) error {
	nr := nar.NewReader(r)
	for {
		hdr, err := nr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		p := filepath.Join(dst, filepath.FromSlash(hdr.Path))
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

// Cleanup releases any resources associated with the receiver.
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

type peerContextKey struct{}

// WithPeer returns a copy of parent
// in which the given handler is used as the client's connection.
func WithPeer(parent context.Context, peer jsonrpc.Handler) context.Context {
	return context.WithValue(parent, peerContextKey{}, peer)
}

func peer(ctx context.Context) jsonrpc.Handler {
	p, _ := ctx.Value(peerContextKey{}).(jsonrpc.Handler)
	if p == nil {
		p = jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
			return nil, jsonrpc.Error(jsonrpc.InternalError, errors.New("no peer in context"))
		})
	}
	return p
}

func marshalResponse(data any) (*jsonrpc.Response, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, jsonrpc.Error(jsonrpc.InternalError, err)
	}
	return &jsonrpc.Response{Result: jsonData}, nil
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
