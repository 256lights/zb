// Copyright 2025 The zb Authors
// Copyright 2018 The Go Authors. All rights reserved.
// SPDX-License-Identifier: BSD 3-Clause
//
// This is a modified copy of https://cs.opensource.google/go/go/+/refs/tags/go1.24.1:src/os/removeall_at.go

//go:build linux || darwin

package osutil

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/sys/unix"
)

func removeAll(path string) error {
	// The rmdir system call does not permit removing ".",
	// so we don't permit it either.
	if path == "." || (len(path) >= 2 && path[len(path)-1] == '.' && os.IsPathSeparator(path[len(path)-2])) {
		return &os.PathError{
			Op:   "RemoveAll",
			Path: path,
			Err:  unix.EINVAL,
		}
	}

	if err := ensureNotMounted(path, false); err != nil {
		return err
	}

	// Simple case: if Remove works, we're done.
	err := os.Remove(path)
	if err == nil || os.IsNotExist(err) {
		return nil
	}

	// RemoveAll recurses by deleting the path base from
	// its parent directory
	parentDir := filepath.Dir(path)
	base := filepath.Base(path)

	parent, err := os.Open(parentDir)
	if errors.Is(err, os.ErrNotExist) {
		// If parent does not exist, base cannot exist. Fail silently.
		return nil
	}
	if err != nil {
		return err
	}
	defer parent.Close()

	return removeAllFrom(parent, parentDir, base)
}

func removeAllFrom(parent *os.File, parentPath, base string) error {
	parentFD := int(parent.Fd())
	fullPath := parentPath + string(os.PathSeparator) + base

	if err := ensureNotMounted(fullPath, false); err != nil {
		return err
	}
	// Simple case: Unlink removes it.
	err := ignoringEINTR(func() error {
		return unix.Unlinkat(parentFD, base, 0)
	})
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}

	// EISDIR means that we have a directory, and we need to
	// remove its contents.
	// EPERM or EACCES means that we don't have write permission on
	// the parent directory, but this entry might still be a directory
	// whose contents need to be removed.
	// Otherwise just return the error.
	if !errors.Is(err, unix.EISDIR) && !errors.Is(err, unix.EPERM) && !errors.Is(err, unix.EACCES) {
		return &os.PathError{
			Op:   "unlinkat",
			Path: fullPath,
			Err:  err,
		}
	}
	unlinkError := err

	var recurseError error
	for {
		const reqSize = 1024
		var respSize int

		file, err := openDirAt(parentFD, base)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.ELOOP) {
				// Not a directory; return the error from the unix.Unlinkat.
				return &os.PathError{
					Op:   "unlinkat",
					Path: fullPath,
					Err:  unlinkError,
				}
			}
			recurseError = &os.PathError{
				Op:   "openfdat",
				Path: fullPath,
				Err:  err,
			}
			break
		}

		for {
			numErr := 0

			names, readErr := file.Readdirnames(reqSize)
			// Errors other than EOF should stop us from continuing.
			if readErr != nil && readErr != io.EOF {
				file.Close()
				if errors.Is(readErr, os.ErrNotExist) {
					return nil
				}
				return &os.PathError{
					Op:   "readdirnames",
					Path: fullPath,
					Err:  readErr,
				}
			}

			respSize = len(names)
			for _, name := range names {
				err := removeAllFrom(file, fullPath, name)
				if err != nil {
					numErr++
					if recurseError == nil {
						recurseError = err
					}
				}
			}

			// If we can delete any entry, break to start new iteration.
			// Otherwise, we discard current names, get next entries and try deleting them.
			if numErr != reqSize {
				break
			}
		}

		// Removing files from the directory may have caused
		// the OS to reshuffle it. Simply calling Readdirnames
		// again may skip some entries. The only reliable way
		// to avoid this is to close and re-open the
		// directory. See https://go.dev/issue/20841.
		file.Close()

		// Finish when the end of the directory is reached
		if respSize < reqSize {
			break
		}
	}

	// Remove the directory itself.
	unlinkError = ignoringEINTR(func() error {
		return unix.Unlinkat(parentFD, base, unix.AT_REMOVEDIR)
	})
	runtime.KeepAlive(parent)
	if unlinkError == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if recurseError != nil {
		return recurseError
	}
	return &os.PathError{
		Op:   "unlinkat",
		Path: fullPath,
		Err:  unlinkError,
	}
}

func ensureNotMounted(path string, unmountContents bool) error {
	unmountError := ignoringEINTR(func() error {
		return unix.Unmount(path, UnmountNoFollow)
	})
	isMountpoint := true
	switch {
	case unmountError == nil || errors.Is(unmountError, os.ErrNotExist):
		return nil
	case errors.Is(unmountError, unix.EINVAL):
		// We have permission to unmount, but path does not name a mount point.
		if !unmountContents {
			return nil
		}
		isMountpoint = false
	case !errors.Is(unmountError, unix.EBUSY):
		return &os.PathError{
			Op:   "umount",
			Path: path,
			Err:  unmountError,
		}
	}

	// We either hit EBUSY (might mean there are nested mountpoints)
	// or a parent path did.
	// Try opening the path as a directory and ensure its contents are unmounted.
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if errors.Is(err, unix.ENOTDIR) {
			if !isMountpoint {
				return nil
			}
			// If it's not a directory, return the original error.
			err = unmountError
		}
		return &os.PathError{
			Op:   "umount",
			Path: path,
			Err:  err,
		}
	}
	for {
		names, err := file.Readdirnames(1024)
		if err != nil && err != io.EOF {
			file.Close()
			return &os.PathError{
				Op:   "readdirnames",
				Path: path,
				Err:  err,
			}
		}
		for _, name := range names {
			if err := ensureNotMounted(path+string(os.PathSeparator)+name, true); err != nil {
				file.Close()
				return err
			}
		}
		if err != nil {
			file.Close()
			break
		}
	}

	// Contents should be unmounted now.
	// Retry unmount if this was originally a mount point.
	if !isMountpoint {
		return nil
	}
	unmountError = ignoringEINTR(func() error {
		return unix.Unmount(path, UnmountNoFollow)
	})
	if unmountError != nil && !errors.Is(unmountError, os.ErrNotExist) && !errors.Is(unmountError, unix.EINVAL) {
		return &os.PathError{
			Op:   "umount",
			Path: path,
			Err:  unmountError,
		}
	}
	return nil
}

// openDirAt opens a directory name relative to the directory referred to by
// the file descriptor dirfd. If name is anything but a directory (this
// includes a symlink to one), it should return an error. Other than that this
// should act like openFileNolog.
//
// This acts like openFileNolog rather than OpenFile because
// we are going to (try to) remove the file.
// The contents of this file are not relevant for test caching.
func openDirAt(dirfd int, name string) (*os.File, error) {
	r, err := ignoringEINTR2(func() (int, error) {
		return unix.Openat(dirfd, name, os.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	})
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(r), name), nil
}
