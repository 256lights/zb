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
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	ioInput  = "_zb_IO_input"
	ioOutput = "_zb_IO_output"
)

// IOLibrary is a pure Go implementation of the standard Lua "io" library.
// The zero value of IOLibrary stubs out all functionality.
type IOLibrary struct {
	// Stdin is the reader for io.stdin.
	// If nil, stdin will act like an empty file.
	Stdin io.ByteReader
	// Stdout is the writer for io.stdout.
	// If nil, io.stdout will discard any data written to it.
	Stdout io.Writer
	// Stderr is the writer for io.stderr.
	// If nil, io.stderr will discard any data written to it.
	Stderr io.Writer

	// Open opens a file with the given name and [mode].
	// The returned file should implement io.Reader and/or io.Writer,
	// and may optionally implement io.ByteReader and/or io.Seeker.
	//
	// [mode]: https://www.lua.org/manual/5.4/manual.html#pdf-io.open
	Open func(name, mode string) (io.Closer, error)

	// CreateTemp returns a handle for a temporary file opened in update mode.
	// The returned file should clean up the file on Close.
	CreateTemp func() (ReadWriteSeekCloser, error)

	// OpenProcessReader starts a subprocess
	// and returns a handle for reading its standard output.
	// If nil, io.popen(command, "r") will return an error.
	OpenProcessReader func(command string) (io.ReadCloser, error)

	// OpenProcessWriter starts a subprocess
	// and returns a handle for writing to its standard input.
	// If nil, io.popen(command, "w") will return an error.
	OpenProcessWriter func(command string) (io.WriteCloser, error)
}

// NewIOLibrary returns an OSLibrary that uses the native operating system.
func NewIOLibrary() *IOLibrary {
	return &IOLibrary{
		Stdin:             bufio.NewReader(os.Stdin),
		Stdout:            os.Stdout,
		Stderr:            os.Stderr,
		Open:              ioOpen,
		CreateTemp:        ioCreateTemp,
		OpenProcessReader: popenRead,
		OpenProcessWriter: popenWrite,
	}
}

func ioOpen(name, mode string) (io.Closer, error) {
	var flag int
	mode = strings.TrimSuffix(mode, "b")
	switch mode {
	case "r":
		flag = os.O_RDONLY
	case "w":
		flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	case "a":
		flag = os.O_WRONLY | os.O_APPEND | os.O_CREATE
	case "r+":
		flag = os.O_RDWR | os.O_CREATE
	case "w+":
		flag = os.O_RDWR | os.O_CREATE | os.O_TRUNC
	case "a+":
		flag = os.O_RDWR | os.O_APPEND | os.O_CREATE
	default:
		return nil, &os.PathError{
			Op:   "open",
			Path: name,
			Err:  fmt.Errorf("invalid mode %q", mode),
		}
	}
	return os.OpenFile(name, flag, 0o666)
}

func ioCreateTemp() (ReadWriteSeekCloser, error) {
	f, err := os.CreateTemp("", "lua_")
	if err != nil {
		return nil, err
	}
	fname := f.Name()
	if runtime.GOOS != "windows" {
		// Non-Windows operating systems usually support unlinking the file while it's open.
		// If it works, then that's all we have to do.
		if err := os.Remove(fname); err == nil {
			return f, nil
		}
	}
	fullPath, err := filepath.Abs(fname)
	if err != nil {
		f.Close()
		os.Remove(fname)
		return nil, err
	}
	return &removeOnCloseFile{f, fullPath}, nil
}

func popenRead(command string) (io.ReadCloser, error) {
	c := osCommand(command)
	c.Stdin = os.Stdin
	c.Stderr = os.Stderr
	stdout, err := c.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := c.Start(); err != nil {
		return nil, err
	}
	return &popenReader{stdout, c}, nil
}

func popenWrite(command string) (io.WriteCloser, error) {
	c := osCommand(command)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	stdin, err := c.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := c.Start(); err != nil {
		return nil, err
	}
	return &popenWriter{stdin, c}, nil
}

// OpenLibrary loads the standard io library.
// This method is intended to be used as an argument to [Require].
func (lib *IOLibrary) OpenLibrary(l *State) (int, error) {
	err := NewLib(l, map[string]Function{
		"close":   lib.close,
		"flush":   lib.flush,
		"input":   lib.input,
		"lines":   lib.lines,
		"open":    lib.open,
		"output":  lib.output,
		"popen":   lib.popen,
		"read":    lib.read,
		"stderr":  nil,
		"stdin":   nil,
		"stdout":  nil,
		"tmpfile": lib.tmpfile,
		"type":    lib.type_,
		"write":   lib.write,
	})
	if err != nil {
		return 0, err
	}
	if err := createStreamMetatable(l); err != nil {
		return 0, err
	}

	stdinStream := &stream{r: stdinReader{&lib.Stdin}, c: noClose{}}
	pushStream(l, stdinStream)
	l.PushValue(-1)
	if err := l.SetField(RegistryIndex, ioInput, 0); err != nil {
		return 0, err
	}
	l.RawSetField(-2, "stdin")

	pushStream(l, &stream{w: stdoutWriter{&lib.Stdout}, c: noClose{}})
	l.PushValue(-1)
	if err := l.SetField(RegistryIndex, ioOutput, 0); err != nil {
		return 0, err
	}
	l.RawSetField(-2, "stdout")

	pushStream(l, &stream{w: stdoutWriter{&lib.Stderr}, c: noClose{}})
	l.RawSetField(-2, "stderr")

	return 1, nil
}

func (lib *IOLibrary) flush(l *State) (int, error) {
	if _, err := registryStream(l, ioOutput); err != nil {
		return 0, err
	}
	l.Insert(1)
	return fflush(l)
}

func (lib *IOLibrary) type_(l *State) (int, error) {
	const argIdx = 1
	if l.IsNone(argIdx) {
		return 0, NewArgError(l, argIdx, "value expected")
	}
	s := testStream(l, argIdx)
	if s == nil {
		pushFail(l)
		return 1, nil
	}
	if s.isClosed() {
		l.PushString("closed file")
	} else {
		l.PushString("file")
	}
	return 1, nil
}

func (lib *IOLibrary) close(l *State) (int, error) {
	if l.IsNone(1) {
		// Use default output.
		if _, err := l.Field(RegistryIndex, ioOutput, 0); err != nil {
			return 0, err
		}
	}
	return fclose(l)
}

func (lib *IOLibrary) open(l *State) (int, error) {
	filename, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	mode := "r"
	if !l.IsNoneOrNil(2) {
		mode, err = CheckString(l, 2)
		if err != nil {
			return 0, err
		}
	}
	s, err := lib.doOpen(filename, mode)
	if err != nil {
		return pushFileResult(l, err), nil
	}
	pushStream(l, s)
	return 1, nil
}

func (lib *IOLibrary) doOpen(filename, mode string) (*stream, error) {
	if lib.Open == nil {
		return nil, errors.ErrUnsupported
	}
	f, err := lib.Open(filename, mode)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, errors.New("IOLibrary.Open returned nil")
	}
	return newStream(f, true, true, true), nil
}

func (lib *IOLibrary) tmpfile(l *State) (int, error) {
	if lib.CreateTemp == nil {
		return pushFileResult(l, errors.ErrUnsupported), nil
	}
	f, err := lib.CreateTemp()
	if err != nil {
		return pushFileResult(l, err), nil
	}
	if f == nil {
		return pushFileResult(l, errors.New("IOLibrary.CreateTemp returned nil")), nil
	}
	pushStream(l, newStream(f, true, true, true))
	return 1, nil
}

func (lib *IOLibrary) input(l *State) (int, error) {
	return lib.filefunc(l, ioInput, "r")
}

func (lib *IOLibrary) output(l *State) (int, error) {
	return lib.filefunc(l, ioOutput, "w")
}

// filefunc implements io.input and io.output.
func (lib *IOLibrary) filefunc(l *State, f, mode string) (int, error) {
	if !l.IsNoneOrNil(1) {
		if filename, ok := l.ToString(1); ok {
			s, err := lib.doOpen(filename, mode)
			if err != nil {
				return 0, fmt.Errorf("%s%w", Where(l, 1), err)
			}
			pushStream(l, s)
		} else {
			if _, err := toStream(l); err != nil {
				return 0, err
			}
			l.PushValue(1)
		}
		if err := l.SetField(RegistryIndex, f, 0); err != nil {
			return 0, err
		}
	}
	if _, err := l.Field(RegistryIndex, f, 0); err != nil {
		return 0, err
	}
	return 1, nil
}

func (lib *IOLibrary) popen(l *State) (int, error) {
	command, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	const modeArg = 2
	mode := "r"
	if !l.IsNoneOrNil(modeArg) {
		mode, err = CheckString(l, modeArg)
		if err != nil {
			return 0, err
		}
	}
	switch mode {
	case "r":
		if lib.OpenProcessReader == nil {
			err := fmt.Errorf("popen %s: %w", command, errors.ErrUnsupported)
			return pushFileResult(l, err), nil
		}
		r, err := lib.OpenProcessReader(command)
		if err != nil {
			return pushFileResult(l, err), nil
		}
		pushStream(l, newStream(r, true, false, false))
		return 1, nil
	case "w":
		if lib.OpenProcessWriter == nil {
			err := fmt.Errorf("popen %s: %w", command, errors.ErrUnsupported)
			return pushFileResult(l, err), nil
		}
		w, err := lib.OpenProcessWriter(command)
		if err != nil {
			return pushFileResult(l, err), nil
		}
		pushStream(l, newStream(w, false, true, false))
		return 1, nil
	default:
		return 0, NewArgError(l, modeArg, "invalid mode")
	}
}

func (lib *IOLibrary) read(l *State) (int, error) {
	s, err := registryStream(l, ioInput)
	if err != nil {
		return 0, err
	}
	return s.read(l, 1)
}

func (lib *IOLibrary) write(l *State) (int, error) {
	s, err := registryStream(l, ioOutput)
	if err != nil {
		return 0, err
	}
	return s.write(l, 1)
}

func (lib *IOLibrary) lines(l *State) (int, error) {
	if l.IsNone(1) {
		l.PushNil()
	}
	toClose := false
	if l.IsNil(1) {
		if _, err := registryStream(l, ioInput); err != nil {
			return 0, err
		}
		l.Replace(1)
	} else {
		filename, err := CheckString(l, 1)
		if err != nil {
			return 0, err
		}
		s, err := lib.doOpen(filename, "r")
		if err != nil {
			return 0, fmt.Errorf("%s%w", Where(l, 1), err)
		}
		pushStream(l, s)
		l.Replace(1)
		toClose = true
	}
	if err := pushLinesFunction(l, toClose); err != nil {
		return 0, err
	}
	if toClose {
		l.PushNil()    // state
		l.PushNil()    // control
		l.PushValue(1) // file as to-be-closed variable
		return 4, nil
	}
	return 1, nil
}

// ReadWriteSeekCloser is an interface
// that groups the basic Read, Write, Seek, and Close methods.
type ReadWriteSeekCloser interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer
}

type stdinReader struct {
	r *io.ByteReader
}

func (in stdinReader) Read(p []byte) (int, error) {
	if *in.r == nil {
		return 0, io.EOF
	}
	if r, ok := (*in.r).(io.Reader); ok {
		return r.Read(p)
	}
	for i := range p {
		c, err := (*in.r).ReadByte()
		if err != nil {
			return i, err
		}
		p[i] = c
	}
	return len(p), nil
}

func (in stdinReader) ReadByte() (byte, error) {
	if *in.r == nil {
		return 0, io.EOF
	}
	return (*in.r).ReadByte()
}

type stdoutWriter struct {
	w *io.Writer
}

func (out stdoutWriter) Write(p []byte) (int, error) {
	if *out.w == nil {
		return len(p), nil
	}
	return (*out.w).Write(p)
}

type noClose struct{}

func (noClose) Close() error {
	return errors.New("cannot close standard file")
}

type removeOnCloseFile struct {
	f    *os.File
	path string
}

func (f *removeOnCloseFile) Read(p []byte) (int, error) {
	return f.f.Read(p)
}

func (f *removeOnCloseFile) Write(p []byte) (int, error) {
	return f.f.Write(p)
}

func (f *removeOnCloseFile) Seek(offset int64, whence int) (int64, error) {
	return f.f.Seek(offset, whence)
}

func (f *removeOnCloseFile) Close() error {
	err1 := f.f.Close()
	err2 := os.Remove(f.path)
	if err1 != nil {
		return err1
	}
	return err2
}

type popenReader struct {
	pipe io.ReadCloser
	cmd  *exec.Cmd
}

func (p *popenReader) Read(b []byte) (int, error) {
	return p.pipe.Read(b)
}

func (p *popenReader) Close() error {
	err1 := p.pipe.Close()
	err2 := p.cmd.Wait()
	if err2 != nil {
		return err2
	}
	return err1
}

type popenWriter struct {
	pipe io.WriteCloser
	cmd  *exec.Cmd
}

func (p *popenWriter) Write(b []byte) (int, error) {
	return p.pipe.Write(b)
}

func (p *popenWriter) Close() error {
	err1 := p.pipe.Close()
	err2 := p.cmd.Wait()
	if err2 != nil {
		return err2
	}
	return err1
}
