// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package backend provides a [zbstore] implementation backed by local compute resources.
package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/osutil"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
)

// DefaultBuildUsersGroup is the conventional name of the Unix group
// for the users that execute builders on behalf of the daemon.
const DefaultBuildUsersGroup = "zbld"

// Options is the set of optional parameters to [NewServer].
type Options struct {
	// RealDir is where the store objects are located physically on disk.
	// If empty, defaults to the store directory.
	RealDir string
	// BuildDir is where realizations' working directories will be placed.
	// If empty, defaults to [os.TempDir].
	BuildDir string

	// If AllowKeepFailed is true, then the KeepFailed field in [zbstore.RealizeRequest] will be respected.
	AllowKeepFailed bool

	// If DisableSandbox is true, then builders are always run without the sandbox.
	// Otherwise, sandboxing is used whenever possible.
	DisableSandbox bool
	// SandboxPaths is a map of paths inside the sandbox
	// to paths on the host machine.
	// These paths will be made available to sandboxed builders.
	SandboxPaths map[string]string

	// CoresPerBuild is a hint from the user to builders
	// on the number of concurrent jobs to perform.
	// If non-positive, then the number of cores detected on the machine is used.
	CoresPerBuild int

	// BuildUsers is the set of user IDs to use for builds on non-Windows systems.
	// If empty, then builds will use the current process's privileges.
	// [NewServer] will panic if multiple entries have the same user ID.
	BuildUsers []BuildUser
}

// BuildUser is a descriptor for a Unix user.
type BuildUser struct {
	// UID is the user ID.
	UID int
	// GID is the user's primary group ID.
	GID int
}

func (user BuildUser) String() string {
	return fmt.Sprintf("%d:%d", user.UID, user.GID)
}

// SystemSupportsSandbox reports whether the host operating system supports sandboxing.
func SystemSupportsSandbox() bool {
	return runtime.GOOS == "linux"
}

// CanSandbox reports whether the current execution environment supports sandboxing.
func CanSandbox() bool {
	return SystemSupportsSandbox() && os.Geteuid() == 0
}

// Server is a local store.
// Server implements [jsonrpc.Handler] and is intended to be used with [jsonrpc.Serve].
type Server struct {
	dir             zbstore.Directory
	realDir         string
	buildDir        string
	db              *sqlitemigration.Pool
	allowKeepFailed bool

	sandbox      bool
	sandboxPaths map[string]string

	coresPerBuild int

	writing  mutexMap[zbstore.Path] // store objects being written
	building mutexMap[zbstore.Path] // derivations being built
	users    *userSet
}

// NewServer returns a new [Server] for the given store directory and database path.
// Callers are responsible for calling [Server.Close] on the returned server.
func NewServer(dir zbstore.Directory, dbPath string, opts *Options) *Server {
	users, err := newUserSet(opts.BuildUsers)
	if err != nil {
		panic(err)
	}
	srv := &Server{
		dir:             dir,
		realDir:         opts.RealDir,
		buildDir:        opts.BuildDir,
		allowKeepFailed: opts.AllowKeepFailed,
		sandbox:         !opts.DisableSandbox && CanSandbox(),
		sandboxPaths:    opts.SandboxPaths,
		coresPerBuild:   opts.CoresPerBuild,
		users:           users,

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
	if srv.coresPerBuild <= 0 {
		srv.coresPerBuild = max(1, runtime.NumCPU())
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
		zbstore.InfoMethod:    jsonrpc.HandlerFunc(s.info),
		zbstore.ExpandMethod:  jsonrpc.HandlerFunc(s.expand),
		zbstore.RealizeMethod: jsonrpc.HandlerFunc(s.realize),
	}.JSONRPC(ctx, req)
}

func (s *Server) realPath(path zbstore.Path) string {
	return filepath.Join(s.realDir, path.Base())
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
	unlock, err := s.writing.lock(ctx, p)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err := os.Lstat(filepath.Join(s.realPath(p), filepath.FromSlash(sub))); err != nil {
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

func (s *Server) info(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	var args zbstore.InfoRequest
	if err := json.Unmarshal(req.Params, &args); err != nil {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
	}
	if args.Path.Dir() != s.dir {
		return marshalResponse(&zbstore.InfoResponse{})
	}

	conn, err := s.db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer s.db.Put(conn)

	log.Debugf(ctx, "Looking up path info for %s...", args.Path)
	info, err := pathInfo(conn, args.Path)
	if errors.Is(err, errObjectNotExist) {
		return marshalResponse(&zbstore.InfoResponse{})
	}
	if info.References == nil {
		// Don't send null for the array.
		info.References = []zbstore.Path{}
	}
	return marshalResponse(&zbstore.InfoResponse{
		Info: info,
	})
}

// NARReceiver is a per-connection [zbstore.NARReceiver].
type NARReceiver struct {
	ctx     context.Context
	dir     zbstore.Directory
	realDir string
	dbPool  *sqlitemigration.Pool
	writing *mutexMap[zbstore.Path]
	tmpFile *os.File

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
		ctx:     ctx,
		dir:     s.dir,
		realDir: s.realDir,
		dbPool:  s.db,
		writing: &s.writing,
		hasher:  *nix.NewHasher(nix.SHA256),
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

	unlock, err := r.writing.lock(ctx, trailer.StorePath)
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
			CA:          ca,
			References:  trailer.References,
		})
	}()
	if err != nil {
		log.Errorf(ctx, "Recording import of %s: %v", trailer.StorePath, err)
		if err := os.RemoveAll(realPath); err != nil {
			log.Errorf(ctx, "Failed to clean up partial import of %s: %v", trailer.StorePath, err)
		}
		return
	}

	makePublicReadOnly(ctx, realPath)

	log.Infof(ctx, "Imported %s", trailer.StorePath)
}

// verifyContentAddress validates that the content matches the given content address.
// If the content address is the zero value,
// then the content address is computed as a "source" store object.
func verifyContentAddress(path zbstore.Path, narContent io.Reader, refs *sets.Sorted[zbstore.Path], ca nix.ContentAddress) (nix.ContentAddress, error) {
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

// makePublicReadOnly calls [osutil.MakePublicReadOnly]
// and logs any errors instead of causing them to stop the operation.
func makePublicReadOnly(ctx context.Context, path string) {
	log.Debugf(ctx, "Marking %s read-only...", path)
	osutil.MakePublicReadOnly(path, func(err error) error {
		// Log errors, but don't abort the chmod attempt.
		// Subsequent use of this store object can still succeed,
		// and we want to mark as many files read-only as possible.
		log.Warnf(ctx, "%v", err)
		return nil
	})
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
