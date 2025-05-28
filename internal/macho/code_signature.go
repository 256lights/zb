// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

//go:generate go tool stringer -type=CodeSignatureMagic,SuperBlobSlot,HashType -linecomment -output=code_signature_string.go

package macho

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"iter"
	"math"
	"slices"
)

// CodeSignatureMagic is an enumeration of types of [CodeSignatureBlob].
type CodeSignatureMagic uint32

// Known [CodeSignatureMagic] values.
const (
	CodeSignatureMagicRequirement          CodeSignatureMagic = 0xfade0c00
	CodeSignatureMagicRequirements         CodeSignatureMagic = 0xfade0c01
	CodeSignatureMagicCodeDirectory        CodeSignatureMagic = 0xfade0c02
	CodeSignatureMagicEmbeddedSignature    CodeSignatureMagic = 0xfade0cc0
	CodeSignatureMagicEmbeddedEntitlements CodeSignatureMagic = 0xfade7171
	CodeSignatureMagicDetachedSignature    CodeSignatureMagic = 0xfade0cc1
	CodeSignatureMagicBlobWrapper          CodeSignatureMagic = 0xfade0b01
)

// CodeSignatureBlobMinSize is the minimum size in bytes of a serialized [CodeSignatureBlob].
const CodeSignatureBlobMinSize = 8

// A CodeSignatureBlob represents a single record in a Mach-O code signature.
type CodeSignatureBlob struct {
	Magic CodeSignatureMagic
	Data  []byte
}

// AppendBinary marshals blob as a Mach-O code signature blob
// and appends the result to dst.
func (blob CodeSignatureBlob) AppendBinary(dst []byte) ([]byte, error) {
	if len(blob.Data) > math.MaxUint32-CodeSignatureBlobMinSize {
		return dst, fmt.Errorf("marshal mach-o code signature blob: data too large (%d bytes)", len(blob.Data))
	}
	dst = binary.BigEndian.AppendUint32(dst, uint32(blob.Magic))
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(blob.Data)+CodeSignatureBlobMinSize))
	dst = append(dst, blob.Data...)
	return dst, nil
}

// MarshalBinary marshals blob as a Mach-O code signature blob.
func (blob CodeSignatureBlob) MarshalBinary() ([]byte, error) {
	return blob.AppendBinary(nil)
}

// UnmarshalBinary unmarshals the Mach-O code signature blob into blob.
func (blob *CodeSignatureBlob) UnmarshalBinary(data []byte) error {
	parsed, err := parseBlob(data)
	if err != nil {
		return fmt.Errorf("unmarshal mach-o code signature blob: %v", err)
	}
	size := binary.BigEndian.Uint32(data[4:])
	if int64(size) != int64(len(data)) {
		return fmt.Errorf("unmarshal mach-o code signature blob: %v", err)
	}
	blob.Magic = parsed.Magic
	blob.Data = slices.Clone(parsed.Data)
	return nil
}

func parseBlob(data []byte) (CodeSignatureBlob, error) {
	if len(data) < CodeSignatureBlobMinSize {
		return CodeSignatureBlob{}, errors.New("short buffer")
	}
	size := binary.BigEndian.Uint32(data[4:])
	if int64(size) != int64(len(data)) {
		return CodeSignatureBlob{}, fmt.Errorf("size (%d) does not match buffer (%d)", size, len(data))
	}
	return CodeSignatureBlob{
		Magic: CodeSignatureMagic(binary.BigEndian.Uint32(data)),
		Data:  data[CodeSignatureBlobMinSize:],
	}, nil
}

// SuperBlob is a container of [CodeSignatureBlob].
type SuperBlob struct {
	Magic CodeSignatureMagic
	Blobs []SuperBlobEntry
}

// UnmarshalBinary unmarshals a Mach-O code signature blob as a [SuperBlob].
func (blob *SuperBlob) UnmarshalBinary(data []byte) error {
	const minSize = 12
	if len(data) < minSize {
		return fmt.Errorf("unmarshal mach-o code signature super blob: short buffer")
	}
	parsed, err := parseBlob(data)
	if err != nil {
		return fmt.Errorf("unmarshal mach-o code signature super blob: %v", err)
	}
	blob.Magic = parsed.Magic

	count := binary.BigEndian.Uint32(data[8:])
	const indexStart = minSize
	indexEnd := indexStart + blobIndexSize*int64(count)
	if indexEnd > int64(len(data)) {
		return fmt.Errorf("unmarshal mach-o code signature super blob: short buffer for %d blobs", count)
	}
	if count == 0 {
		blob.Blobs = nil
		if indexEnd < int64(len(data)) {
			return fmt.Errorf("unmarshal mach-o code signature super blob: trailing data")
		}
		return nil
	}

	// Ensure that blobs are contiguous
	// and fill the region from the end of the blob indices to the end of the data.
	blobRegions := make([][2]int, 0, count)
	for _, index := range blobIndexSeq(data[indexStart:indexEnd]) {
		if int64(index.offset) < indexEnd || int64(index.offset)+CodeSignatureBlobMinSize > int64(len(data)) {
			return fmt.Errorf("unmarshal mach-o code signature super blob: blob offset %d out of bounds", index.offset)
		}
		size := binary.BigEndian.Uint32(data[index.offset+4:])
		endOffset := int64(index.offset) + int64(size)
		if size < CodeSignatureBlobMinSize || endOffset > int64(len(data)) {
			return fmt.Errorf("unmarshal mach-o code signature super blob: blob at offset %d has invalid size", index.offset)
		}

		regionIndex, hasOffset := slices.BinarySearchFunc(blobRegions, int(index.offset), func(region [2]int, offset int) int {
			return cmp.Compare(region[0], offset)
		})
		if hasOffset {
			return fmt.Errorf("unmarshal mach-o code signature super blob: multiple blobs use offset %d", index.offset)
		}
		blobRegions = slices.Insert(blobRegions, regionIndex, [2]int{int(index.offset), int(endOffset)})
	}
	if blobRegions[0][0] != int(indexEnd) {
		return fmt.Errorf("unmarshal mach-o code signature super blob: gap at offset %d", indexEnd)
	}
	if blobRegions[len(blobRegions)-1][1] != len(data) {
		return fmt.Errorf("unmarshal mach-o code signature super blob: trailing data")
	}
	for i := range blobRegions[:len(blobRegions)-1] {
		if blobRegions[i][1] < blobRegions[i+1][0] {
			return fmt.Errorf("unmarshal mach-o code signature super blob: gap at offset %d", blobRegions[i][1])
		}
		if blobRegions[i][1] > blobRegions[i+1][0] {
			return fmt.Errorf("unmarshal mach-o code signature super blob: overlap at offset %d", blobRegions[i+1][0])
		}
	}

	// Blobs have been validated. Fill the array.
	blob.Blobs = make([]SuperBlobEntry, 0, count)
	for i, index := range blobIndexSeq(data[indexStart:indexEnd]) {
		entry := SuperBlobEntry{Type: index.type_}
		size := binary.BigEndian.Uint32(data[index.offset+4:])
		if err := entry.Blob.UnmarshalBinary(data[index.offset : index.offset+size]); err != nil {
			return fmt.Errorf("unmarshal mach-o code signature super blob: blob[%d]: %v", i, err)
		}
		blob.Blobs = append(blob.Blobs, entry)
	}

	return nil
}

const blobIndexSize = 8

type blobIndex struct {
	type_  SuperBlobSlot
	offset uint32
}

func blobIndexSeq(index []byte) iter.Seq2[int, blobIndex] {
	return func(yield func(int, blobIndex) bool) {
		for i, j := 0, 0; j+blobIndexSize <= len(index); i, j = i+1, j+blobIndexSize {
			ent := blobIndex{
				type_:  SuperBlobSlot(binary.BigEndian.Uint32(index[j:])),
				offset: binary.BigEndian.Uint32(index[j+4:]),
			}
			if !yield(i, ent) {
				return
			}
		}
	}
}

// SuperBlobSlot is an enumeration of types used in [SuperBlobEntry].
type SuperBlobSlot uint32

// Known [SuperBlobSlot] values.
const (
	SuperBlobCodeDirectorySlot SuperBlobSlot = 0 // CSSLOT_CODEDIRECTORY
	SuperBlobInfoSlot          SuperBlobSlot = 1 // CSSLOT_INFOSLOT
	SuperBlobRequirementsSlot  SuperBlobSlot = 2 // CSSLOT_REQUIREMENTS
	SuperBlobResourceDirSlot   SuperBlobSlot = 3 // CSSLOT_RESOURCEDIR
	SuperBlobApplicationSlot   SuperBlobSlot = 4 // CSSLOT_APPLICATION
	SuperBlobEntitlementsSlot  SuperBlobSlot = 5 // CSSLOT_ENTITLEMENTS
)

// SuperBlobEntry is a [CodeSignatureBlob] with a "slot" (its intended usage).
type SuperBlobEntry struct {
	Type SuperBlobSlot
	Blob CodeSignatureBlob
}

// CodeSignatureFlags is a bitset of flags used in [CodeDirectory].
type CodeSignatureFlags uint32

// [CodeSignatureFlags] permitted to be used in the Mach-O format.
const (
	// CodeSignatureAdHoc indicates that the bundle is ad hoc signed.
	CodeSignatureAdHoc CodeSignatureFlags = 0x00000002 // CS_ADHOC
	// CodeSignatureHard requests to not load invalid pages.
	CodeSignatureHard CodeSignatureFlags = 0x00000100 // CS_HARD
	// CodeSignatureKill requests to kill the process if it becomes invalid.
	CodeSignatureKill CodeSignatureFlags = 0x00000200 // CS_KILL
	// CodeSignatureCheckExpiration forces expiration checking.
	CodeSignatureCheckExpiration CodeSignatureFlags = 0x00000400 // CS_CHECK_EXPIRATION
	// CodeSignatureRestrict tells dyld to treat the bundle as restricted.
	CodeSignatureRestrict CodeSignatureFlags = 0x00000800 // CS_RESTRICT
	// CodeSignatureEnforcement indicates that the bundle requires enforcement.
	CodeSignatureEnforcement CodeSignatureFlags = 0x00001000 // CS_ENFORCEMENT
	// CodeSignatureRequireLV indicates that the bundle requires library validation
	CodeSignatureRequireLV CodeSignatureFlags = 0x00002000 // CS_REQUIRE_LV
	// CodeSignatureRuntime requests to apply hardened runtime policies.
	CodeSignatureRuntime CodeSignatureFlags = 0x00010000 // CS_RUNTIME
	// CodeSignatureLinkerSigned indicates the bundle was automatically signed by the linker.
	CodeSignatureLinkerSigned CodeSignatureFlags = 0x00020000 // CS_LINKER_SIGNED
)

// HashType is an enumeration of cryptographic hash algorithms used in [CodeDirectory].
type HashType uint8

// Known [HashType] values.
const (
	HashTypeSHA1            HashType = 1 // CS_HASHTYPE_SHA1
	HashTypeSHA256          HashType = 2 // CS_HASHTYPE_SHA256
	HashTypeSHA256Truncated HashType = 3 // CS_HASHTYPE_SHA256_TRUNCATED
	HashTypeSHA384          HashType = 4 // CS_HASHTYPE_SHA384
)

// Size returns the number of bytes the hash type is expected to produce.
func (ht HashType) Size() (_ int, ok bool) {
	switch ht {
	case HashTypeSHA1, HashTypeSHA256Truncated:
		return 20, true
	case HashTypeSHA256:
		return 32, true
	case HashTypeSHA384:
		return 48, true
	default:
		return 0, false
	}
}

// CodeDirectory represents a parsed [CodeSignatureBlob]
// for the [CodeSignatureMagicCodeDirectory] magic number.
type CodeDirectory struct {
	Flags            CodeSignatureFlags
	HashData         []byte
	Identifier       string
	SpecialSlotCount uint32
	CodeLimit        uint64
	HashType         HashType
	Platform         uint8
	PageSize         Alignment
	TeamIdentifier   string

	ExecutableSegmentBase  uint64
	ExecutableSegmentLimit uint64
	ExecutableSegmentFlags uint64
}

// HashSlots returns an iterator over the hash slots (including the special ones).
func (cd *CodeDirectory) HashSlots() iter.Seq2[int, []byte] {
	return func(yield func(int, []byte) bool) {
		size, ok := cd.HashType.Size()
		if !ok {
			return
		}
		data := cd.HashData
		for i := 0; len(data) >= size; i, data = i+1, data[size:] {
			if !yield(i, data[:size]) {
				return
			}
		}
	}
}

// HashSlotCount returns the number of hash slots present in cd.HashData
// based on cd.HashType.
func (cd *CodeDirectory) HashSlotCount() int {
	size, ok := cd.HashType.Size()
	if !ok {
		return 0
	}
	return len(cd.HashData) / size
}

// UnmarshalBinary parses a Mach-O code signature blob as a [CodeDirectory].
func (cd *CodeDirectory) UnmarshalBinary(data []byte) error {
	const endEarliest = 44
	if len(data) < endEarliest {
		return fmt.Errorf("unmarshal mach-o code directory: short buffer")
	}
	if size := binary.BigEndian.Uint32(data[4:]); int64(size) != int64(len(data)) {
		return fmt.Errorf("unmarshal mach-o code directory: size (%d) does not match buffer (%d)", size, len(data))
	}
	if got, want := CodeSignatureMagic(binary.BigEndian.Uint32(data)), CodeSignatureMagicCodeDirectory; got != want {
		return fmt.Errorf("unmarshal mach-o code directory: magic number is %v instead of %v", got, want)
	}
	version := binary.BigEndian.Uint32(data[8:])
	cd.Flags = CodeSignatureFlags(binary.BigEndian.Uint32(data[12:]))
	hashOffset := binary.BigEndian.Uint32(data[16:])
	identifierOffset := binary.BigEndian.Uint32(data[20:])
	cd.SpecialSlotCount = binary.BigEndian.Uint32(data[24:])
	codeSlotCount := binary.BigEndian.Uint32(data[28:])
	cd.CodeLimit = uint64(binary.BigEndian.Uint32(data[32:]))
	hashSize := data[36]
	cd.HashType = HashType(data[37])
	if wantSize, ok := cd.HashType.Size(); !ok {
		return fmt.Errorf("unmarshal mach-o code directory: unrecognized hash type %v", cd.HashType)
	} else if int(hashSize) != wantSize {
		return fmt.Errorf("unmarshal mach-o code directory: incorrect hash size (got %d; expected %d)", hashSize, wantSize)
	}
	cd.Platform = data[38]
	cd.PageSize = Alignment(data[39])
	if _, ok := cd.PageSize.Bytes(); !ok {
		return fmt.Errorf("unmarshal mach-o code directory: page size (%v) too large", cd.PageSize)
	}
	if !isZero(data[40:44]) {
		return fmt.Errorf("unmarshal mach-o code directory: reserved spare2 not zero")
	}

	directoryEnd := endEarliest
	var teamOffset uint32
	if version >= 0x20100 {
		if scatterOffset := binary.BigEndian.Uint32(data[44:]); scatterOffset != 0 {
			return fmt.Errorf("unmarshal mach-o code directory: scatter vector not supported")
		}
		directoryEnd = 48

		if version >= 0x20200 {
			teamOffset = binary.BigEndian.Uint32(data[48:])
			directoryEnd = 52

			if version >= 0x20300 {
				if !isZero(data[52:56]) {
					return fmt.Errorf("unmarshal mach-o code directory: reserved spare3 not zero")
				}
				if codeLimit64 := binary.BigEndian.Uint64(data[56:]); codeLimit64 != 0 {
					cd.CodeLimit = codeLimit64
				}
				directoryEnd = 64

				if version >= 0x20400 {
					cd.ExecutableSegmentBase = binary.BigEndian.Uint64(data[64:])
					cd.ExecutableSegmentLimit = binary.BigEndian.Uint64(data[72:])
					cd.ExecutableSegmentFlags = binary.BigEndian.Uint64(data[80:])
					directoryEnd = 88
				}
			}
		}
	}

	if identifierOffset < uint32(directoryEnd) || int64(identifierOffset) >= int64(len(data)) {
		return fmt.Errorf("unmarshal mach-o code directory: identifier offset (%d) out of range [%d, %d)", identifierOffset, directoryEnd, len(data))
	}
	var err error
	cd.Identifier, err = parseCString(data[identifierOffset:])
	if err != nil {
		return fmt.Errorf("unmarshal mach-o code directory: identifier: %v", err)
	}
	if teamOffset != 0 {
		if teamOffset < uint32(directoryEnd) || int64(teamOffset) >= int64(len(data)) {
			return fmt.Errorf("unmarshal mach-o code directory: team offset (%d) out of range [%d, %d)", teamOffset, directoryEnd, len(data))
		}
		cd.TeamIdentifier, err = parseCString(data[teamOffset:])
		if err != nil {
			return fmt.Errorf("unmarshal mach-o code directory: team identifier: %v", err)
		}
	}

	hashEndOffset := int64(hashOffset) + int64(hashSize)*(int64(cd.SpecialSlotCount)+int64(codeSlotCount))
	if hashOffset < uint32(directoryEnd) || hashEndOffset > int64(len(data)) {
		return fmt.Errorf("unmarshal mach-o code directory: hash offset (%d) out of range [%d, %d)", hashOffset, directoryEnd, int64(len(data))-(hashEndOffset-int64(hashOffset)))
	}
	cd.HashData = slices.Clone(data[hashOffset:hashEndOffset])

	return nil
}

func parseCString(b []byte) (string, error) {
	i := bytes.IndexByte(b, 0)
	if i < 0 {
		return "", errors.New("missing nul byte")
	}
	return string(b[:i]), nil
}

func isZero(b []byte) bool {
	for _, bb := range b {
		if bb != 0 {
			return false
		}
	}
	return true
}
