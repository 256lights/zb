// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

// Package macho provides a parser for untrusted Mach object (Mach-O) files.
//
// See https://en.wikipedia.org/wiki/Mach-O
// and https://github.com/apple/darwin-xnu/blob/main/EXTERNAL_HEADERS/mach-o/loader.h
// for documentation on the format.
package macho

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Type is an enumeration of Mach-O file types.
type Type uint32

// Known Mach-O file types.
const (
	TypeObj    Type = 1
	TypeExec   Type = 2
	TypeDylib  Type = 6
	TypeBundle Type = 8
)

// FileHeader represents a Mach-O single-architecture file header.
type FileHeader struct {
	ByteOrder    binary.ByteOrder
	AddressWidth int
	Type         Type

	LoadCommandCount      uint32
	LoadCommandRegionSize uint32
}

// ReadFileHeader reads the header of a Mach-O single architecture file.
// After a successful call to ReadFileHeader,
// calling [*FileHeader.NewCommandReader] on the reader
// allows iteration over the load commands in the file.
func ReadFileHeader(r io.Reader) (*FileHeader, error) {
	buf := make([]byte, maxImageHeaderSize)
	if _, err := io.ReadFull(r, buf[:minImageHeaderSize]); err != nil {
		return nil, fmt.Errorf("parse mach-o header: %v", err)
	}
	hdr := new(imageHeader)
	hdrSize := imageHeaderSize(magicNumber(buf))
	if hdrSize > minImageHeaderSize {
		if _, err := io.ReadFull(r, buf[minImageHeaderSize:hdrSize]); err != nil {
			return nil, fmt.Errorf("parse mach-o header: %v", err)
		}
	}
	if err := hdr.UnmarshalBinary(buf[:hdrSize]); err != nil {
		return nil, err
	}
	result := &FileHeader{
		Type:                  hdr.fileType,
		LoadCommandCount:      hdr.loadCommandCount,
		LoadCommandRegionSize: hdr.loadCommandSize,
	}
	switch {
	case hdr.magic.isLittleEndian():
		result.ByteOrder = binary.LittleEndian
	case hdr.magic.isBigEndian():
		result.ByteOrder = binary.BigEndian
	default:
		return nil, fmt.Errorf("parse mach-o header: unknown address width")
	}
	switch {
	case hdr.magic.is32Bit():
		result.AddressWidth = 32
	case hdr.magic.is64Bit():
		result.AddressWidth = 64
	default:
		return nil, fmt.Errorf("parse mach-o header: unknown address width")
	}
	return result, nil
}

// LoadCommandsOffset returns the offset
// in bytes from the beginning of the Mach-O file
// where the load commands region begins.
func (hdr *FileHeader) LoadCommandsOffset() int64 {
	if hdr.AddressWidth == 32 {
		return minImageHeaderSize
	}
	return maxImageHeaderSize
}

// DataOffset returns the offset
// in bytes from the beginning of the Mach-O file
// where the data region begins.
func (hdr *FileHeader) DataOffset() int64 {
	return hdr.LoadCommandsOffset() + int64(hdr.LoadCommandRegionSize)
}

const (
	minImageHeaderSize = 28
	maxImageHeaderSize = 32
)

type imageHeader struct {
	magic            magicNumber
	cpu              uint32
	cpuSubtype       uint32
	fileType         Type
	loadCommandCount uint32
	loadCommandSize  uint32
	flags            uint32
}

func imageHeaderSize(magic magicNumber) int {
	if !magic.is64Bit() {
		return minImageHeaderSize
	}
	return maxImageHeaderSize
}

func (hdr *imageHeader) UnmarshalBinary(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("parse mach-o header: %v", io.ErrUnexpectedEOF)
	}
	hdr.magic = magicNumber(data)
	byteOrder := hdr.magic.byteOrder()
	if byteOrder == nil {
		return fmt.Errorf("parser mach-o header: invalid magic number %x", hdr.magic[:])
	}
	if want := imageHeaderSize(hdr.magic); len(data) < want {
		return fmt.Errorf("parse mach-o header: %v", io.ErrUnexpectedEOF)
	} else if len(data) > want {
		return fmt.Errorf("parse mach-o header: trailing data")
	}
	hdr.cpu = byteOrder.Uint32(data[4:])
	hdr.cpuSubtype = byteOrder.Uint32(data[8:])
	hdr.fileType = Type(byteOrder.Uint32(data[12:]))
	hdr.loadCommandCount = byteOrder.Uint32(data[16:])
	hdr.loadCommandSize = byteOrder.Uint32(data[20:])
	hdr.flags = byteOrder.Uint32(data[24:])
	return nil
}

// Alignment is an alignment value.
// Its raw value is the exponent of two.
type Alignment uint32

// Bytes returns the number of bytes that align represents.
func (align Alignment) Bytes() (_ uint64, ok bool) {
	if align >= 64 {
		return 1 << 63, false
	}
	return 1 << align, true
}

// String returns a string in the format "2^X",
// where X is the decimal representation of align.
func (align Alignment) String() string {
	return fmt.Sprintf("2^%d", uint32(align))
}
