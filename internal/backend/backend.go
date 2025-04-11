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
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/osutil"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
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

	// BuildContext optionally specifies a function that detaches the context for a build.
	// If BuildContext is nil, the default is [context.Background].
	BuildContext func(parent context.Context, buildID string) context.Context

	// BuildLogRetention is the length of time to retain build logs.
	// If non-positive, then build logs will be not be automatically deleted.
	BuildLogRetention time.Duration
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
	buildContext    func(context.Context, string) context.Context

	sandbox      bool
	sandboxPaths map[string]string

	cancelGCLogs context.CancelFunc
	gcLogsDone   <-chan struct{}

	coresPerBuild int

	writing        mutexMap[zbstore.Path] // store objects being written
	building       mutexMap[zbstore.Path] // derivations being built
	users          *userSet
	buildWaitGroup sync.WaitGroup

	activeBuildsMu sync.Mutex
	activeBuilds   map[uuid.UUID]context.CancelFunc
	draining       bool
}

// NewServer returns a new [Server] for the given store directory and database path.
// Callers are responsible for calling [Server.Close] on the returned server.
func NewServer(dir zbstore.Directory, dbPath string, opts *Options) *Server {
	if opts == nil {
		opts = new(Options)
	}
	users, err := newUserSet(opts.BuildUsers)
	if err != nil {
		panic(err)
	}
	gcLogsDone := make(chan struct{})
	srv := &Server{
		dir:             dir,
		realDir:         opts.RealDir,
		buildDir:        opts.BuildDir,
		allowKeepFailed: opts.AllowKeepFailed,
		sandbox:         !opts.DisableSandbox && CanSandbox(),
		sandboxPaths:    opts.SandboxPaths,
		coresPerBuild:   opts.CoresPerBuild,
		users:           users,
		activeBuilds:    make(map[uuid.UUID]context.CancelFunc),
		buildContext:    opts.BuildContext,

		gcLogsDone: gcLogsDone,

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
	if srv.buildContext == nil {
		srv.buildContext = func(_ context.Context, _ string) context.Context {
			return context.Background()
		}
	}
	if opts.BuildLogRetention <= 0 {
		srv.cancelGCLogs = func() {}
		close(gcLogsDone)
	} else {
		var gcLogContext context.Context
		gcLogContext, srv.cancelGCLogs = context.WithCancel(context.Background())
		go func() {
			defer close(gcLogsDone)
			srv.gcLogs(gcLogContext, opts.BuildLogRetention)
		}()
	}
	return srv
}

// Close releases any resources associated with the server.
func (s *Server) Close() error {
	s.cancelGCLogs()
	s.activeBuildsMu.Lock()
	s.draining = true
	for _, cancel := range s.activeBuilds {
		cancel()
	}
	s.activeBuildsMu.Unlock()

	s.buildWaitGroup.Wait()
	<-s.gcLogsDone

	return s.db.Close()
}

// JSONRPC implements the [jsonrpc.Handler] interface
// and serves the [zbstore] API.
func (s *Server) JSONRPC(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	return jsonrpc.ServeMux{
		zbstorerpc.ExistsMethod:         jsonrpc.HandlerFunc(s.exists),
		zbstorerpc.InfoMethod:           jsonrpc.HandlerFunc(s.info),
		zbstorerpc.ExpandMethod:         jsonrpc.HandlerFunc(s.expand),
		zbstorerpc.RealizeMethod:        jsonrpc.HandlerFunc(s.realize),
		zbstorerpc.GetBuildMethod:       jsonrpc.HandlerFunc(s.getBuild),
		zbstorerpc.GetBuildResultMethod: jsonrpc.HandlerFunc(s.getBuildResult),
		zbstorerpc.CancelBuildMethod:    jsonrpc.HandlerFunc(s.cancelBuild),
		zbstorerpc.ReadLogMethod:        jsonrpc.HandlerFunc(s.readLog),
	}.JSONRPC(ctx, req)
}

func (s *Server) realPath(path zbstore.Path) string {
	return filepath.Join(s.realDir, path.Base())
}

func (s *Server) exists(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	var args zbstorerpc.ExistsRequest
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
	var args zbstorerpc.InfoRequest
	if err := json.Unmarshal(req.Params, &args); err != nil {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
	}
	if args.Path.Dir() != s.dir {
		return marshalResponse(&zbstorerpc.InfoResponse{})
	}

	conn, err := s.db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer s.db.Put(conn)

	log.Debugf(ctx, "Looking up path info for %s...", args.Path)
	info, err := pathInfo(conn, args.Path)
	if errors.Is(err, errObjectNotExist) {
		return marshalResponse(&zbstorerpc.InfoResponse{})
	}
	if info.References == nil {
		// Don't send null for the array.
		info.References = []zbstore.Path{}
	}
	return marshalResponse(&zbstorerpc.InfoResponse{
		Info: info,
	})
}

func (s *Server) getBuild(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	var args zbstorerpc.GetBuildRequest
	if err := json.Unmarshal(req.Params, &args); err != nil {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
	}
	buildID, ok := parseBuildID(args.BuildID)
	if !ok {
		return marshalResponse(&zbstorerpc.Build{
			ID:      args.BuildID,
			Status:  zbstorerpc.BuildUnknown,
			Results: []*zbstorerpc.BuildResult{},
		})
	}

	conn, err := s.db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer s.db.Put(conn)

	// Read active status before consulting database.
	// We write to the database before clearing the active status.
	s.activeBuildsMu.Lock()
	_, isActive := s.activeBuilds[buildID]
	s.activeBuildsMu.Unlock()

	rollback, err := readonlySavepoint(conn)
	if err != nil {
		return nil, fmt.Errorf("get build %v: %v", buildID, err)
	}
	defer rollback()

	resp := &zbstorerpc.Build{
		ID:      args.BuildID,
		Status:  zbstorerpc.BuildUnknown,
		Results: []*zbstorerpc.BuildResult{},
	}
	err = sqlitex.ExecuteTransientFS(conn, sqlFiles(), "build/find.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":build_id": buildID.String(),
		},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			switch typ := stmt.ColumnType(stmt.ColumnIndex("ended_at")); typ {
			case sqlite.TypeNull:
				if !isActive {
					// If we don't have an end time and we're not running a build with this ID,
					// assume it was orphaned from a previous run.
					return nil
				}
				resp.Status = zbstorerpc.BuildActive
			case sqlite.TypeInteger:
				resp.Status = zbstorerpc.BuildStatus(stmt.GetText("status"))
				resp.EndedAt = zbstorerpc.NonNull(time.UnixMilli(stmt.GetInt64("ended_at")).UTC())
			default:
				return fmt.Errorf("type(ended_at) = %v", typ)
			}

			resp.StartedAt = time.UnixMilli(stmt.GetInt64("started_at"))

			if stmt.GetBool("has_expand") {
				resp.Expand = &zbstorerpc.ExpandResult{
					Builder: stmt.GetText("expand_builder"),
				}
				if s := stmt.GetText("expand_args"); s == "" {
					resp.Expand.Args = []string{}
				} else if err := unmarshalJSONString(s, &resp.Expand.Args); err != nil {
					return fmt.Errorf("expand.args: %v", err)
				}
				if s := stmt.GetText("expand_env"); s == "" {
					resp.Expand.Env = map[string]string{}
				} else if err := unmarshalJSONString(s, &resp.Expand.Env); err != nil {
					return fmt.Errorf("expand.env: %v", err)
				}
			}

			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get build %v: %v", buildID, err)
	}
	if resp.Status == zbstorerpc.BuildUnknown {
		return marshalResponse(resp)
	}

	resp.Results, err = findBuildResults(resp.Results, conn, buildID, "")
	if err != nil {
		return nil, err
	}

	return marshalResponse(resp)
}

func (s *Server) getBuildResult(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	var args zbstorerpc.GetBuildResultRequest
	if err := json.Unmarshal(req.Params, &args); err != nil {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
	}
	buildID, ok := parseBuildID(args.BuildID)
	if !ok || args.DrvPath == "" {
		return marshalResponse(&zbstorerpc.BuildResult{
			DrvPath: args.DrvPath,
			Status:  zbstorerpc.BuildUnknown,
			Outputs: []*zbstorerpc.RealizeOutput{},
		})
	}

	conn, err := s.db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer s.db.Put(conn)

	results, err := findBuildResults(nil, conn, buildID, args.DrvPath)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return marshalResponse(&zbstorerpc.BuildResult{
			DrvPath: args.DrvPath,
			Status:  zbstorerpc.BuildUnknown,
			Outputs: []*zbstorerpc.RealizeOutput{},
		})
	}
	if len(results) > 1 {
		return nil, fmt.Errorf("internal error: multiple build results found for %s in build %v", args.DrvPath, buildID)
	}
	return marshalResponse(results[0])
}

func (s *Server) cancelBuild(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	var args zbstorerpc.CancelBuildNotification
	if err := json.Unmarshal(req.Params, &args); err != nil {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
	}
	buildID, ok := parseBuildID(args.BuildID)
	if !ok {
		return nil, nil
	}
	s.activeBuildsMu.Lock()
	cancel := s.activeBuilds[buildID]
	s.activeBuildsMu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil, nil
}

func (s *Server) readLog(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	var args zbstorerpc.ReadLogRequest
	if err := json.Unmarshal(req.Params, &args); err != nil {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
	}
	newNotFoundError := func() error {
		// TODO(someday): Use 404-like error code.
		return fmt.Errorf("could not find log for %q in build ID %q", args.DrvPath, args.BuildID)
	}
	if args.DrvPath == "" {
		return nil, newNotFoundError()
	}
	if args.RangeStart < 0 {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, fmt.Errorf("log range start must be non-negative"))
	}
	if args.RangeEnd.Valid && args.RangeEnd.X <= args.RangeStart {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, fmt.Errorf("log range end must be greater than range start"))
	}
	buildID, ok := parseBuildID(args.BuildID)
	if !ok {
		return nil, newNotFoundError()
	}

	conn, err := s.db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer s.db.Put(conn)

	const maxRead = 64 * 1024
	end := args.RangeStart + maxRead
	if args.RangeEnd.Valid {
		end = min(end, args.RangeEnd.X)
	}
	buf := make([]byte, end-args.RangeStart)
	n := 0
	for {
		nn, err := readBuildLogAt(conn, buildID, args.DrvPath, buf[n:], args.RangeStart+int64(n))
		n += nn
		switch {
		case err == nil || err == io.EOF:
			resp := &zbstorerpc.ReadLogResponse{EOF: err == io.EOF}
			resp.SetPayload(buf[:n])
			return marshalResponse(resp)
		case errors.Is(err, errBuildLogPending):
			if n > 0 {
				// Prefer returning what we have rather than blocking.
				resp := new(zbstorerpc.ReadLogResponse)
				resp.SetPayload(buf[:n])
				return marshalResponse(resp)
			}

			// Wait for more.
			// TODO(someday): Have build logs signal when there's more data rather than polling.
			t := time.NewTimer(builderLogInterval)
			select {
			case <-t.C:
			case <-ctx.Done():
				t.Stop()
				return nil, fmt.Errorf("read log for %s in build %v: %w", args.DrvPath, buildID, ctx.Err())
			}
		case errors.Is(err, errBuildLogNotFound):
			return nil, newNotFoundError()
		default:
			return nil, err
		}
	}
}

// RecentBuildIDs returns the most recent builds started or finished.
// It returns at most limit values.
func (s *Server) RecentBuildIDs(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}

	conn, err := s.db.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("list recent builds: %v", err)
	}
	defer s.db.Put(conn)

	result := make([]string, 0, limit)
	err = sqlitex.ExecuteTransientFS(conn, sqlFiles(), "build/recent.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":n": limit,
		},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			result = append(result, stmt.GetText("id"))
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("list recent builds: %v", err)
	}
	return result, nil
}

func (s *Server) gcLogs(ctx context.Context, window time.Duration) {
	ticker := time.NewTicker(min(5*time.Minute, window))
	defer ticker.Stop()

	t := time.Now()
	for {
		conn, err := s.db.Get(ctx)
		if err != nil {
			// Likely means context was canceled.
			log.Debugf(ctx, "Exiting build log GC due to: %v", err)
			return
		}
		cutoff := t.Add(-window)
		s.activeBuildsMu.Lock()
		activeBuilds := slices.Collect(maps.Keys(s.activeBuilds))
		s.activeBuildsMu.Unlock()
		log.Debugf(ctx, "Cleaning up build logs older than %v...", cutoff.UTC())
		if n, err := deleteOldBuilds(conn, cutoff, slices.Values(activeBuilds)); err != nil {
			log.Warnf(ctx, "Failed to clean up build logs: %v", err)
		} else if n > 0 {
			log.Infof(ctx, "Deleted %d build logs older than %v", n, cutoff.Truncate(time.Millisecond).UTC())
		} else {
			log.Debugf(ctx, "No build logs to clean up.")
		}
		s.db.Put(conn)

		select {
		case t = <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
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

type exporterContextKey struct{}

// A type that implements Exporter can receive a `nix-store --export` formatted stream.
type Exporter interface {
	Export(r io.Reader) error
}

// WithExporter returns a copy of parent
// in which the given exporter is used to send back export information.
func WithExporter(parent context.Context, e Exporter) context.Context {
	return context.WithValue(parent, exporterContextKey{}, e)
}

func exporterFromContext(ctx context.Context) Exporter {
	e, _ := ctx.Value(exporterContextKey{}).(Exporter)
	if e == nil {
		e = stubExporter{}
	}
	return e
}

type stubExporter struct{}

func (stubExporter) Export(r io.Reader) error {
	return errors.New("no exporter in context")
}

func parseBuildID(id string) (_ uuid.UUID, ok bool) {
	u, err := uuid.Parse(id)
	if err != nil || id != u.String() {
		return uuid.UUID{}, false
	}
	return u, true
}

func marshalResponse(data any) (*jsonrpc.Response, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, jsonrpc.Error(jsonrpc.InternalError, err)
	}
	return &jsonrpc.Response{Result: jsonData}, nil
}
