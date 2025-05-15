// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

//go:generate stringer -type=LoadCmd -linecomment -output=load_command_string.go

package macho

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const loadCommandFixedSize = 8

// LoadCmd is an enumeration of load command types.
type LoadCmd uint32

const (
	LoadCmdSegment        LoadCmd = 0x1        // LC_SEGMENT
	LoadCmdSymtab         LoadCmd = 0x2        // LC_SYMTAB
	LoadCmdThread         LoadCmd = 0x4        // LC_THREAD
	LoadCmdUnixThread     LoadCmd = 0x5        // LC_UNIXTHREAD
	LoadCmdDysymtab       LoadCmd = 0xb        // LC_DYSYMTAB
	LoadCmdLoadDylib      LoadCmd = 0xc        // LC_LOAD_DYLIB
	LoadCmdLoadDylinker   LoadCmd = 0xe        // LC_LOAD_DYLINKER
	LoadCmdIDDylinker     LoadCmd = 0xf        // LC_ID_DYLINKER
	LoadCmdSegment64      LoadCmd = 0x19       // LC_SEGMENT_64
	LoadCmdUUID           LoadCmd = 0x1b       // LC_UUID
	LoadCmdRPath          LoadCmd = 0x8000001c // LC_RPATH
	LoadCmdCodeSignature  LoadCmd = 0x1d       // LC_CODE_SIGNATURE
	LoadCmdSourceVersion  LoadCmd = 0x2a       // LC_SOURCE_VERSION
	LoadCmdDyldInfo       LoadCmd = 0x22       // LC_DYLD_INFO
	LoadCmdDyldInfoOnly   LoadCmd = 0x80000022 // LC_DYLD_INFO_ONLY
	LoadCmdFunctionStarts LoadCmd = 0x26       // LC_FUNCTION_STARTS
	LoadCmdDataInCode     LoadCmd = 0x29       // LC_DATA_IN_CODE
	LoadCmdMain           LoadCmd = 0x80000028 // LC_MAIN
	LoadCmdBuildVersion   LoadCmd = 0x32       // LC_BUILD_VERSION
)

// A CommandReader reads Mach-O load commands from a stream.
// CommandReaders do not buffer their reads
// and will not read past the allocated
type CommandReader struct {
	r                 io.Reader
	remainingCommands uint32
	remainingBytes    uint32
	byteOrder         binary.ByteOrder

	buf                [loadCommandFixedSize]byte
	nbuf               uint8
	currRemainingBytes uint32
	err                error
}

func newCommandReader(r io.Reader, n uint32, size uint32, byteOrder binary.ByteOrder) *CommandReader {
	if n == 0 && size != 0 {
		return &CommandReader{err: errCommandTrailingData}
	}
	if n > 0 && int64(size) < int64(n)*loadCommandFixedSize {
		return &CommandReader{err: fmt.Errorf("read mach-o load command: declared size (%d) too small for number of commands (%d)", size, n)}
	}
	return &CommandReader{
		r:                 r,
		remainingCommands: n,
		remainingBytes:    size,
		byteOrder:         byteOrder,

		// Special sentinel value for first read.
		nbuf: loadCommandFixedSize + 1,
	}
}

// Err returns the first non-[io.EOF] error encountered by r.
func (r *CommandReader) Err() error {
	if r.err == io.EOF {
		return nil
	}
	return r.err
}

// Next advances r to the next load command,
// which will then be available through [*CommandReader.Read].
// It returns false when there are no more load commands,
// either by reaching the end of the input or an error.
// If the previous load command was not fully read,
// Next will discard the unread bytes.
// After Next returns false,
// the [*CommandReader.Err] method will return any error that occurred during scanning,
// except that if it was [io.EOF], [*CommandReader.Err] will return nil.
func (r *CommandReader) Next() bool {
	// Read command size if the caller did not.
	for r.nbuf < loadCommandFixedSize && r.err == nil {
		r.fillInfo(loadCommandFixedSize)
	}
	if r.err != nil {
		r.clearInfo()
		return false
	}

	// Discard the remaining bytes in the command.
	r.err = skipBytes(r.r, int64(r.currRemainingBytes))
	if r.err != nil {
		r.clearInfo()
		return false
	}
	// fillInfo guarantees r.remainingBytes >= r.currRemainingBytes if r.err == nil.
	r.remainingBytes -= r.currRemainingBytes
	r.currRemainingBytes = 0

	// Was this the last command?
	if r.remainingCommands == 0 {
		if r.remainingBytes > 0 {
			r.err = errCommandTrailingData
		} else {
			r.err = io.EOF
		}
		r.clearInfo()
		return false
	}

	// Are there enough bytes for another load command?
	if r.remainingBytes < loadCommandFixedSize {
		r.err = errCommandSizeTooLarge
		r.clearInfo()
		return false
	}

	r.remainingCommands--
	r.nbuf = 0
	return true
}

// Read reads up to the next len(p) bytes of the current load command into p.
// It returns the number of bytes read (0 <= n <= len(p)) and any error encountered.
// The first 8 bytes of a load command are always the its type and size.
// After reading the first 8 bytes,
// use [*CommandReader.Command] and [*CommandReader.Size] to parse these values.
//
// Read does not introduce any buffering:
// each call to Read corresponds to at most one call to r's underlying [io.Reader].
func (r *CommandReader) Read(p []byte) (n int, err error) {
	if len(p) == 0 || r.err != nil {
		return 0, r.err
	}

	// Fill info first.
	if int(r.nbuf) < len(r.buf) {
		n = copy(p, r.fillInfo(len(p)))
		return n, r.err
	}

	// Read rest of command.
	if int64(r.currRemainingBytes) < int64(len(p)) {
		p = p[:r.currRemainingBytes]
	}
	n = r.read(p)
	err = r.err
	r.currRemainingBytes = decreaseCounter(r.currRemainingBytes, n)
	if r.currRemainingBytes == 0 && err == nil {
		err = io.EOF
	}
	return n, err
}

func (r *CommandReader) fillInfo(maxRead int) []byte {
	buf := r.buf[r.nbuf:]
	if len(buf) > maxRead {
		buf = buf[:maxRead]
	}
	n := r.read(buf)
	r.nbuf += uint8(n)

	if size, ok := r.size(); ok {
		// Buffer filled. Validate size.
		if size < 8 {
			r.currRemainingBytes = 0
			if r.err == nil {
				r.err = errCommandSizeTooSmall
			}
		} else {
			r.currRemainingBytes = size - 8
			if r.currRemainingBytes > r.remainingBytes && r.err == nil {
				r.err = errCommandSizeTooLarge
			}
		}
	}

	return buf[:n]
}

// clearInfo clears r.buf,
// causing both [*CommandReader.Command] and [*CommandReader.Size] to report ok == false.
func (r *CommandReader) clearInfo() {
	clear(r.buf[:])
	r.nbuf = 0
}

func (r *CommandReader) read(p []byte) int {
	if r.err != nil || len(p) == 0 {
		return 0
	}
	var n int
	n, r.err = r.r.Read(p)
	r.remainingBytes = decreaseCounter(r.remainingBytes, n)
	if r.err == io.EOF && r.remainingBytes > 0 {
		r.err = io.ErrUnexpectedEOF
	}
	return n
}

// Command returns the type of the current command
// if it has been read.
// ok will be true if and only if at least 4 bytes of the current command have been read.
func (r *CommandReader) Command() (_ LoadCmd, ok bool) {
	if r.nbuf < 4 || r.nbuf > loadCommandFixedSize {
		return 0, false
	}
	return LoadCmd(r.byteOrder.Uint32(r.buf[:])), true
}

// Size returns the total size of the current command in bytes.
// ok is false if the first 8 bytes of the command haven't been read yet
// or the size is invalid.
// Size will never return a value less than 8 (the fixed base size of a command).
func (r *CommandReader) Size() (_ uint32, ok bool) {
	size, ok := r.size()
	if !ok || size < loadCommandFixedSize {
		return loadCommandFixedSize, false
	}
	return size, r.currRemainingBytes <= r.remainingBytes
}

func (r *CommandReader) size() (_ uint32, ok bool) {
	return r.byteOrder.Uint32(r.buf[4:]), r.nbuf == loadCommandFixedSize
}

var (
	errCommandSizeTooSmall = errors.New("read mach-o load command: invalid size for command")
	errCommandSizeTooLarge = errors.New("read mach-o load command: command array larger than declared size")
	errCommandTrailingData = errors.New("read mach-o load command: command array smaller than declared size")
)

func decreaseCounter(count uint32, n int) uint32 {
	switch {
	case n < 0:
		return count
	case int64(n) >= int64(count):
		return 0
	default:
		return count - uint32(n)
	}
}

func skipBytes(r io.Reader, n int64) error {
	if n == 0 {
		return nil
	}
	if s, ok := r.(io.Seeker); ok {
		_, err := s.Seek(n, io.SeekCurrent)
		return err
	}
	_, err := io.CopyN(io.Discard, r, n)
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return err
}
