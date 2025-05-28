// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package macho

import (
	"encoding/binary"
	"fmt"
	"io"
)

const universalHeaderFixedSize = 8

// ReadUniversalHeader reads a Mach-O multi-architecture header and all of its entries.
func ReadUniversalHeader(r io.Reader) ([]UniversalFileEntry, error) {
	var headerData [universalHeaderFixedSize]byte
	if _, err := io.ReadFull(r, headerData[:]); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("parse universal mach-o header: %v", err)
	}
	if n := magicNumber(headerData[:]); !n.isUniversal() {
		if !n.isLittleEndian() && !n.isBigEndian() {
			return nil, fmt.Errorf("parse univeral mach-o header: not a mach-o file")
		}
		return nil, fmt.Errorf("parse univeral mach-o header: found single-architecture mach-o")
	}
	entryCount := binary.BigEndian.Uint32(headerData[4:])
	if entryCount == 0 {
		return nil, fmt.Errorf("parse univeral mach-o header: empty")
	}
	if entryCount > 128 {
		return nil, fmt.Errorf("parse univeral mach-o header: too many entries (%d)", entryCount)
	}

	entryData := make([]byte, entryCount*universalFileEntrySize)
	n, readError := io.ReadFull(r, entryData)
	if readError == io.EOF {
		readError = io.ErrUnexpectedEOF
	}
	result := make([]UniversalFileEntry, 0, entryCount)
	for i := 0; i+universalFileEntrySize <= n; i += universalFileEntrySize {
		currData := entryData[i : i+universalFileEntrySize]
		currEntry := len(result)
		result = result[:currEntry+1]
		if err := (&result[currEntry]).UnmarshalBinary(currData); err != nil {
			return result[:currEntry], fmt.Errorf("parse universal mach-o header: %v", err)
		}
	}
	if readError != nil {
		return result, fmt.Errorf("parse universal mach-o header: %v", readError)
	}
	return result, nil
}

const universalFileEntrySize = 20

// UniversalFileEntry is a single record from a Mach-O multi-architecture file.
type UniversalFileEntry struct {
	CPU        uint32
	CPUSubtype uint32
	// Offset is the offset in bytes from the beginning of the Mach-O file
	// that this image starts at.
	Offset uint32
	// Size is the size of the image in bytes.
	Size      uint32
	Alignment Alignment
}

// UnmarshalBinary unmarshals a universal file entry in Mach-O format.
func (ent *UniversalFileEntry) UnmarshalBinary(data []byte) error {
	if len(data) < universalFileEntrySize {
		return fmt.Errorf("parse universal mach-o entry: %v", io.ErrUnexpectedEOF)
	}
	if len(data) > universalFileEntrySize {
		return fmt.Errorf("parse universal mach-o entry: trailing data")
	}
	ent.CPU = binary.BigEndian.Uint32(data)
	ent.CPUSubtype = binary.BigEndian.Uint32(data[4:])
	ent.Offset = binary.BigEndian.Uint32(data[8:])
	ent.Size = binary.BigEndian.Uint32(data[12:])
	ent.Alignment = Alignment(binary.BigEndian.Uint32(data[16:]))
	if _, ok := ent.Alignment.Bytes(); !ok {
		return fmt.Errorf("parse universal mach-o entry: alignment too large")
	}
	return nil
}
