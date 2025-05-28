// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

//go:generate go tool stringer -type=LoadCmd -linecomment -output=load_command_string.go

package macho

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// LoadCommandMinSize is the minimum size (in bytes) of a Mach-O load command.
const LoadCommandMinSize = 8

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

// VirtualMemoryProtection is a set of bitflags used to indicate permissions on a [SegmentCommand].
type VirtualMemoryProtection uint32

// Virtual memory permissions.
const (
	VirtualMemoryReadPermission    VirtualMemoryProtection = 0x1
	VirtualMemoryWritePermission   VirtualMemoryProtection = 0x2
	VirtualMemoryExecutePermission VirtualMemoryProtection = 0x4
)

// SegmentCommand is the structure for [LoadCmdSegment] and [LoadCmdSegment64].
type SegmentCommand struct {
	Command              LoadCmd
	RawName              [16]byte
	VirtualMemoryAddress uint64
	VirtualMemorySize    uint64
	FileOffset           uint64
	FileSize             uint64
	MaxProtection        VirtualMemoryProtection
	InitProtection       VirtualMemoryProtection
	Flags                uint32

	Sections []Section
}

// Name returns the segment's name as a string.
func (cmd *SegmentCommand) Name() string {
	return nameToString(cmd.RawName[:])
}

// UnmarshalMachO unmarshals the load command in data into cmd.
func (cmd *SegmentCommand) UnmarshalMachO(byteOrder binary.ByteOrder, data []byte) error {
	var err error
	cmd.Command, err = unmarshalLoadCommand(byteOrder, data)
	if err != nil {
		return err
	}
	switch cmd.Command {
	case LoadCmdSegment:
		const fixedSize = LoadCommandMinSize + 48
		if len(data) < fixedSize {
			return fmt.Errorf("unmarshal %v: too short", cmd.Command)
		}
		copy(cmd.RawName[:], data[LoadCommandMinSize:])
		cmd.VirtualMemoryAddress = uint64(byteOrder.Uint32(data[LoadCommandMinSize+16:]))
		cmd.VirtualMemorySize = uint64(byteOrder.Uint32(data[LoadCommandMinSize+20:]))
		cmd.FileOffset = uint64(byteOrder.Uint32(data[LoadCommandMinSize+24:]))
		cmd.FileSize = uint64(byteOrder.Uint32(data[LoadCommandMinSize+28:]))
		cmd.MaxProtection = VirtualMemoryProtection(byteOrder.Uint32(data[LoadCommandMinSize+32:]))
		cmd.InitProtection = VirtualMemoryProtection(byteOrder.Uint32(data[LoadCommandMinSize+36:]))
		sectionCount := byteOrder.Uint32(data[LoadCommandMinSize+40:])
		cmd.Flags = byteOrder.Uint32(data[LoadCommandMinSize+44:])

		const sectionSize = 68
		if want := fixedSize + int64(sectionCount)*sectionSize; int64(len(data)) != want {
			return fmt.Errorf("unmarshal %v: size (%d) incorrect for section count (%d)",
				cmd.Command, len(data), want)
		}
		if sectionCount == 0 {
			cmd.Sections = nil
		} else {
			cmd.Sections = make([]Section, 0, sectionCount)
			for start := fixedSize; start+sectionSize <= len(data); start += sectionSize {
				n := len(cmd.Sections)
				cmd.Sections = cmd.Sections[:n+1]
				currSection := &cmd.Sections[n]

				copy(currSection.RawName[:], data[start:])
				copy(currSection.RawSegmentName[:], data[start+16:])
				currSection.Address = uint64(byteOrder.Uint32(data[start+32:]))
				currSection.Size = uint64(byteOrder.Uint32(data[start+36:]))
				currSection.Offset = byteOrder.Uint32(data[start+40:])
				currSection.Alignment = Alignment(byteOrder.Uint32(data[start+44:]))
				currSection.RelocationOffset = byteOrder.Uint32(data[start+48:])
				currSection.RelocationCount = byteOrder.Uint32(data[start+52:])
				currSection.Flags = byteOrder.Uint32(data[start+56:])

				if _, ok := currSection.Alignment.Bytes(); !ok {
					return fmt.Errorf("unmarshal %v: alignment of section[%d] too large", cmd.Command, n)
				}
			}
		}
	case LoadCmdSegment64:
		const fixedSize = LoadCommandMinSize + 64
		if len(data) < fixedSize {
			return fmt.Errorf("unmarshal %v: too short", cmd.Command)
		}
		copy(cmd.RawName[:], data[LoadCommandMinSize:])
		cmd.VirtualMemoryAddress = byteOrder.Uint64(data[LoadCommandMinSize+16:])
		cmd.VirtualMemorySize = byteOrder.Uint64(data[LoadCommandMinSize+24:])
		cmd.FileOffset = byteOrder.Uint64(data[LoadCommandMinSize+32:])
		cmd.FileSize = byteOrder.Uint64(data[LoadCommandMinSize+40:])
		cmd.MaxProtection = VirtualMemoryProtection(byteOrder.Uint32(data[LoadCommandMinSize+48:]))
		cmd.InitProtection = VirtualMemoryProtection(byteOrder.Uint32(data[LoadCommandMinSize+52:]))
		sectionCount := byteOrder.Uint32(data[LoadCommandMinSize+56:])
		cmd.Flags = byteOrder.Uint32(data[LoadCommandMinSize+60:])

		const sectionSize = 80
		if want := fixedSize + int64(sectionCount)*sectionSize; int64(len(data)) != want {
			return fmt.Errorf("unmarshal %v: size (%d) incorrect for section count (%d)",
				cmd.Command, len(data), want)
		}
		if sectionCount == 0 {
			cmd.Sections = nil
		} else {
			cmd.Sections = make([]Section, 0, sectionCount)
			for start := fixedSize; start+sectionSize <= len(data); start += sectionSize {
				n := len(cmd.Sections)
				cmd.Sections = cmd.Sections[:n+1]
				currSection := &cmd.Sections[n]

				copy(currSection.RawName[:], data[start:])
				copy(currSection.RawSegmentName[:], data[start+16:])
				currSection.Address = byteOrder.Uint64(data[start+32:])
				currSection.Size = byteOrder.Uint64(data[start+40:])
				currSection.Offset = byteOrder.Uint32(data[start+48:])
				currSection.Alignment = Alignment(byteOrder.Uint32(data[start+52:]))
				currSection.RelocationOffset = byteOrder.Uint32(data[start+56:])
				currSection.RelocationCount = byteOrder.Uint32(data[start+60:])
				currSection.Flags = byteOrder.Uint32(data[start+64:])

				if _, ok := currSection.Alignment.Bytes(); !ok {
					return fmt.Errorf("unmarshal %v: alignment of section[%d] too large", cmd.Command, n)
				}
			}
		}
	default:
		return fmt.Errorf("unmarshal mach-o load command: unexpected %v", cmd.Command)
	}
	return nil
}

// Section represents an instruction within a [SegmentCommand]
// to load a contiguous chunk of the file into virtual memory.
type Section struct {
	RawName        [16]byte
	RawSegmentName [16]byte
	// Address is the memory address of this section.
	Address uint64
	// Size is the size in bytes of this section.
	Size uint64
	// Offset is the file offset of this section.
	Offset uint32
	// Alignment is the section's alignment.
	Alignment Alignment
	// RelocationOffset is the file offset of relocation entries.
	RelocationOffset uint32
	// RelocationCount is the number of relocation entries.
	RelocationCount uint32
	// Flags holds the section type and attributes.
	Flags uint32
}

// Name returns the section's name as a string.
func (s *Section) Name() string {
	return nameToString(s.RawName[:])
}

// SegmentName returns the name of the segment that contains the section as a string.
func (s *Section) SegmentName() string {
	return nameToString(s.RawSegmentName[:])
}

// LinkeditDataCommand is the structure for
// [LoadCmdCodeSignature], [LoadCmdFunctionStarts], and [LoadCmdDataInCode].
type LinkeditDataCommand struct {
	Command LoadCmd
	// DataOffset is the offset in bytes of the data's start
	// relative to the start of the __LINKEDIT segment.
	DataOffset uint32
	// DataSize is the size in bytes of data in the __LINKEDIT segment.
	DataSize uint32
}

// UnmarshalMachO unmarshals the load command in data into cmd.
func (cmd *LinkeditDataCommand) UnmarshalMachO(byteOrder binary.ByteOrder, data []byte) error {
	var err error
	cmd.Command, err = unmarshalLoadCommand(byteOrder, data)
	if err != nil {
		return err
	}
	if cmd.Command != LoadCmdCodeSignature &&
		cmd.Command != LoadCmdFunctionStarts &&
		cmd.Command != LoadCmdDataInCode {
		return fmt.Errorf("unmarshal mach-o load command: unexpected %v", cmd.Command)
	}
	const wantSize = LoadCommandMinSize + 8
	if len(data) != wantSize {
		return fmt.Errorf("unmarshal %v: wrong size (got %d; want %d)", cmd.Command, len(data), wantSize)
	}
	cmd.DataOffset = byteOrder.Uint32(data[LoadCommandMinSize:])
	cmd.DataSize = byteOrder.Uint32(data[LoadCommandMinSize+4:])
	return nil
}

// A CommandReader reads Mach-O load commands from a stream.
// CommandReaders do not buffer their reads
// and will not read past the load command region defined by the [FileHeader].
type CommandReader struct {
	r                 io.Reader
	remainingCommands uint32
	remainingBytes    uint32
	byteOrder         binary.ByteOrder

	buf                [LoadCommandMinSize]byte
	nbuf               uint8
	currRemainingBytes uint32
	err                error
}

// NewCommandReader returns a [CommandReader] that reads from r.
// r should read from the part of the Mach-O file directly after the header,
// as in the state of a reader after calling [ReadFileHeader].
func (hdr *FileHeader) NewCommandReader(r io.Reader) *CommandReader {
	if hdr.LoadCommandCount == 0 && hdr.LoadCommandRegionSize != 0 {
		return &CommandReader{err: errCommandTrailingData}
	}
	if hdr.LoadCommandCount > 0 && int64(hdr.LoadCommandRegionSize) < int64(hdr.LoadCommandCount)*LoadCommandMinSize {
		err := fmt.Errorf("read mach-o load command: declared size (%d) too small for number of commands (%d)",
			hdr.LoadCommandRegionSize, hdr.LoadCommandCount)
		return &CommandReader{err: err}
	}
	return &CommandReader{
		r:                 r,
		remainingCommands: hdr.LoadCommandCount,
		remainingBytes:    hdr.LoadCommandRegionSize,
		byteOrder:         hdr.ByteOrder,

		// Special sentinel value for first read.
		nbuf: LoadCommandMinSize + 1,
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
	for r.nbuf < LoadCommandMinSize && r.err == nil {
		r.fillInfo(LoadCommandMinSize)
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
	if r.remainingBytes < LoadCommandMinSize {
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
// The first [LoadCommandMinSize] bytes of a load command are always the its type and size.
// After reading the first [LoadCommandMinSize] bytes,
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
		if size < LoadCommandMinSize {
			r.currRemainingBytes = 0
			if r.err == nil {
				r.err = errCommandSizeTooSmall
			}
		} else {
			r.currRemainingBytes = size - LoadCommandMinSize
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
	if r.nbuf < 4 || r.nbuf > LoadCommandMinSize {
		return 0, false
	}
	return LoadCmd(r.byteOrder.Uint32(r.buf[:])), true
}

// Size returns the total size of the current command in bytes.
// ok is false if the first [LoadCommandMinSize] bytes of the command haven't been read yet
// or the size is invalid.
// Size will never return a value less than [LoadCommandMinSize].
func (r *CommandReader) Size() (_ uint32, ok bool) {
	size, ok := r.size()
	if !ok || size < LoadCommandMinSize {
		return LoadCommandMinSize, false
	}
	return size, r.currRemainingBytes <= r.remainingBytes
}

func (r *CommandReader) size() (_ uint32, ok bool) {
	return r.byteOrder.Uint32(r.buf[4:]), r.nbuf == LoadCommandMinSize
}

var (
	errCommandSizeTooSmall = errors.New("read mach-o load command: invalid size for command")
	errCommandSizeTooLarge = errors.New("read mach-o load command: command array larger than declared size")
	errCommandTrailingData = errors.New("read mach-o load command: command array smaller than declared size")
)

func unmarshalLoadCommand(byteOrder binary.ByteOrder, data []byte) (LoadCmd, error) {
	if len(data) < LoadCommandMinSize {
		return 0, fmt.Errorf("unmarshal mach-o load command: short buffer")
	}
	cmd := LoadCmd(byteOrder.Uint32(data))
	size := byteOrder.Uint32(data[4:])
	if int64(size) != int64(len(data)) {
		return cmd, fmt.Errorf("unmarshal %v: size (%d) does not match buffer (%d)", cmd, size, len(data))
	}
	return cmd, nil
}

func nameToString(name []byte) string {
	i := bytes.IndexByte(name, 0)
	if i < 0 {
		return string(name)
	}
	return string(name[:i])
}

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
