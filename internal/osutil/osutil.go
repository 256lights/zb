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
)

// MkdirAll creates a directory with the specified name,
// along with any necessary parents,
// and returns nil,
// or else returns an error.
// The permission bits parentsPerm (before umask)
// are used for any parent directories that MkdirAll creates;
// the permission bits perm (before umask)
// are used for creating the directory with the specified name.
// If path is already a directory, MkdirAll does nothing and returns nil.
//
// (This is mostly the same as [os.MkdirAll]
// with additional control over the parent directory permissions.)
func MkdirAll(name string, parentsPerm, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(name), parentsPerm); err != nil {
		return err
	}
	if err := os.Mkdir(name, perm); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return nil
}

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
