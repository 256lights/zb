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

	"golang.org/x/sys/unix"
	"zb.256lights.llc/pkg/internal/xmaps"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
)

func runSandboxed(ctx context.Context, opts *builderInvocation) error {
	inputs := make(sets.Set[zbstore.Path])
	inputs.AddSeq(opts.derivation.InputSources.Values())
	for input := range opts.derivation.InputDerivationOutputs() {
		inputPath, ok := opts.lookup(input)
		if !ok {
			return fmt.Errorf("missing store path for %v", input)
		}
		err := opts.closure(inputPath, func(path zbstore.Path) bool {
			inputs.Add(path)
			return true
		})
		if err != nil {
			return err
		}
	}
	// If any of the sandbox paths reference a store path,
	// then add the store object's closure as an input.
	for _, hostPath := range opts.sandboxPaths {
		hostStorePath, _, err := opts.derivation.Dir.ParsePath(hostPath)
		if err != nil {
			continue
		}
		err = opts.closure(hostStorePath, func(path zbstore.Path) bool {
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
	chrootDir := filepath.Join(opts.realStoreDir, opts.derivationPath.Base()+".chroot")
	if err := os.Mkdir(chrootDir, 0o775); err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(chrootDir); err != nil {
			log.Errorf(ctx, "Failed to clean up: %v", err)
		}
	}()

	const workDir = "/build"
	cleanupMounts, err := setupSandboxFilesystem(ctx, chrootDir, &linuxSandboxOptions{
		storeDir:     opts.derivation.Dir,
		realStoreDir: opts.realStoreDir,
		workDir:      workDir,
		realWorkDir:  opts.buildDir,
		inputs:       inputs,
		extra:        opts.sandboxPaths,

		// TODO(soon): Use separate UID/GID.
		builderUID: os.Geteuid(),
		builderGID: os.Getegid(),

		network: opts.derivation.Outputs[zbstore.DefaultDerivationOutputName].IsFixed(),
		caFile:  caFile,
		// TODO(maybe): This seems high to me.
		shmSize: "50%",
	})
	if err != nil {
		return err
	}
	defer cleanupMounts()

	c := exec.CommandContext(ctx, opts.derivation.Builder, opts.derivation.Args...)
	setCancelFunc(c)
	env := maps.Clone(opts.derivation.Env)
	fillBaseEnv(env, opts.derivation.Dir, workDir)
	for k, v := range xmaps.Sorted(env) {
		c.Env = append(c.Env, k+"="+v)
	}
	c.Dir = workDir
	c.Stdout = opts.logWriter
	c.Stderr = opts.logWriter
	c.SysProcAttr = &unix.SysProcAttr{
		Chroot: chrootDir,
		// TODO(soon): Use separate UID/GID.
	}

	if err := c.Run(); err != nil {
		return builderFailure{err}
	}

	for _, outputPath := range opts.outputPaths {
		src := filepath.Join(chrootDir, string(outputPath))
		dst := filepath.Join(opts.realStoreDir, outputPath.Base())
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
			if err := unix.Unmount(m, 0); err != nil {
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

	if !opts.storeDir.IsNative() {
		return nil, fmt.Errorf("using non-native store %s", opts.storeDir)
	}

	if err := mkdirPerm(filepath.Join(dir, "tmp"), 0o777|os.ModeSticky); err != nil {
		return nil, err
	}
	workDir := filepath.Join(dir, opts.workDir)
	isMount, err := bindMount(ctx, opts.realWorkDir, workDir)
	if err != nil {
		return nil, err
	}
	if isMount {
		mounts = append(mounts, workDir)
	}

	etcDir := filepath.Join(dir, "etc")
	if err := os.Mkdir(etcDir, 0o755); err != nil {
		return nil, err
	}
	if err := writeFilePerm(filepath.Join(etcDir, "passwd"), sandboxPasswd(opts.builderUID, opts.builderGID), 0o444); err != nil {
		return nil, err
	}
	if err := writeFilePerm(filepath.Join(etcDir, "group"), sandboxGroup(opts.builderGID), 0o444); err != nil {
		return nil, err
	}
	const hostsContent = "127.0.0.1 localhost\n::1 localhost\n"
	if err := writeFilePerm(filepath.Join(etcDir, "hosts"), []byte(hostsContent), 0o444); err != nil {
		return nil, err
	}
	if opts.network {
		const nsswitchContent = "hosts: files dns\nservices: files\n"
		if err := writeFilePerm(filepath.Join(etcDir, "nsswitch.conf"), []byte(nsswitchContent), 0o444); err != nil {
			return nil, err
		}
		for newname, oldname := range linuxNetworkBindMounts(etcDir, opts) {
			isMount, err := bindMount(ctx, oldname, newname)
			if err != nil {
				return nil, err
			}
			if isMount {
				mounts = append(mounts, newname)
			}
		}
	}
	if err := os.Chmod(etcDir, 0o555); err != nil {
		return nil, err
	}

	devDir := filepath.Join(dir, "dev")
	if err := mkdirPerm(devDir, 0o755); err != nil {
		return nil, err
	}
	if exists("/dev/shm") {
		shmDir := filepath.Join(devDir, "shm")
		if err := mkdirPerm(shmDir, 0o755); err != nil {
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
	if err := mkdirPerm(ptsDir, 0o755); err != nil {
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
			ptsIsMount, err := bindMount(ctx, "/dev/pts", ptsDir)
			if err != nil {
				return nil, err
			}
			if ptsIsMount {
				mounts = append(mounts, ptsDir)
			}

			ptmxIsMount, err := bindMount(ctx, "/dev/ptmx", ptmxPath)
			if err != nil {
				return nil, err
			}
			if ptmxIsMount {
				mounts = append(mounts, ptmxPath)
			}
		default:
			return nil, err
		}
	}

	for newname, oldname := range linuxDeviceBindMounts(devDir) {
		isMount, err := bindMount(ctx, oldname, newname)
		if err != nil {
			return nil, err
		}
		if isMount {
			mounts = append(mounts, newname)
		}
	}
	for newname, oldname := range linuxDeviceSymlinks(devDir) {
		if err := os.Symlink(oldname, newname); err != nil {
			return nil, err
		}
	}

	procDir := filepath.Join(dir, "proc")
	if err := mkdirPerm(procDir, 0o755); err != nil {
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
	if err := mkdirPerm(storeDir, 0o755|os.ModeSticky); err != nil {
		return nil, err
	}
	// Bind-mount input paths.
	for input := range opts.inputs {
		if inputDir := input.Dir(); inputDir != opts.storeDir {
			return nil, fmt.Errorf("input %s is not inside %s", input, opts.storeDir)
		}
		dst := filepath.Join(dir, string(input))
		isMount, err := bindMount(ctx, filepath.Join(opts.realStoreDir, input.Base()), dst)
		if err != nil {
			return nil, err
		}
		if isMount {
			mounts = append(mounts, dst)
		}
	}

	// Bind-mount requested extras.
	for sandboxPath, hostPath := range opts.extra {
		dst := filepath.Join(dir, sandboxPath)
		isMount, err := bindMount(ctx, hostPath, dst)
		if err != nil {
			return nil, err
		}
		if isMount {
			mounts = append(mounts, dst)
		}
	}

	log.Debugf(ctx, "Created sandbox at %s", dir)
	return doCleanupMounts, nil
}

func sandboxPasswd(builderUID, builderGID int) []byte {
	buf := new(bytes.Buffer)
	buf.WriteString("root:x:0:0:Nix build user:/build:/noshell\n")
	if builderUID != 0 {
		fmt.Fprintf(buf, "zbld:x:%d:%d:zb build user:/build:/noshell\n", builderUID, builderGID)
	}
	buf.WriteString("nobody:x:65534:65534:Nobody:/:/noshell\n")
	return buf.Bytes()
}

func sandboxGroup(builderGID int) []byte {
	buf := new(bytes.Buffer)
	buf.WriteString("root:x:0:\n")
	if builderGID != 0 {
		fmt.Fprintf(buf, "zbld:!:%d:\n", builderGID)
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

// mkdirPerm creates a new directory with the given mode bits.
// It ignores any umask.
func mkdirPerm(name string, perm os.FileMode) error {
	if err := os.Mkdir(name, perm); err != nil {
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		return err
	}
	return nil
}

// writeFilePerm writes data to the named file, creating it if necessary,
// and ensuring it has the given permissions (ignoring umask).
func writeFilePerm(name string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm|0o200)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %v", name, err)
	}
	err = f.Chmod(perm)
	err2 := f.Close()
	if err == nil {
		err = err2
	}
	if err != nil {
		return fmt.Errorf("write %s: %v", name, err)
	}
	return nil
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
	return firstPresentFile(paths)
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
