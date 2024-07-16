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
	"math"
	"runtime/cgo"
	"strings"
	"unsafe"

	"zombiezen.com/go/zb/internal/bufseek"
)

const streamMetatableName = "*zombiezen.com/go/zb/internal/lua.stream"

// PushReader pushes a Lua file object onto the stack
// that reads from the given reader.
// If the reader also implements io.Seeker,
// then the file object's seek method will use it.
func PushReader(l *State, r io.ReadCloser) error {
	if err := createStreamMetatable(l); err != nil {
		return fmt.Errorf("lua: push io.ReadCloser: %v", err)
	}
	pushStream(l, newStream(r, true, false, true))
	return nil
}

// PushWriter pushes a Lua file object onto the stack
// that writes to the given writer.
// If the writer also implements io.Seeker,
// then the file object's seek method will use it.
func PushWriter(l *State, w io.WriteCloser) error {
	if err := createStreamMetatable(l); err != nil {
		return fmt.Errorf("lua: push io.WriteCloser: %v", err)
	}
	pushStream(l, newStream(w, false, true, true))
	return nil
}

// PushPipe pushes a Lua file object onto the stack
// that reads and writes to rw.
func PushPipe(l *State, rw io.ReadWriteCloser) error {
	if err := createStreamMetatable(l); err != nil {
		return fmt.Errorf("lua: push io.ReadWriteCloser: %v", err)
	}
	pushStream(l, newStream(rw, true, true, false))
	return nil
}

// PushFile pushes a Lua file object onto the stack
// that reads, writes, and seeks using f.
func PushFile(l *State, f ReadWriteSeekCloser) error {
	if err := createStreamMetatable(l); err != nil {
		return fmt.Errorf("lua: push file: %v", err)
	}
	pushStream(l, newStream(f, true, true, true))
	return nil
}

func pushStream(l *State, s *stream) {
	l.NewUserdataUV(int(unsafe.Sizeof(uintptr(0))), 1)
	SetMetatable(l, streamMetatableName)
	setUintptr(l, -1, uintptr(cgo.NewHandle(s)))
}

func createStreamMetatable(l *State) error {
	if !NewMetatable(l, streamMetatableName) {
		l.Pop(1)
		return nil
	}
	err := SetFuncs(l, 0, map[string]Function{
		"__index":     nil,
		"__gc":        fgc,
		"__close":     nil,
		"__tostring":  ftostring,
		"__metatable": nil, // prevent access to metatable
	})
	if err != nil {
		l.Pop(1)
		return err
	}

	// Use same __gc function for __close.
	l.RawField(-1, "__gc")
	l.RawSetField(-2, "__close")

	err = NewLib(l, map[string]Function{
		"close":   fclose,
		"flush":   fflush,
		"lines":   flines,
		"read":    fread,
		"seek":    fseek,
		"setvbuf": fsetvbuf,
		"write":   fwrite,
	})
	if err != nil {
		l.Pop(1)
		return err
	}
	l.RawSetField(-2, "__index") // metatable.__index = method table

	l.Pop(1)
	return nil
}

func ftostring(l *State) (int, error) {
	s, err := toStream(l)
	if err != nil {
		return 0, err
	}
	switch {
	case s.isClosed():
		l.PushString("file (closed)")
	case s.r != nil:
		l.PushString(fmt.Sprintf("file (%p)", s.r))
	case s.w != nil:
		l.PushString(fmt.Sprintf("file (%p)", s.w))
	default:
		l.PushString("file")
	}
	return 1, nil
}

func fgc(l *State) (int, error) {
	s, err := toStream(l)
	if err != nil {
		return 0, err
	}
	s.Close()
	setUintptr(l, 1, 0)
	return 0, nil
}

func fclose(l *State) (int, error) {
	s, err := toStream(l)
	if err != nil {
		return 0, err
	}
	err = s.Close()
	return pushFileResult(l, err), nil
}

func fread(l *State) (int, error) {
	s, err := toStream(l)
	if err != nil {
		return 0, err
	}
	return s.read(l, 2)
}

func fwrite(l *State) (int, error) {
	s, err := toStream(l)
	if err != nil {
		return 0, err
	}
	l.PushValue(1) // push file at the stack top (to be returned)
	return s.write(l, 2)
}

func fseek(l *State) (int, error) {
	s, err := toStream(l)
	if err != nil {
		return 0, err
	}

	const modeArg = 2
	whence := io.SeekCurrent
	if !l.IsNoneOrNil(modeArg) {
		modes := map[string]int{
			"set": io.SeekStart,
			"cur": io.SeekCurrent,
			"end": io.SeekEnd,
		}
		mode, err := CheckString(l, modeArg)
		if err != nil {
			return 0, err
		}
		var ok bool
		whence, ok = modes[mode]
		if !ok {
			return 0, NewArgError(l, modeArg, fmt.Sprintf("invalid option '%s'", mode))
		}
	}

	const offsetArg = 3
	var offset int64
	if !l.IsNoneOrNil(offsetArg) {
		var err error
		offset, err = CheckInteger(l, offsetArg)
		if err != nil {
			return 0, err
		}
	}

	if s.seek == nil {
		return pushFileResult(l, fmt.Errorf("seek: %w", errors.ErrUnsupported)), nil
	}
	pos, err := s.seek.Seek(offset, whence)
	if err != nil {
		return pushFileResult(l, err), nil
	}
	l.PushInteger(pos)
	return 1, nil
}

func flines(l *State) (int, error) {
	if _, err := toStream(l); err != nil {
		return 0, err
	}
	if err := pushLinesFunction(l, false); err != nil {
		return 0, err
	}
	return 1, nil
}

func fflush(l *State) (int, error) {
	l.PushBoolean(true)
	return 1, nil
}

func fsetvbuf(l *State) (int, error) {
	pushFail(l)
	return 1, nil
}

// registryStream gets the stream stored in the registry at the given key
// and pushes it onto the stack.
func registryStream(l *State, findex string) (*stream, error) {
	if _, err := l.Field(RegistryIndex, findex, 0); err != nil {
		return nil, err
	}
	s := testStream(l, -1)
	if s == nil {
		return nil, fmt.Errorf("could not extract stream from registry %q", findex)
	}
	return s, nil
}

func toStream(l *State) (*stream, error) {
	const idx = 1
	if _, err := CheckUserdata(l, idx, streamMetatableName); err != nil {
		return nil, err
	}
	s := testStream(l, idx)
	if s == nil {
		return nil, NewArgError(l, idx, "could not extract stream")
	}
	return s, nil
}

func testStream(l *State, idx int) *stream {
	handle := cgo.Handle(unmarshalUintptr(TestUserdata(l, idx, streamMetatableName)))
	if handle == 0 {
		return nil
	}
	s, _ := handle.Value().(*stream)
	return s
}

type byteReader interface {
	io.Reader
	io.ByteReader
}

type stream struct {
	r    byteReader
	w    io.Writer
	seek io.Seeker
	c    io.Closer
}

func newStream(f io.Closer, read, write, seek bool) *stream {
	s := &stream{c: f}
	var r io.Reader
	var br io.ByteReader
	if read {
		r, _ = f.(io.Reader)
		br, _ = f.(io.ByteReader)
	}
	if write {
		s.w, _ = f.(io.Writer)
	}
	if seek {
		s.seek, _ = f.(io.Seeker)
	}

	switch {
	case r != nil && br != nil:
		s.r = f.(byteReader)
	case r != nil && br == nil && s.seek == nil:
		s.r = bufio.NewReader(r)
	case r != nil && br == nil && s.seek != nil && s.w == nil:
		b := bufseek.NewReader(r.(io.ReadSeeker))
		s.r = b
		s.seek = b
	case r != nil && br == nil && s.seek != nil && s.w != nil:
		b := bufseek.NewReadWriter(f.(io.ReadWriteSeeker))
		s.r = b
		s.w = b
		s.seek = b
	case r == nil && br != nil && s.seek == nil:
		s.r = polyfillReader{br}
	case r == nil && br != nil && s.seek != nil && s.w == nil:
		s.r = struct {
			polyfillReader
			io.Seeker
		}{
			polyfillReader{br},
			s.seek,
		}
	case r == nil && br != nil && s.seek != nil && s.w != nil:
		rw := struct {
			polyfillReader
			io.Writer
			io.Seeker
		}{
			polyfillReader{br},
			s.w,
			s.seek,
		}
		b := bufseek.NewReadWriter(rw)
		s.r = b
		s.w = b
		s.seek = b
	}
	return s
}

// read handles the io.read function or file:read method.
// first is the 1-based first format to read.
// It is assumed that the stream object is at either top or bottom of the stack.
func (s *stream) read(l *State, first int) (int, error) {
	if s.r == nil {
		return pushFileResult(l, fmt.Errorf("read: %w", errors.ErrUnsupported)), nil
	}

	nArgs := l.Top() - 1
	if nArgs <= 0 {
		line, err := s.readLine(true)
		if err == io.EOF {
			pushFail(l)
			return 1, nil
		}
		if err != nil {
			return pushFileResult(l, err), nil
		}
		l.PushString(line)
		return 1, nil
	}

	if !l.CheckStack(nArgs + 20) {
		return 0, fmt.Errorf("%sstack overflow (too many arguments)", Where(l, 1))
	}
	var n int
	for n = first; nArgs > 0; n, nArgs = n+1, nArgs-1 {
		if l.Type(n) == TypeNumber {
			size, err := CheckInteger(l, n)
			if err != nil {
				return 0, err
			}
			if size < 0 || size > math.MaxInt {
				return 0, NewArgError(l, n, "out of range")
			}
			buf, err := s.readSlice(int(size))
			if err == io.EOF {
				pushFail(l)
				break
			}
			if err != nil {
				return pushFileResult(l, err), nil
			}
			// TODO(someday): Push bytes directly.
			l.PushString(string(buf))
			continue
		}

		format, err := CheckString(l, n)
		if err != nil {
			return 0, err
		}
		format = strings.TrimPrefix(format, "*")
		switch format {
		case "l", "L":
			line, err := s.readLine(format == "l")
			if err == io.EOF {
				pushFail(l)
				break
			}
			if err != nil {
				return pushFileResult(l, err), nil
			}
			l.PushString(line)
		case "a":
			l.PushString(s.readAll())
		default:
			return 0, NewArgError(l, n, "invalid format")
		}
	}
	return n - first, nil
}

func (s *stream) readSlice(n int) ([]byte, error) {
	if n == 0 {
		_, err := s.r.Read(nil)
		return nil, err
	}
	buf := make([]byte, n)
	n, err := s.r.Read(buf)
	if n == 0 {
		if err == nil {
			return nil, io.ErrNoProgress
		}
		return nil, err
	}
	return buf[:n], nil
}

func (s *stream) readAll() string {
	// TODO(someday): Add limits.
	sb := new(strings.Builder)
	_, _ = io.Copy(sb, s.r)
	return sb.String()
}

func (s *stream) readLine(chop bool) (string, error) {
	sb := new(strings.Builder)
	for {
		b, err := s.r.ReadByte()
		if err != nil {
			if sb.Len() == 0 {
				return "", err
			}
			return sb.String(), nil
		}
		if b == '\n' {
			if !chop {
				sb.WriteByte(b)
			}
			return sb.String(), nil
		}
		sb.WriteByte(b)
	}
}

// write handles the io.write function or file:write method.
// The top of the stack must be the file handle object.
// arg is the 1-based first argument to write.
func (s *stream) write(l *State, arg int) (int, error) {
	if s.w == nil {
		return pushFileResult(l, fmt.Errorf("write: %w", errors.ErrUnsupported)), nil
	}

	nArgs := l.Top() - arg
	for ; nArgs > 0; arg, nArgs = arg+1, nArgs-1 {
		var werr error
		if l.Type(arg) == TypeNumber {
			if l.IsInteger(arg) {
				n, _ := l.ToInteger(arg)
				_, werr = fmt.Fprintf(s.w, "%d", n)
			} else {
				n, _ := l.ToNumber(arg)
				_, werr = fmt.Fprintf(s.w, "%.14g", n)
			}
		} else {
			var argString string
			argString, err := CheckString(l, arg)
			if err != nil {
				return 0, err
			}
			_, werr = io.WriteString(s.w, argString)
		}
		if werr != nil {
			return pushFileResult(l, werr), nil
		}
	}
	// File handle already on stack top.
	return 1, nil
}

// pushLinesFunction pushes the result of the file:lines method onto the stack
// after popping its arguments.
// It assumes that the first item on the stack is the stream handle,
// and the remaining items on the stack are the arguments to pass to file:read.
func pushLinesFunction(l *State, toClose bool) error {
	nArgs := l.Top() - 1
	const maxArgs = 250
	if nArgs >= maxArgs {
		return NewArgError(l, maxArgs+2, "too many arguments")
	}
	l.PushValue(1)
	l.PushClosure(nArgs+1, func(l *State) (int, error) {
		s := testStream(l, UpvalueIndex(1))
		if s == nil {
			return 0, fmt.Errorf("%sinvalid stream upvalue", Where(l, 1))
		}
		if s.isClosed() {
			return 0, fmt.Errorf("%sfile is already closed", Where(l, 1))
		}
		l.SetTop(1)
		if !l.CheckStack(nArgs) {
			return 0, fmt.Errorf("%stoo many arguments", Where(l, 1))
		}
		for i := 1; i <= nArgs; i++ {
			l.PushValue(UpvalueIndex(1 + i))
		}
		nResults, err := s.read(l, 2)
		if err != nil {
			return 0, err
		}
		if !l.ToBoolean(-nResults) {
			// EOF or error without reading anything.
			if nResults > 1 {
				// Has error information (not an EOF).
				msg, _ := l.ToString(-nResults + 1)
				return 0, fmt.Errorf("%s%s", Where(l, 1), msg)
			}
			if toClose {
				l.SetTop(0)
				l.PushValue(UpvalueIndex(1))
				if _, err := fclose(l); err != nil {
					return 0, err
				}
			}
			return 0, nil
		}
		return nResults, nil
	})
	return nil
}

func (s *stream) isClosed() bool {
	return s.c == nil
}

func (s *stream) Close() error {
	if s.isClosed() {
		return nil
	}
	err := s.c.Close()
	*s = stream{}
	return err
}

type polyfillReader struct {
	r io.ByteReader
}

func (pr polyfillReader) Read(p []byte) (int, error) {
	if r, ok := pr.r.(io.Reader); ok {
		return r.Read(p)
	}
	for i := range p {
		c, err := pr.r.ReadByte()
		if err != nil {
			return i, err
		}
		p[i] = c
	}
	return len(p), nil
}

func (pr polyfillReader) ReadByte() (byte, error) {
	return pr.r.ReadByte()
}
