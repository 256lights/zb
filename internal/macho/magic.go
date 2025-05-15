// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package macho

import "encoding/binary"

// MagicNumberSize is the size (in bytes) of the magic number at the start of the Mach-O file.
const MagicNumberSize = 4

type magicNumber [MagicNumberSize]byte

func (magic magicNumber) isKnown() bool {
	return magic.isUniversal() || magic.isLittleEndian() || magic.isBigEndian()
}

func (magic magicNumber) isUniversal() bool {
	return magic == magicNumber{0xca, 0xfe, 0xba, 0xbe}
}

func (magic magicNumber) isBigEndian() bool {
	return magic[0] == 0xfe &&
		magic[1] == 0xed &&
		magic[2] == 0xfa &&
		(magic[3] == 0xce || magic[3] == 0xcf)
}

func (magic magicNumber) isLittleEndian() bool {
	return magic[3] == 0xfe &&
		magic[2] == 0xed &&
		magic[1] == 0xfa &&
		(magic[0] == 0xce || magic[0] == 0xcf)
}

func (magic magicNumber) byteOrder() binary.ByteOrder {
	switch {
	case magic.isBigEndian():
		return binary.BigEndian
	case magic.isLittleEndian():
		return binary.LittleEndian
	default:
		return nil
	}
}

func (magic magicNumber) is32Bit() bool {
	return magic == magicNumber{0xfe, 0xed, 0xfa, 0xce} ||
		magic == magicNumber{0xce, 0xfa, 0xed, 0xfe}
}

func (magic magicNumber) is64Bit() bool {
	return magic == magicNumber{0xfe, 0xed, 0xfa, 0xcf} ||
		magic == magicNumber{0xcf, 0xfa, 0xed, 0xfe}
}

// IsSingleArchitecture reports whether head starts with the Mach-O magic number
// for a single-architecture Mach-O file.
// IsSingleArchitecture will always report false if len(head) < [MagicNumberSize].
func IsSingleArchitecture(head []byte) bool {
	if len(head) < MagicNumberSize {
		return false
	}
	magic := magicNumber(head)
	return magic.isLittleEndian() || magic.isBigEndian()
}

// IsUniversal reports whether head starts with the Mach-O magic number
// for a multi-architecture Mach-O file.
// IsUniversal will always report false if len(head) < [MagicNumberSize].
func IsUniversal(head []byte) bool {
	if len(head) < MagicNumberSize {
		return false
	}
	magic := magicNumber(head)
	return magic.isUniversal()
}
