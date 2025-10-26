// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/activation"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"zb.256lights.llc/pkg/bytebuffer"
	"zb.256lights.llc/pkg/internal/backend"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/osutil"
	"zb.256lights.llc/pkg/internal/system"
	"zb.256lights.llc/pkg/internal/ui"
	"zb.256lights.llc/pkg/internal/xmaps"
	"zb.256lights.llc/pkg/internal/xnet"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/bass/runhttp"
	"zombiezen.com/go/log"
	"zombiezen.com/go/log/zstdlog"
)

const contentAddressTempFilePattern = "zb-ca-*"

type serveOptions struct {
	dbPath            string
	buildDir          string
	buildUsersGroup   string
	logDir            string
	keyFiles          []string
	sandbox           bool
	sandboxPaths      map[string]backend.SandboxPath
	allowKeepFailed   bool
	coresPerBuild     int
	buildLogRetention time.Duration
	systemdSocket     bool

	webListenAddress   string
	allowRemoteWeb     bool
	templatesDirectory string
	staticDirectory    string
}

func newServeCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "serve [options]",
		Short:                 "run a build server",
		DisableFlagsInUseLine: true,
		Args:                  cobra.NoArgs,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	opts := &serveOptions{
		dbPath:  filepath.Join(defaultVarDir(), "db.sqlite"),
		sandbox: backend.SystemSupportsSandbox(),
	}
	if osutil.IsRoot() {
		opts.buildUsersGroup = backend.DefaultBuildUsersGroup
	}
	if runtime.GOOS == "linux" {
		c.Flags().BoolVar(&opts.systemdSocket, "systemd", false, "use systemd socket activation")
	}
	c.Flags().StringVar(&opts.dbPath, "db", opts.dbPath, "`path` to store database file")
	c.Flags().StringVar(&opts.buildDir, "build-root", os.TempDir(), "`dir`ectory to store temporary build artifacts")
	c.Flags().StringVar(&opts.logDir, "log-directory", filepath.Join(filepath.Dir(string(zbstore.DefaultDirectory())), "var", "log", "zb"), "`dir`ectory to store builder logs in")
	c.Flags().StringVar(&opts.buildUsersGroup, "build-users-group", opts.buildUsersGroup, "name of Unix `group` of users to run builds as")
	c.Flags().StringArrayVar(&opts.keyFiles, "signing-key", nil, "key `file` for signing realizations (can be passed multiple times)")
	c.Flags().BoolVar(&opts.sandbox, "sandbox", opts.sandbox, "run builders in a restricted environment")
	sandboxPaths := make(map[string]string)
	c.Flags().Var(pathMapFlag(sandboxPaths), "sandbox-path", "`path` to allow in sandbox (can be passed multiple times)")
	implicitSystemDeps := new(stringSetFlag)
	c.Flags().Var(implicitSystemDeps, "implicit-system-dep", "`path` to always mount in sandbox (can be passed multiple times)")
	c.Flags().BoolVar(&opts.allowKeepFailed, "allow-keep-failed", true, "allow user to skip cleanup of failed builds")
	c.Flags().IntVar(&opts.coresPerBuild, "cores-per-build", runtime.NumCPU(), "hint to builders for `number` of concurrent jobs to run")
	c.Flags().DurationVar(&opts.buildLogRetention, "build-log-retention", 7*24*time.Hour, "`duration` before deleting finished build logs")
	c.Flags().StringVar(&opts.webListenAddress, "ui", "", "`address` to listen on for web UI (disabled by default)")
	c.Flags().BoolVar(&opts.allowRemoteWeb, "allow-remote-ui", false, "whether to accept non-localhost connections for UI")
	c.Flags().StringVar(&opts.templatesDirectory, "dev-templates", "", "`directory` to use for templates")
	c.Flag("dev-templates").Hidden = true
	c.Flags().StringVar(&opts.staticDirectory, "dev-static", "", "`directory` to use for static assets")
	c.Flag("dev-static").Hidden = true
	c.RunE = func(cmd *cobra.Command, args []string) error {
		opts.sandboxPaths = combineSandboxPathsAndImplicitDeps(sandboxPaths, implicitSystemDeps.set)
		return runServe(cmd.Context(), g, opts)
	}
	return c
}

func runServe(ctx context.Context, g *globalConfig, opts *serveOptions) error {
	if !g.storeDir.IsNative() {
		return fmt.Errorf("%s cannot be used on this system", g.storeDir)
	}
	if opts.sandbox && !backend.CanSandbox() {
		if !backend.SystemSupportsSandbox() {
			return fmt.Errorf("sandboxing requested but not supported on %v", system.Current())
		}
		return fmt.Errorf("sandboxing requested but unable to use (are you running with admin privileges?)")
	}
	keyring, err := readKeyringFromFiles(opts.keyFiles)
	if err != nil {
		return err
	}
	storeDirGroupID, buildUsers, err := buildUsersForGroup(ctx, opts.buildUsersGroup)
	if err != nil {
		return err
	}
	if err := ensureStoreDirectory(string(g.storeDir), storeDirGroupID); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(g.storeSocket), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(opts.dbPath), 0o755); err != nil {
		return err
	}
	// TODO(someday): Properly set permissions on the created database.
	webHandler := new(webServer)
	if opts.templatesDirectory != "" {
		root, err := os.OpenRoot(opts.templatesDirectory)
		if err != nil {
			return err
		}
		webHandler.templateFiles = root.FS()
	} else {
		webHandler.templateFiles = ui.TemplateFiles()
	}
	if opts.staticDirectory != "" {
		root, err := os.OpenRoot(opts.staticDirectory)
		if err != nil {
			return err
		}
		webHandler.staticAssets = root.FS()
	} else {
		webHandler.staticAssets = ui.StaticAssets()
	}

	var l net.Listener
	if runtime.GOOS == "linux" && opts.systemdSocket {
		listeners, err := activation.Listeners()
		if err != nil {
			return err
		}
		if len(listeners) != 1 {
			return fmt.Errorf("systemd passed in %d sockets (want 1)", len(listeners))
		}
		l = listeners[0]
	} else {
		var err error
		l, err = listenUnix(g.storeSocket)
		if err != nil {
			return err
		}
		defer func() {
			if err := os.Remove(g.storeSocket); err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Warnf(ctx, "Failed to clean up socket: %v", err)
			}
		}()
	}

	grp, grpCtx := errgroup.WithContext(ctx)

	openConns := make(sets.Set[net.Conn])
	var openConnsMu sync.Mutex
	grp.Go(func() error {
		// Once the context is Done, refuse new connections and RPCs.
		<-grpCtx.Done()
		log.Infof(grpCtx, "Shutting down (signal received)...")

		if err := l.Close(); err != nil {
			log.Errorf(grpCtx, "Closing Unix socket: %v", err)
		}
		openConnsMu.Lock()
		for conn := range openConns.All() {
			if err := closeRead(conn); err != nil {
				log.Errorf(grpCtx, "Closing Unix socket: %v", err)
			}
		}
		openConnsMu.Unlock()
		return nil
	})

	log.Infof(ctx, "Listening on %s", g.storeSocket)
	backendServer := backend.NewServer(g.storeDir, opts.dbPath, &backend.Options{
		BuildDirectory:              opts.buildDir,
		LogDirectory:                opts.logDir,
		ContentAddressBufferCreator: bytebuffer.TempFileCreator{Pattern: contentAddressTempFilePattern},
		SandboxPaths:                opts.sandboxPaths,
		DisableSandbox:              !opts.sandbox,
		BuildUsers:                  buildUsers,
		AllowKeepFailed:             opts.allowKeepFailed,
		CoresPerBuild:               opts.coresPerBuild,
		BuildLogRetention:           opts.buildLogRetention,
		Keyring:                     keyring,
	})
	defer func() {
		if err := backendServer.Close(); err != nil {
			log.Errorf(ctx, "%v", err)
		}
	}()
	webHandler.backend = backendServer

	grp.Go(func() error {
		for {
			conn, err := l.Accept()
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			if err != nil {
				return err
			}
			openConnsMu.Lock()
			openConns.Add(conn)
			openConnsMu.Unlock()

			grp.Go(func() error {
				recv := backendServer.NewNARReceiver(grpCtx, bytebuffer.TempFileCreator{
					Pattern: "zb-serve-receive-*.nar",
				})
				defer recv.Cleanup(grpCtx)

				codec := zbstorerpc.NewCodec(nopCloser{conn}, &zbstorerpc.CodecOptions{
					Importer: zbstorerpc.NewReceiverImporter(recv),
				})
				jsonrpc.Serve(backend.WithExporter(grpCtx, codec), codec, backendServer)
				codec.Close()

				openConnsMu.Lock()
				openConns.Delete(conn)
				openConnsMu.Unlock()

				if err := conn.Close(); err != nil {
					log.Errorf(grpCtx, "%v", err)
				}
				return nil
			})
		}
	})

	if opts.webListenAddress != "" {
		grp.Go(func() error {
			httpServer := &http.Server{
				Addr:    opts.webListenAddress,
				Handler: webHandler,
				BaseContext: func(l net.Listener) context.Context {
					return grpCtx
				},
				ErrorLog: zstdlog.New(log.Default(), &zstdlog.Options{
					Context: grpCtx,
					Level:   log.Error,
				}),

				ReadTimeout:       60 * time.Second,
				ReadHeaderTimeout: 30 * time.Second,
				WriteTimeout:      60 * time.Second,
			}
			if !opts.allowRemoteWeb {
				httpServer.Handler = localOnlyMiddleware{httpServer.Handler}
			}

			err := runhttp.Serve(grpCtx, httpServer, &runhttp.Options{
				OnStartup: func(ctx context.Context, addr net.Addr) {
					addrString := addr.String()
					if parsed, err := xnet.HostPortToIP(addrString, netip.Addr{}); err != nil {
						log.Debugf(ctx, "Invalid listen address: %v", err)
					} else {
						port := strconv.Itoa(int(parsed.Port()))
						if host := parsed.Addr(); host.IsLoopback() || host.IsUnspecified() {
							addrString = net.JoinHostPort("localhost", port)
						}
					}
					log.Infof(ctx, "Listening for HTTP on http://%s", addrString)
				},
			})
			if err == nil {
				err = net.ErrClosed
			}
			return err
		})
	}

	waitError := grp.Wait()
	if errors.Is(waitError, net.ErrClosed) {
		waitError = nil
	}
	return waitError
}

func ensureStoreDirectory(path string, gid int) error {
	if err := os.MkdirAll(filepath.Dir(string(path)), 0o755); err != nil {
		return err
	}
	const mode os.FileMode = 0o775 | os.ModeSticky
	if err := os.Mkdir(path, mode); err != nil {
		if errors.Is(err, os.ErrExist) {
			err = nil
		}
		return err
	}
	// Run an extra chmod to bypass umask.
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	if gid == -1 || gid == os.Getegid() {
		return nil
	}
	if err := os.Chown(path, -1, gid); err != nil {
		return err
	}
	return nil
}

func buildUsersForGroup(ctx context.Context, name string) (gid int, buildUsers []backend.BuildUser, err error) {
	if name == "" {
		return -1, nil, nil
	}
	if runtime.GOOS == "windows" {
		return -1, nil, fmt.Errorf("cannot set --build-users-group on Windows")
	}
	g, userNames, err := osutil.LookupGroup(ctx, name)
	if err != nil {
		return -1, nil, err
	}
	gid, err = strconv.Atoi(g.Gid)
	if err != nil {
		return -1, nil, fmt.Errorf("build users group id: %v", err)
	}
	log.Debugf(ctx, "Using build group %s (gid=%d), users=%v", name, gid, userNames)
	for _, userName := range userNames {
		u, err := user.Lookup(userName)
		if err != nil {
			return gid, nil, fmt.Errorf("build users group: %v", err)
		}
		var buildUser backend.BuildUser
		buildUser.UID, err = strconv.Atoi(u.Uid)
		if err != nil {
			return gid, nil, fmt.Errorf("build users group: user %s: user id: %v", userName, err)
		}
		buildUser.GID, err = strconv.Atoi(u.Gid)
		if err != nil {
			return gid, nil, fmt.Errorf("build users group: user %s: group id: %v", userName, err)
		}
		buildUsers = append(buildUsers, buildUser)
	}
	return gid, buildUsers, nil
}

func combineSandboxPathsAndImplicitDeps(sandboxPaths map[string]string, implicitDeps sets.Set[string]) map[string]backend.SandboxPath {
	result := make(map[string]backend.SandboxPath)
	for mappedPath, hostPath := range sandboxPaths {
		result[mappedPath] = backend.SandboxPath{Path: hostPath}
	}
	for path := range implicitDeps {
		opts := result[path]
		opts.AlwaysPresent = true
		result[path] = opts
	}
	return result
}

func listenUnix(path string) (*net.UnixListener, error) {
	laddr := &net.UnixAddr{
		Net:  "unix",
		Name: path,
	}
	l, err := net.ListenUnix(laddr.Net, laddr)
	if err != nil {
		return nil, err
	}

	// TODO(soon): Restrict to a group.
	if err := os.Chmod(path, 0o777); err != nil {
		l.Close()
		return nil, err
	}

	return l, nil
}

func closeRead(c net.Conn) error {
	cr, ok := c.(interface{ CloseRead() error })
	if !ok {
		return fmt.Errorf("%T does not support uni-directional close", c)
	}
	return cr.CloseRead()
}

type pathMapFlag map[string]string

func (f pathMapFlag) Type() string {
	return "string"
}

func (f pathMapFlag) String() string {
	sb := new(strings.Builder)
	first := true
	for k, v := range xmaps.Sorted(f) {
		if first {
			first = false
		} else {
			sb.WriteString(" ")
		}
		sb.WriteString(k)
		if k != v || strings.Contains(k, "=") || strings.Contains(v, "=") {
			sb.WriteString("=")
			sb.WriteString(v)
		}
	}
	return sb.String()
}

func (f pathMapFlag) Get() any {
	return map[string]string(f)
}

func (f pathMapFlag) Set(s string) error {
	for _, word := range strings.Fields(s) {
		k, v, isMap := strings.Cut(word, "=")
		if !isMap {
			v = k
		}
		f[k] = v
	}
	return nil
}

type nopCloser struct {
	io.ReadWriter
}

func (nopCloser) Close() error {
	return nil
}
