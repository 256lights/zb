// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package backend provides a [zbstorerpc] implementation backed by local compute resources.
package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/go-json-experiment/json/jsontext"
	"github.com/google/uuid"
	"zb.256lights.llc/pkg/bytebuffer"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/xiter"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
)

// DefaultBuildUsersGroup is the conventional name of the Unix group
// for the users that execute builders on behalf of the daemon.
const DefaultBuildUsersGroup = "zbld"

// Options is the set of optional parameters to [NewServer].
type Options struct {
	// RealStoreDirectory is where the store objects are located physically on disk.
	// If empty, defaults to the store directory.
	RealStoreDirectory string
	// BuildDirectory is where realizations' working directories will be placed.
	// If empty, defaults to [os.TempDir].
	BuildDirectory string
	// LogDirectory is where builder logs will be stored.
	// If empty, defaults to a directory called "log" in the same directory as the database.
	LogDirectory string
	// ContentAddressBufferCreator is used to create buffers for content addressing analysis.
	// If nil, then in-memory byte slices are used with reasonable limits.
	ContentAddressBufferCreator bytebuffer.Creator

	// DatabasePoolSize is the maximum permitted number of concurrent connections to the database.
	// If less than 1, a reasonable default is used.
	DatabasePoolSize int

	// If AllowKeepFailed is true, then the KeepFailed field in [zbstore.RealizeRequest] will be respected.
	AllowKeepFailed bool

	// If DisableSandbox is true, then builders are always run without the sandbox.
	// Otherwise, sandboxing is used whenever possible.
	DisableSandbox bool
	// SandboxPaths is a map of paths inside the sandbox
	// to paths on the host machine.
	// These paths will be made available to sandboxed builders.
	SandboxPaths map[string]SandboxPath

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

// A SandboxPath is the set of options for SandboxPaths in [Options].
type SandboxPath struct {
	// Path is the path on the backend's filesystem to make available at the path.
	// If empty, the SandboxPaths key is used.
	Path string
	// If AlwaysPresent is true, then the path will always be made available in the sandbox.
	// The default is to only allow the path to be used if it is declared in __buildSystemDeps.
	AlwaysPresent bool
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
	logDir          string
	caCreateTemp    bytebuffer.Creator
	db              *sqlitemigration.Pool
	allowKeepFailed bool
	buildContext    func(context.Context, string) context.Context

	sandbox      bool
	sandboxPaths map[string]SandboxPath

	cancelBackground context.CancelFunc
	background       sync.WaitGroup

	coresPerBuild int

	writing  mutexMap[zbstore.Path] // store objects being written
	building mutexMap[zbstore.Path] // derivations being built
	users    *userSet

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
	srv := &Server{
		dir:             dir,
		realDir:         opts.RealStoreDirectory,
		buildDir:        opts.BuildDirectory,
		logDir:          opts.LogDirectory,
		caCreateTemp:    opts.ContentAddressBufferCreator,
		allowKeepFailed: opts.AllowKeepFailed,
		sandbox:         !opts.DisableSandbox && CanSandbox(),
		sandboxPaths:    maps.Clone(opts.SandboxPaths),
		coresPerBuild:   opts.CoresPerBuild,
		users:           users,
		activeBuilds:    make(map[uuid.UUID]context.CancelFunc),
		buildContext:    opts.BuildContext,

		db: sqlitemigration.NewPool(dbPath, loadSchema(), sqlitemigration.Options{
			Flags:       sqlite.OpenCreate | sqlite.OpenReadWrite,
			PrepareConn: prepareConn,
			PoolSize:    opts.DatabasePoolSize,
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
	if srv.logDir == "" {
		srv.logDir = filepath.Join(filepath.Dir(dbPath), "log")
	}
	if srv.caCreateTemp == nil {
		srv.caCreateTemp = bytebuffer.BufferCreator{}
	}
	if srv.buildContext == nil {
		srv.buildContext = func(_ context.Context, _ string) context.Context {
			return context.Background()
		}
	}
	var bgCtx context.Context
	bgCtx, srv.cancelBackground = context.WithCancel(context.Background())
	srv.background.Add(1)
	go func() {
		defer srv.background.Done()
		srv.optimizeDatabase(bgCtx)
	}()
	if opts.BuildLogRetention > 0 {
		srv.background.Add(1)
		go func() {
			defer srv.background.Done()
			srv.gcLogs(bgCtx, opts.BuildLogRetention)
		}()
	}
	return srv
}

// Close releases any resources associated with the server.
func (s *Server) Close() error {
	s.cancelBackground()
	s.activeBuildsMu.Lock()
	s.draining = true
	for _, cancel := range s.activeBuilds {
		cancel()
	}
	s.activeBuildsMu.Unlock()

	s.background.Wait()

	return s.db.Close()
}

// JSONRPC implements the [jsonrpc.Handler] interface
// and serves the [zbstorerpc] API.
func (s *Server) JSONRPC(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	return jsonrpc.ServeMux{
		zbstorerpc.ExistsMethod:         jsonrpc.HandlerFunc(s.exists),
		zbstorerpc.InfoMethod:           jsonrpc.HandlerFunc(s.info),
		zbstorerpc.ExportMethod:         jsonrpc.HandlerFunc(s.export),
		zbstorerpc.ExpandMethod:         jsonrpc.HandlerFunc(s.expand),
		zbstorerpc.RealizeMethod:        jsonrpc.HandlerFunc(s.realize),
		zbstorerpc.GetBuildMethod:       jsonrpc.HandlerFunc(s.getBuild),
		zbstorerpc.GetBuildResultMethod: jsonrpc.HandlerFunc(s.getBuildResult),
		zbstorerpc.CancelBuildMethod:    jsonrpc.HandlerFunc(s.cancelBuild),
		zbstorerpc.ReadLogMethod:        jsonrpc.HandlerFunc(s.readLog),

		zbstorerpc.NopMethod: jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
			return &jsonrpc.Response{
				Result: jsontext.Value("null"),
			}, nil
		}),
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
			Result: jsontext.Value("false"),
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
			Result: jsontext.Value("false"),
		}, nil
	}
	log.Debugf(ctx, "%s exists", args.Path)
	return &jsonrpc.Response{
		Result: jsontext.Value("true"),
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
	return marshalResponse(&zbstorerpc.InfoResponse{
		Info: info.ToRPC(),
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

	resp.Results, err = findBuildResults(resp.Results, conn, s.logDir, buildID, "")
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

	results, err := findBuildResults(nil, conn, s.logDir, buildID, args.DrvPath)
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

	f, openError := os.Open(builderLogPath(s.logDir, buildID, args.DrvPath))
	if errors.Is(openError, os.ErrNotExist) {
		conn, err := s.db.Get(ctx)
		if err != nil {
			return nil, fmt.Errorf("fetch build result for %s in build %v: %v", args.DrvPath, buildID, err)
		}
		defer s.db.Put(conn)
		results, err := findBuildResults(nil, conn, "", buildID, args.DrvPath)
		if err != nil {
			return nil, err
		}
		if len(results) == 0 {
			return nil, newNotFoundError()
		}
		// Treat like a zero-length log.
		return marshalResponse(&zbstorerpc.ReadLogResponse{EOF: true})
	}
	if openError != nil {
		return nil, openError
	}
	defer f.Close()

	const maxRead = 64 * 1024
	end := args.RangeStart + maxRead
	if args.RangeEnd.Valid {
		end = min(end, args.RangeEnd.X)
	}
	buf := make([]byte, end-args.RangeStart)

	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, fmt.Errorf("read log for %s in build %v: %v", args.DrvPath, buildID, err)
	}
	if args.RangeStart+int64(len(buf)) < size {
		// Special case: if the requested range is within what's already written,
		// we can skip acquiring a database connection.
		// We only need the database connection to check whether the builder is finished.
		if _, err := f.Seek(args.RangeStart, io.SeekStart); err != nil {
			return nil, fmt.Errorf("read log for %s in build %v: %v", args.DrvPath, buildID, err)
		}
		n, err := io.ReadFull(f, buf)
		if n == 0 {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return nil, fmt.Errorf("read log for %s in build %v: %v", args.DrvPath, buildID, err)
		}
		resp := &zbstorerpc.ReadLogResponse{EOF: err == io.EOF}
		resp.SetPayload(buf[:n])
		return marshalResponse(resp)
	}

	conn, err := s.db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer s.db.Put(conn)

	var results []*zbstorerpc.BuildResult
	for {
		var err error
		results, err = findBuildResults(results[:0], conn, "", buildID, args.DrvPath)
		if err != nil {
			return nil, err
		}
		if len(results) == 0 {
			return nil, newNotFoundError()
		}

		// Read log size after reading builder status.
		// If the status is finished, then the log should be at its final size.
		size, err := f.Seek(0, io.SeekEnd)
		if err != nil {
			return nil, fmt.Errorf("read log for %s in build %v: %v", args.DrvPath, buildID, err)
		}
		// At least one byte should be available.
		if args.RangeStart < size {
			if _, err := f.Seek(args.RangeStart, io.SeekStart); err != nil {
				return nil, fmt.Errorf("read log for %s in build %v: %v", args.DrvPath, buildID, err)
			}
			n := 0
			var readError error
			for n < len(buf) && readError == nil {
				var nn int
				nn, readError = f.Read(buf[n:])
				n += nn
			}
			if n == 0 {
				if readError == io.EOF {
					readError = io.ErrUnexpectedEOF
				}
				return nil, fmt.Errorf("read log for %s in build %v: %v", args.DrvPath, buildID, readError)
			}
			resp := &zbstorerpc.ReadLogResponse{
				EOF: readError == io.EOF && results[0].Status.IsFinished(),
			}
			resp.SetPayload(buf[:n])
			return marshalResponse(resp)
		}

		if results[0].Status.IsFinished() {
			if args.RangeStart > size {
				return nil, fmt.Errorf("read log for %s in build %v: start byte %d out of range",
					args.DrvPath, buildID, args.RangeStart)
			}
			return marshalResponse(&zbstorerpc.ReadLogResponse{EOF: true})
		}

		// Wait for more.
		t := time.NewTimer(builderLogInterval)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return nil, fmt.Errorf("read log for %s in build %v: %w", args.DrvPath, buildID, ctx.Err())
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

// Delete deletes the set of store paths.
// Delete will return an error if any of the named paths do not exist
// or there are store objects beyond those named that refer to the named store objects.
func (s *Server) Delete(ctx context.Context, paths sets.Set[zbstore.Path]) error {
	return s.delete(ctx, paths, false)
}

// DeleteIncludingReferences is the same as [*Server.Delete],
// but if the store objects have other store objects referring to them,
// then they will be deleted as well.
func (s *Server) DeleteIncludingReferences(ctx context.Context, paths sets.Set[zbstore.Path]) error {
	return s.delete(ctx, paths, true)
}

func (s *Server) delete(ctx context.Context, paths sets.Set[zbstore.Path], recursive bool) (err error) {
	if paths.Len() == 0 {
		return nil
	}
	defer func() {
		if err != nil {
			if path, singleError := xiter.Single(paths.All()); singleError == nil {
				err = fmt.Errorf("delete %s: %v", path, err)
			} else {
				err = fmt.Errorf("delete store paths: %v", err)
			}
		}
	}()

	for path := range paths.All() {
		if path.Dir() != s.dir {
			return fmt.Errorf("%s not in %s", path, s.dir)
		}
	}

	var allPaths []zbstore.Path
	var unlocks []func()
	defer func() {
		for _, u := range unlocks {
			u()
		}
		unlocks = nil
	}()
	err = func() (err error) {
		conn, err := s.db.Get(ctx)
		if err != nil {
			return err
		}
		defer s.db.Put(conn)

		endFn, err := sqlitex.ImmediateTransaction(conn)
		if err != nil {
			return err
		}
		defer endFn(&err)
		if err := sqlitex.ExecuteScriptFS(conn, sqlFiles(), "delete/create_target_table.sql", nil); err != nil {
			return err
		}

		insertStmt, err := sqlitex.PrepareTransientFS(conn, sqlFiles(), "delete/insert_target.sql")
		if err != nil {
			return err
		}
		defer insertStmt.Finalize()
		for path := range paths.All() {
			if exists, err := objectExists(conn, path); err != nil {
				return err
			} else if !exists {
				return fmt.Errorf("%s: %w", path, errObjectNotExist)
			}

			insertStmt.SetText(":path", string(path))
			if _, err := insertStmt.Step(); err != nil {
				return fmt.Errorf("%s: %v", path, err)
			}
			if err := insertStmt.Reset(); err != nil {
				return fmt.Errorf("%s: %v", path, err)
			}
		}

		reverseDeps := make(sets.Set[zbstore.Path])
		err = sqlitex.ExecuteTransientFS(conn, sqlFiles(), "delete/reverse_deps.sql", &sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				path, err := zbstore.ParsePath(stmt.GetText("path"))
				if err != nil {
					return err
				}
				reverseDeps.Add(path)
				return nil
			},
		})
		if err != nil {
			return err
		}

		if reverseDeps.Len() > 0 {
			log.Debugf(ctx, "Found more dependencies to delete: %v", reverseDeps)
			if !recursive {
				return fmt.Errorf("store objects have references")
			}
		}
		if err := sqlitex.ExecuteScriptFS(conn, sqlFiles(), "delete/drop_target_table.sql", nil); err != nil {
			return err
		}

		// Reverse topological sort.
		allPaths = make([]zbstore.Path, 0, paths.Len()+reverseDeps.Len())
		allPaths = slices.AppendSeq(allPaths, xiter.Chain(paths.All(), reverseDeps.All()))
		references := make(map[zbstore.Path]sets.Sorted[zbstore.Path], len(allPaths))
		for _, path := range allPaths {
			var err error
			references[path], err = listReferences(conn, path)
			if err != nil {
				return err
			}
		}
		err = sortByReferences(
			allPaths,
			func(p zbstore.Path) zbstore.Path { return p },
			func(p zbstore.Path) sets.Sorted[zbstore.Path] { return references[p] },
			true,
		)
		if err != nil {
			return err
		}
		slices.Reverse(allPaths)

		// Delete the files in reverse dependency order.
		deleteStmt, err := sqlitex.PrepareTransientFS(conn, sqlFiles(), "delete/delete.sql")
		if err != nil {
			return err
		}
		defer deleteStmt.Finalize()
		deleteSelfRefStmt, err := sqlitex.PrepareTransientFS(conn, sqlFiles(), "delete/delete_self_ref.sql")
		if err != nil {
			return err
		}
		defer deleteSelfRefStmt.Finalize()
		for _, path := range allPaths {
			deleteSelfRefStmt.SetText(":path", string(path))
			if _, err := deleteSelfRefStmt.Step(); err != nil {
				return fmt.Errorf("%s: %v", path, err)
			}
			if err := deleteSelfRefStmt.Reset(); err != nil {
				return fmt.Errorf("%s: %v", path, err)
			}

			deleteStmt.SetText(":path", string(path))
			if _, err := deleteStmt.Step(); err != nil {
				return fmt.Errorf("%s: %v", path, err)
			}
			if err := deleteStmt.Reset(); err != nil {
				return fmt.Errorf("%s: %v", path, err)
			}
		}

		// Acquire write locks on the paths we're about to delete before committing the transaction.
		unlocks = make([]func(), 0, len(allPaths))
		for _, path := range allPaths {
			unlock, err := s.writing.lock(ctx, path)
			if err != nil {
				return err
			}
			unlocks = append(unlocks, unlock)
		}

		// We end the transaction before removing the files
		// to avoid blocking other work
		// but also because we don't want a partial failure in removing files
		// to abort the transaction.
		return nil
	}()
	if err != nil {
		return err
	}

	ok := true
	for _, path := range allPaths {
		log.Debugf(ctx, "Deleting store object %s...", path)
		if err := os.RemoveAll(s.realPath(path)); err != nil {
			log.Errorf(ctx, "Failed to delete %s: %v", path, err)
			ok = false
		}
	}
	if !ok {
		return fmt.Errorf("one or more store paths could not be deleted")
	}
	return nil
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
		n, err := deleteOldBuilds(ctx, conn, cutoff, &deleteOldBuildOptions{
			logDir: s.logDir,
			keep:   slices.Values(activeBuilds),
		})
		if err != nil {
			log.Warnf(ctx, "Failed to clean up build logs: %v", err)
		} else if n > 0 {
			log.Infof(ctx, "Deleted %d build logs older than %v", n, cutoff.Truncate(time.Millisecond).UTC())
		} else {
			log.Debugf(ctx, "No build logs to clean up.")
		}
		// Attempt to reclaim disk space.
		if err := sqlitex.ExecuteTransient(conn, "PRAGMA incremental_vacuum(128);", nil); err != nil {
			log.Warnf(ctx, "Incremental vacuum failed: %v", err)
		}
		s.db.Put(conn)

		select {
		case t = <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) optimizeDatabase(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}

		conn, err := s.db.Get(ctx)
		if err != nil {
			// Likely means context was canceled.
			log.Debugf(ctx, "Exiting background optimization due to: %v", err)
			return
		}
		if err := sqlitex.ExecuteTransient(conn, "PRAGMA optimize;", nil); err != nil {
			log.Warnf(ctx, "Incremental vacuum failed: %v", err)
		}
		s.db.Put(conn)
	}
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
