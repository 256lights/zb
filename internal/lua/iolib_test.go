// Copyright 2023 Roxy Light
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of
// this software and associated documentation files (the “Software”), to deal in
// the Software without restriction, including without limitation the rights to
// use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
// the Software, and to permit persons to whom the Software is furnished to do so,
// subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED “AS IS”, WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
// FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
// COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
// IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
// CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
//
// SPDX-License-Identifier: MIT

package lua

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIOLibrary(t *testing.T) {
	t.Run("Files", func(t *testing.T) {
		lib := NewIOLibrary()
		lib.Stdin = nil
		lib.Stdout = nil
		lib.Stderr = nil
		lib.OpenProcessReader = nil
		lib.OpenProcessWriter = nil

		dir := t.TempDir()
		origOpen := lib.Open
		lib.Open = func(name, mode string) (io.Closer, error) {
			cleaned := filepath.Clean(name)
			if cleaned == "." || cleaned == ".." || strings.ContainsRune(cleaned, filepath.Separator) {
				return nil, &os.PathError{
					Op:   "open",
					Path: name,
					Err:  os.ErrNotExist,
				}
			}
			f, err := origOpen(filepath.Join(dir, cleaned), mode)
			if e, ok := err.(*os.PathError); ok {
				e.Path = name
			}
			return f, err
		}

		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()
		out := new(strings.Builder)
		openBase := NewOpenBase(out, nil)
		if err := Require(state, GName, true, openBase); err != nil {
			t.Error(err)
		}
		if err := Require(state, IOLibraryName, true, lib.OpenLibrary); err != nil {
			t.Error(err)
		}

		f, err := os.Open(filepath.Join("testdata", "iolib.lua"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := state.Load(f, "@testdata/iolib.lua", "t"); err != nil {
			t.Fatal(err)
		}
		err = state.Call(0, 0, 0)
		if out.Len() > 0 {
			t.Log(out.String())
		}
		if err != nil {
			t.Error(err)
		}
	})

	t.Run("Popen", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Not running popen test on Windows")
		}
		wd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			os.Chdir(wd)
		})

		lib := NewIOLibrary()
		lib.Stdin = nil
		lib.Stdout = nil
		lib.Stderr = nil

		state := new(State)
		defer func() {
			if err := state.Close(); err != nil {
				t.Error("Close:", err)
			}
		}()
		out := new(strings.Builder)
		openBase := NewOpenBase(out, nil)
		if err := Require(state, GName, true, openBase); err != nil {
			t.Error(err)
		}
		if err := Require(state, IOLibraryName, true, lib.OpenLibrary); err != nil {
			t.Error(err)
		}

		f, err := os.Open(filepath.Join("testdata", "popen_unix.lua"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := state.Load(f, "@testdata/popen_unix.lua", "t"); err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(t.TempDir()); err != nil {
			t.Fatal(err)
		}
		err = state.Call(0, 0, 0)
		if out.Len() > 0 {
			t.Log(out.String())
		}
		if err != nil {
			t.Error(err)
		}
	})
}
