// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package backend

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"iter"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
	"zb.256lights.llc/pkg/internal/osutil"
	"zb.256lights.llc/pkg/internal/xmaps"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
)

func runSandboxed(ctx context.Context, invocation *builderInvocation) error {
	inputs := make(sets.Set[zbstore.Path])
	for inputPath := range invocation.derivation.InputSources.Values() {
		err := invocation.closure(inputPath, func(path zbstore.Path) bool {
			inputs.Add(path)
			return true
		})
		if err != nil {
			return err
		}
	}
	for input := range invocation.derivation.InputDerivationOutputs() {
		inputPath, ok := invocation.lookup(input)
		if !ok {
			return fmt.Errorf("missing store path for %v", input)
		}
		err := invocation.closure(inputPath, func(path zbstore.Path) bool {
			inputs.Add(path)
			return true
		})
		if err != nil {
			return err
		}
	}
	// If any of the sandbox paths reference a store path,
	// then add the store object's closure as an input.
	for _, hostPath := range invocation.sandboxPaths {
		hostStorePath, _, err := invocation.derivation.Dir.ParsePath(hostPath)
		if err != nil {
			continue
		}
		err = invocation.closure(hostStorePath, func(path zbstore.Path) bool {
			inputs.Add(path)
			return true
		})
		if err != nil {
			return err
		}
	}

	caFile, err := defaultSystemCertFile()
	if err != nil {
		return err
	}

	// Create the chroot directory inside the store
	// so we can rename the outputs to their expected locations.
	chrootDir := filepath.Join(invocation.realStoreDir, invocation.derivationPath.Base()+".chroot")
	if err := os.Mkdir(chrootDir, 0o755); err != nil {
		return err
	}
	defer func() {
		if err := osutil.UnmountAndRemoveAll(chrootDir); err != nil {
			log.Errorf(ctx, "Failed to clean up: %v", err)
		}
	}()

	const workDir = "/build"
	opts := &linuxSandboxOptions{
		storeDir:     invocation.derivation.Dir,
		realStoreDir: invocation.realStoreDir,
		workDir:      workDir,
		realWorkDir:  invocation.buildDir,
		inputs:       inputs,
		extra:        invocation.sandboxPaths,

		builderUID: os.Geteuid(),
		builderGID: os.Getegid(),

		network: invocation.derivation.Outputs[zbstore.DefaultDerivationOutputName].IsFixed(),
		caFile:  caFile,
		// TODO(maybe): This seems high to me.
		shmSize: "50%",
	}
	if invocation.user != nil {
		opts.builderUID = invocation.user.UID
		opts.builderGID = invocation.user.GID
	}
	cleanupMounts, err := setupSandboxFilesystem(ctx, chrootDir, opts)
	if err != nil {
		return err
	}
	defer cleanupMounts()

	c := exec.CommandContext(ctx, invocation.derivation.Builder, invocation.derivation.Args...)
	setCancelFunc(c)
	env := maps.Clone(invocation.derivation.Env)
	fillBaseEnv(env, invocation.derivation.Dir, workDir)
	for k, v := range xmaps.Sorted(env) {
		c.Env = append(c.Env, k+"="+v)
	}
	c.Dir = workDir
	c.Stdout = invocation.logWriter
	c.Stderr = invocation.logWriter
	c.SysProcAttr = sysProcAttrForUser(invocation.user)
	if c.SysProcAttr == nil {
		c.SysProcAttr = new(syscall.SysProcAttr)
	}
	c.SysProcAttr.Chroot = chrootDir

	if err := c.Run(); err != nil {
		return builderFailure{err}
	}

	for _, outputPath := range invocation.outputPaths {
		src := filepath.Join(chrootDir, string(outputPath))
		dst := filepath.Join(invocation.realStoreDir, outputPath.Base())
		if err := os.Rename(src, dst); err != nil {
			return err
		}
	}

	return nil
}

type linuxSandboxOptions struct {
	storeDir     zbstore.Directory
	realStoreDir string
	inputs       sets.Set[zbstore.Path]

	workDir     string
	realWorkDir string

	extra map[string]string

	builderUID int
	builderGID int

	network bool
	caFile  string
	shmSize string
}

func setupSandboxFilesystem(ctx context.Context, dir string, opts *linuxSandboxOptions) (cleanupMounts func(), err error) {
	log.Debugf(ctx, "Creating sandbox at %s...", dir)
	var mounts []string
	// Separate variable so named return does not clobber in defer.
	doCleanupMounts := func() {
		for i := range mounts {
			// Unmount in reverse order of creation.
			m := mounts[len(mounts)-1-i]

			log.Debugf(ctx, "umount %s", m)
			if err := unix.Unmount(m, osutil.UnmountNoFollow); err != nil {
				log.Errorf(ctx, "Failed to unmount %s during cleanup: %v", m, err)
			}
		}
		mounts = nil
	}
	defer func() {
		if err != nil {
			err = fmt.Errorf("create sandbox in %s: %v", dir, err)
			doCleanupMounts()
		}
	}()

	exists := func(path string) bool {
		_, err := os.Lstat(path)
		return err == nil
	}
	doBindMount := func(ctx context.Context, oldname, newname string) error {
		isMount, err := bindMount(ctx, oldname, newname)
		if isMount {
			mounts = append(mounts, newname)
		}
		return err
	}

	if !opts.storeDir.IsNative() {
		return nil, fmt.Errorf("using non-native store %s", opts.storeDir)
	}

	if err := osutil.MkdirPerm(filepath.Join(dir, "tmp"), 0o777|os.ModeSticky); err != nil {
		return nil, err
	}
	workDir := filepath.Join(dir, opts.workDir)
	if err := doBindMount(ctx, opts.realWorkDir, workDir); err != nil {
		return nil, err
	}

	etcDir := filepath.Join(dir, "etc")
	if err := os.Mkdir(etcDir, 0o755); err != nil {
		return nil, err
	}
	if err := osutil.WriteFilePerm(filepath.Join(etcDir, "passwd"), sandboxPasswd(opts.builderUID, opts.builderGID), 0o444); err != nil {
		return nil, err
	}
	if err := osutil.WriteFilePerm(filepath.Join(etcDir, "group"), sandboxGroup(opts.builderGID), 0o444); err != nil {
		return nil, err
	}
	const hostsContent = "127.0.0.1 localhost\n::1 localhost\n"
	if err := osutil.WriteFilePerm(filepath.Join(etcDir, "hosts"), []byte(hostsContent), 0o444); err != nil {
		return nil, err
	}
	if opts.network {
		const nsswitchContent = "hosts: files dns\nservices: files\n"
		if err := osutil.WriteFilePerm(filepath.Join(etcDir, "nsswitch.conf"), []byte(nsswitchContent), 0o444); err != nil {
			return nil, err
		}
		for newname, oldname := range linuxNetworkBindMounts(etcDir, opts) {
			if err := doBindMount(ctx, oldname, newname); err != nil {
				return nil, err
			}
		}
	}
	if err := os.Chmod(etcDir, 0o555); err != nil {
		return nil, err
	}

	devDir := filepath.Join(dir, "dev")
	if err := osutil.MkdirPerm(devDir, 0o755); err != nil {
		return nil, err
	}
	if exists("/dev/shm") {
		shmDir := filepath.Join(devDir, "shm")
		if err := osutil.MkdirPerm(shmDir, 0o755); err != nil {
			return nil, err
		}
		if opts.shmSize != "" {
			mountOpts := "size=" + opts.shmSize
			log.Debugf(ctx, "mount -t tmpfs -o %s none %s", mountOpts, shmDir)
			err := unix.Mount("none", shmDir, "tmpfs", 0, mountOpts)
			if err != nil {
				return nil, err
			}
			mounts = append(mounts, shmDir)
		}
	}

	ptsDir := filepath.Join(devDir, "pts")
	if err := osutil.MkdirPerm(ptsDir, 0o755); err != nil {
		return nil, err
	}
	if exists("/dev/pts/ptmx") {
		ptmxPath := filepath.Join(devDir, "ptmx")
		const devptsMountOpts = "newinstance,mode=0620"
		log.Debugf(ctx, "mount -t devpts -o %s none %s", devptsMountOpts, ptsDir)
		err := unix.Mount("none", ptsDir, "devpts", 0, devptsMountOpts)
		switch {
		case err == nil:
			mounts = append(mounts, ptsDir)
			if err := os.Symlink("/dev/pts/ptmx", ptmxPath); err != nil {
				return nil, err
			}
			// Make sure /dev/pts/ptmx is world-writable.
			// With some Linux versions, it is created with permissions 0.
			if err := os.Chmod(filepath.Join(ptsDir, "ptmx"), 0o666); err != nil {
				return nil, err
			}
		case errors.Is(err, unix.EINVAL):
			// Fallback: bind-mount /dev/pts and /dev/ptmx.
			log.Debugf(ctx, "Failed to mount devpts at %s, falling back to bind mounts... (%v)", ptsDir, err)
			if err := doBindMount(ctx, "/dev/pts", ptsDir); err != nil {
				return nil, err
			}

			if err := doBindMount(ctx, "/dev/ptmx", ptmxPath); err != nil {
				return nil, err
			}
		default:
			return nil, err
		}
	}

	for newname, oldname := range linuxDeviceBindMounts(devDir) {
		if err := doBindMount(ctx, oldname, newname); err != nil {
			return nil, err
		}
	}
	for newname, oldname := range linuxDeviceSymlinks(devDir) {
		if err := os.Symlink(oldname, newname); err != nil {
			return nil, err
		}
	}

	procDir := filepath.Join(dir, "proc")
	if err := osutil.MkdirPerm(procDir, 0o755); err != nil {
		return nil, err
	}
	if err := unix.Mount("none", procDir, "proc", 0, ""); err != nil {
		return nil, err
	}
	mounts = append(mounts, procDir)

	// Create writable store directory.
	storeDir := filepath.Join(dir, string(opts.storeDir))
	if err := os.MkdirAll(filepath.Dir(storeDir), 0o755); err != nil {
		return nil, err
	}
	if err := osutil.MkdirPerm(storeDir, 0o775|os.ModeSticky); err != nil {
		return nil, err
	}
	if err := os.Chown(storeDir, opts.builderUID, opts.builderGID); err != nil {
		return nil, err
	}
	// Bind-mount input paths.
	for input := range opts.inputs {
		if inputDir := input.Dir(); inputDir != opts.storeDir {
			return nil, fmt.Errorf("input %s is not inside %s", input, opts.storeDir)
		}
		dst := filepath.Join(dir, string(input))
		if err := doBindMount(ctx, filepath.Join(opts.realStoreDir, input.Base()), dst); err != nil {
			return nil, err
		}
	}

	// Bind-mount requested extras.
	for sandboxPath, hostPath := range opts.extra {
		dst := filepath.Join(dir, sandboxPath)
		if err := doBindMount(ctx, hostPath, dst); err != nil {
			return nil, err
		}
	}

	log.Debugf(ctx, "Created sandbox at %s", dir)
	return doCleanupMounts, nil
}

func sandboxPasswd(builderUID, builderGID int) []byte {
	buf := new(bytes.Buffer)
	buf.WriteString("root:x:0:0:Nix build user:/build:/noshell\n")
	if builderUID != 0 {
		fmt.Fprintf(buf, "%s:x:%d:%d:zb build user:/build:/noshell\n", DefaultBuildUsersGroup, builderUID, builderGID)
	}
	buf.WriteString("nobody:x:65534:65534:Nobody:/:/noshell\n")
	return buf.Bytes()
}

func sandboxGroup(builderGID int) []byte {
	buf := new(bytes.Buffer)
	buf.WriteString("root:x:0:\n")
	if builderGID != 0 {
		fmt.Fprintf(buf, "%s:!:%d:\n", DefaultBuildUsersGroup, builderGID)
	}
	buf.WriteString("nogroup:x:65534:\n")
	return buf.Bytes()
}

// bindMount creates a bind mount of oldname at newname,
// creating any parent directories of newname that do not exist.
// bindMount will return an error for which errors.Is(err, [os.ErrNotExist]) reports true
// if oldname does not exist.
// isMount is true if and only if a bind mount was created at newname.
//
// If oldname references a symlink, an equivalent symlink will be created
// instead of creating a bind mount
// and isMount will be false.
// Symlinks cannot be bind-mounted, so recreating the symlink is the best that can be done.
func bindMount(ctx context.Context, oldname, newname string) (isMount bool, err error) {
	info, err := os.Lstat(oldname)
	if err != nil {
		return false, fmt.Errorf("bind mount %s to %s: %w", oldname, newname, err)
	}

	switch info.Mode().Type() {
	case os.ModeDir:
		if err := os.MkdirAll(newname, 0o777); err != nil {
			return false, fmt.Errorf("bind mount %s to %s: %v", oldname, newname, err)
		}
		log.Debugf(ctx, "mount --rbind %s %s", oldname, newname)
		if err := unix.Mount(oldname, newname, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
			return false, fmt.Errorf("bind mount %s to %s: %v", oldname, newname, err)
		}
	case os.ModeSymlink:
		if err := os.MkdirAll(filepath.Dir(newname), 0o777); err != nil {
			return false, fmt.Errorf("bind mount %s to %s: %v", oldname, newname, err)
		}
		target, err := os.Readlink(oldname)
		if err != nil {
			return false, fmt.Errorf("bind mount %s to %s: %v", oldname, newname, err)
		}
		log.Debugf(ctx, "ln -s %s %s", target, newname)
		if err := os.Symlink(target, newname); err != nil {
			return false, fmt.Errorf("bind mount %s to %s: %v", oldname, newname, err)
		}
		return false, nil
	default:
		if err := os.MkdirAll(filepath.Dir(newname), 0o777); err != nil {
			return false, fmt.Errorf("bind mount %s to %s: %v", oldname, newname, err)
		}
		if err := os.WriteFile(newname, nil, 0o666); err != nil {
			return false, fmt.Errorf("bind mount %s to %s: %v", oldname, newname, err)
		}
		log.Debugf(ctx, "mount --rbind %s %s", oldname, newname)
		if err := unix.Mount(oldname, newname, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
			return false, fmt.Errorf("bind mount %s to %s: %v", oldname, newname, err)
		}
	}

	return true, nil
}

func linuxNetworkBindMounts(etcDir string, opts *linuxSandboxOptions) iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		if !yield(filepath.Join(etcDir, "resolv.conf"), "/etc/resolv.conf") {
			return
		}
		if !yield(filepath.Join(etcDir, "services"), "/etc/services") {
			return
		}
		if !yield(filepath.Join(etcDir, "hosts"), "/etc/hosts") {
			return
		}
		if opts.caFile != "" {
			if _, err := os.Lstat(opts.caFile); err == nil {
				if !yield(filepath.Join(etcDir, "ssl", "certs", "ca-certificates.crt"), opts.caFile) {
					return
				}
			}
		}
	}
}

func defaultSystemCertFile() (string, error) {
	if path := os.Getenv("SSL_CERT_FILE"); path != "" {
		return path, nil
	}

	paths := iter.Seq[string](func(yield func(string) bool) {
		// Debian/Ubuntu/Gentoo etc.
		if !yield("/etc/ssl/certs/ca-certificates.crt") {
			return
		}
		// Fedora/RHEL 6
		if !yield("/etc/pki/tls/certs/ca-bundle.crt") {
			return
		}
		// OpenSUSE
		if !yield("/etc/ssl/ca-bundle.pem") {
			return
		}
		// OpenELEC
		if !yield("/etc/pki/tls/cacert.pem") {
			return
		}
		// CentOS/RHEL 7
		if !yield("/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem") {
			return
		}
		// Alpine Linux
		if !yield("/etc/ssl/cert.pem") {
			return
		}
	})
	return osutil.FirstPresentFile(paths)
}

func linuxDeviceBindMounts(devDir string) iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		if !yield(filepath.Join(devDir, "full"), "/dev/full") {
			return
		}
		if !yield(filepath.Join(devDir, "null"), "/dev/null") {
			return
		}
		if !yield(filepath.Join(devDir, "random"), "/dev/random") {
			return
		}
		if !yield(filepath.Join(devDir, "tty"), "/dev/tty") {
			return
		}
		if !yield(filepath.Join(devDir, "urandom"), "/dev/urandom") {
			return
		}
		if !yield(filepath.Join(devDir, "zero"), "/dev/zero") {
			return
		}
	}
}

func linuxDeviceSymlinks(devDir string) iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		if !yield(filepath.Join(devDir, "fd"), "/proc/self/fd") {
			return
		}
		if !yield(filepath.Join(devDir, "stdin"), "/proc/self/fd/0") {
			return
		}
		if !yield(filepath.Join(devDir, "stdout"), "/proc/self/fd/1") {
			return
		}
		if !yield(filepath.Join(devDir, "stderr"), "/proc/self/fd/2") {
			return
		}
	}
}
