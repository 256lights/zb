// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package osutil provides convenience functions for working with the local filesystem.
package osutil

import (
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"runtime"
)

// MkdirPerm creates a new directory with the given permission bits (after umask).
func MkdirPerm(name string, perm os.FileMode) error {
	if err := os.Mkdir(name, perm); err != nil {
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		return err
	}
	return nil
}

// WriteFilePerm writes data to the named file, creating it if necessary,
// and ensuring it has the given permissions (after umask).
func WriteFilePerm(name string, data []byte, perm os.FileMode) error {
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

// FirstPresentFile returns the first path in the sequence that exists in the filesystem,
// or an error if no path could be found.
func FirstPresentFile(paths iter.Seq[string]) (string, error) {
	var firstError, firstUnexpectedError error
	for path := range paths {
		_, err := os.Lstat(path)
		switch {
		case err == nil:
			return path, nil
		case !errors.Is(err, os.ErrNotExist):
			if firstUnexpectedError == nil {
				firstUnexpectedError = err
			}
		default:
			if firstError == nil {
				firstError = err
			}
		}
	}
	if firstUnexpectedError != nil {
		return "", firstUnexpectedError
	}
	if firstError == nil {
		firstError = errors.New("no files searched")
	}
	return "", firstError
}

const (
	rootUID = 0
	rootGID = 0
)

// MakePublicReadOnly removes any write permissions on the filesystem object at the given path
// and adds read permissions for all users.
// If the path names a directory,
// then this applies recursively to any filesystem objects in the directory.
//
// If onError is not nil, it will be used to handle any errors encountered.
// Its return value is handled in the same manner as in [io/fs.WalkDirFunc].
func MakePublicReadOnly(path string, onError func(error) error) error {
	if onError == nil {
		onError = func(err error) error { return err }
	}
	return filepath.WalkDir(path, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return onError(err)
		}

		existingMode := os.FileMode(0o666)
		if runtime.GOOS != "windows" {
			info, err := entry.Info()
			if err != nil {
				return onError(err)
			}
			const permMask = os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky | os.ModeAppend | os.ModeExclusive | os.ModeTemporary
			existingMode = info.Mode() & permMask
		}

		newMode := (existingMode | 0o444) &^ 0o222 // +r-w
		if entry.IsDir() || existingMode&0o111 != 0 {
			newMode |= 0o111 // +x
		}
		if err := os.Chmod(path, newMode); err != nil {
			return onError(err)
		}

		if IsRoot() {
			if err := os.Chown(path, rootUID, rootGID); err != nil {
				return onError(err)
			}
		}

		return nil
	})
}
