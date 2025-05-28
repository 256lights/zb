// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package zbstore

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"

	"zb.256lights.llc/pkg/internal/macho"
	"zb.256lights.llc/pkg/internal/xio"
	"zombiezen.com/go/nix"
)

// A Rewriter represents a modification to a store object to account for self-references.
// It must be safe to call methods on a Rewriter from multiple goroutines concurrently.
type Rewriter interface {
	// Rewrite returns the bytes to replace in the store object
	// starting at WriteOffset.
	// context is a reader over the rewritten bytes
	// starting at ReadOffset and ending at WriteOffset.
	Rewrite(newDigest string, context io.Reader) ([]byte, error)
	// WriteOffset returns the offset from the beginning of the NAR file
	// of the first byte that needs to be rewritten.
	WriteOffset() int64
	// ReadOffset returns the offset from the beginning of the NAR file
	// of the first byte that needs to be read
	// to compute the rewrite.
	// If no additional context is required,
	// ReadOffset shall return the same offset as WriteOffset.
	// A Rewriter's ReadOffset must be less than or equal to its WriteOffset.
	ReadOffset() int64
}

func compareReadOffset(r Rewriter, offset int64) int {
	return cmp.Compare(r.ReadOffset(), offset)
}

func compareReadOffsets(loc1, loc2 Rewriter) int {
	return cmp.Compare(loc1.ReadOffset(), loc2.ReadOffset())
}

func compareWriteOffsets(loc1, loc2 Rewriter) int {
	return cmp.Compare(loc1.WriteOffset(), loc2.WriteOffset())
}

// SelfReference is a [Rewriter] that represents a self-reference.
type SelfReference interface {
	Rewriter

	// AppendReferenceText appends the textual representation of the self-reference location
	// as used in computing the content address to dst.
	// Such strings must not contain a "|" character.
	AppendReferenceText(dst []byte) ([]byte, error)
}

// Rewrite applies rewriters to f.
// newDigest is the string to replace self-references with.
// The length of newDigest must be the same as the length of the digest passed to [SourceSHA256ContentAddress].
//
// f is treated like it starts at baseOffset bytes from the beginning of a NAR serialization.
// This can be used to apply a subset of rewrites from [SourceSHA256ContentAddress]
// to a particular file inside the store object.
func Rewrite(f io.ReadWriteSeeker, baseOffset int64, newDigest string, rewriters []Rewriter) error {
	if newDigest == "" {
		return fmt.Errorf("rewrite hash: digest empty")
	}
	if baseOffset < 0 {
		return fmt.Errorf("rewrite hash: negative base offset (%d)", baseOffset)
	}
	if !slices.IsSortedFunc(rewriters, compareWriteOffsets) {
		rewriters = slices.Clone(rewriters)
		slices.SortStableFunc(rewriters, compareWriteOffsets)
	}

	for len(rewriters) > 0 {
		readOffset := rewriters[0].ReadOffset()
		if readOffset < baseOffset {
			return fmt.Errorf("rewrite hash: rewrite offset (%d) < base offset (%d)", readOffset, baseOffset)
		}
		if _, err := f.Seek(readOffset-baseOffset, io.SeekStart); err != nil {
			return fmt.Errorf("rewrite hash: %v", err)
		}
		writeOffset := rewriters[0].WriteOffset()
		var b []byte
		if readOffset == writeOffset {
			var err error
			b, err = rewriters[0].Rewrite(newDigest, xio.Null())
			if err != nil {
				return fmt.Errorf("rewrite hash: %v", err)
			}
		} else {
			var err error
			b, err := rewriters[0].Rewrite(newDigest, io.LimitReader(f, writeOffset-readOffset))
			if err != nil {
				return fmt.Errorf("rewrite hash: %v", err)
			}
			if len(rewriters) >= 2 && rewriters[1].WriteOffset() < writeOffset+int64(len(b)) {
				return fmt.Errorf("rewrite hash: internal error: overlapping rewrite")
			}
			if _, err := f.Seek(writeOffset-baseOffset, io.SeekStart); err != nil {
				return fmt.Errorf("rewrite hash: %v", err)
			}
		}

		if _, err := f.Write(b); err != nil {
			return fmt.Errorf("rewrite hash: %v", err)
		}
		rewriters = rewriters[1:]
	}

	return nil
}

// A SelfReferenceOffset is a [SelfReference]
// that represents a simple textual reference to the store path's digest.
// It is stored as an offset in bytes relative to the beginning of the NAR file.
type SelfReferenceOffset int64

// ReadOffset returns the offset as an int64.
func (offset SelfReferenceOffset) ReadOffset() int64 {
	return int64(offset)
}

// WriteOffset returns the offset as an int64.
func (offset SelfReferenceOffset) WriteOffset() int64 {
	return int64(offset)
}

// Rewrite returns newDigest.
func (offset SelfReferenceOffset) Rewrite(newDigest string, context io.Reader) ([]byte, error) {
	return []byte(newDigest), nil
}

// AppendReferenceText implements [SelfReference]
// by appending the decimal representation of the offset to dst.
func (offset SelfReferenceOffset) AppendReferenceText(dst []byte) ([]byte, error) {
	if offset < 0 {
		return dst, fmt.Errorf("append self-reference: offset is negative")
	}
	return strconv.AppendInt(dst, int64(offset), 10), nil
}

// MachOSignatureRewrite is a [Rewriter]
// that represents a recomputation of an ad hoc Mach-O code signature
// due to self references inside the executable section.
// A multi-architecture Mach-O file may contain multiple rewrites:
// one for each architecture.
type MachOSignatureRewrite struct {
	// ImageStart is the offset in bytes that the Mach-O file starts
	// relative to the beginning of the NAR file.
	ImageStart int64
	// CodeEnd is the offset in bytes to the first byte of the code signature section
	// relative to the beginning of the NAR file.
	// CodeEnd is also the end of the image signature range.
	CodeEnd int64
	// PageSize is the number of bytes to hash for each hash slot.
	// A value <=1 indicates "infinity",
	// meaning that the entirety of the image signature range
	// turns into a single signature.
	PageSize int
	// HashType is the hash algorithm in use.
	HashType nix.HashType
	// HashOffset is the offset in bytes of the hash slot element at index zero
	// relative to the beginning of the NAR file.
	HashOffset int64
}

// ReadOffset returns rewrite.ImageStart.
func (rewrite *MachOSignatureRewrite) ReadOffset() int64 {
	return rewrite.ImageStart
}

// WriteOffset returns rewrite.HashOffset.
func (rewrite *MachOSignatureRewrite) WriteOffset() int64 {
	return rewrite.HashOffset
}

// Rewrite is equivalent to rewrite.Sign(nil, context).
func (rewrite *MachOSignatureRewrite) Rewrite(newDigest string, context io.Reader) ([]byte, error) {
	return rewrite.Sign(nil, context)
}

// CodeLimit returns the number of bytes that need to be hashed.
func (rewrite *MachOSignatureRewrite) CodeLimit() uint64 {
	return uint64(rewrite.codeLimit())
}

func (rewrite *MachOSignatureRewrite) codeLimit() int64 {
	return rewrite.CodeEnd - rewrite.ImageStart
}

// HashSlots returns the number of hashes that this rewrite will produce.
func (rewrite *MachOSignatureRewrite) HashSlots() int {
	codeLimit := rewrite.codeLimit()
	if codeLimit < 0 {
		return 0
	}
	return int(hashSlotsForCodeLimit(uint64(codeLimit), uint64(rewrite.PageSize)))
}

func hashSlotsForCodeLimit(codeLimit uint64, pageSize uint64) int64 {
	if pageSize <= 1 {
		return 1
	}
	return int64((codeLimit + (pageSize - 1)) / pageSize)
}

// HashSize returns the total size of all hash slots in bytes.
func (rewrite *MachOSignatureRewrite) HashSize() int {
	return rewrite.HashSlots() * rewrite.HashType.Size()
}

// IsPageSizeInfinite reports whether the entirety of the image signature range
// turns into a single signature.
func (rewrite *MachOSignatureRewrite) IsPageSizeInfinite() bool {
	return rewrite.PageSize <= 1
}

// Sign computes the code signature hash slots for the Mach-O image
// and appends them to dst.
// code should be a reader that starts at rewrite.ImageStart.
// Sign will not read more than [*MachOSignatureRewrite.CodeLimit] bytes from code.
func (rewrite *MachOSignatureRewrite) Sign(dst []byte, code io.Reader) ([]byte, error) {
	codeLimit := rewrite.codeLimit()
	if codeLimit < 0 {
		return dst, fmt.Errorf("sign mach-o binary: code end (%d) is before image start (%d)", rewrite.CodeEnd, rewrite.ImageStart)
	}
	if !rewrite.IsPageSizeInfinite() && hashSlotsForCodeLimit(uint64(codeLimit), uint64(rewrite.PageSize)) > math.MaxInt/int64(rewrite.HashType.Size()) {
		return dst, fmt.Errorf("sign mach-o binary: hash slot size too large")
	}

	h := nix.NewHasher(rewrite.HashType)
	if rewrite.IsPageSizeInfinite() {
		if _, err := io.CopyN(h, code, codeLimit); err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return dst, fmt.Errorf("sign mach-o binary: %w", err)
		}
		dst = h.Sum(dst)
		return dst, nil
	}

	for n := int64(0); n < codeLimit; {
		pageSize := min(int64(rewrite.PageSize), codeLimit-n)
		if _, err := io.CopyN(h, code, pageSize); err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return dst, fmt.Errorf("sign mach-o binary: %w", err)
		}
		codeLimit -= pageSize
		dst = h.Sum(dst)
		h.Reset()
	}
	return dst, nil
}

type rewritableCodeDirectory struct {
	macho.CodeDirectory
	hashSlotsStart int
	hashSlotsEnd   int
}

// findRewritableCodeDirectory parses a code signature [macho.SuperBlob]
// and checks whether it fits the format of what can be rewritten.
func findRewritableCodeDirectory(signatureBytes []byte) (*rewritableCodeDirectory, error) {
	const (
		superBlobMinSize = 12
		blobIndexSize    = 8
	)

	signatureBlob := new(macho.SuperBlob)
	if err := signatureBlob.UnmarshalBinary(signatureBytes); err != nil {
		return nil, err
	}
	if signatureBlob.Magic != macho.CodeSignatureMagicEmbeddedSignature {
		return nil, fmt.Errorf("signature not a %v", signatureBlob.Magic)
	}
	if len(signatureBlob.Blobs) != 1 {
		return nil, fmt.Errorf("blob count %d", len(signatureBlob.Blobs))
	}
	if got, want := signatureBlob.Blobs[0].Type, macho.SuperBlobCodeDirectorySlot; got != want {
		return nil, fmt.Errorf("slot is %v instead of %v", got, want)
	}
	if got, want := signatureBlob.Blobs[0].Blob.Magic, macho.CodeSignatureMagicCodeDirectory; got != want {
		return nil, fmt.Errorf("slot is %v instead of %v", got, want)
	}

	// codeDirectoryOffset is immediately after the super blob header.
	// [*macho.SuperBlob.UnmarshalBinary] guarantees that there are no gaps between blobs.
	// Since we verify there is a single blob, it will appear at this offset.
	const codeDirectoryOffset = superBlobMinSize + blobIndexSize
	codeDirectoryBlob := signatureBytes[codeDirectoryOffset:]

	result := new(rewritableCodeDirectory)
	if err := result.UnmarshalBinary(codeDirectoryBlob); err != nil {
		return nil, err
	}
	if result.Flags != macho.CodeSignatureAdHoc|macho.CodeSignatureLinkerSigned {
		return nil, fmt.Errorf("unsupported flags")
	}
	if result.SpecialSlotCount != 0 {
		return nil, fmt.Errorf("special hash slots not supported")
	}
	if pageSize, ok := result.PageSize.Bytes(); !ok {
		return nil, fmt.Errorf("page size (%v) too large", result.PageSize)
	} else if got, want := int64(result.HashSlotCount()), hashSlotsForCodeLimit(result.CodeLimit, pageSize)+int64(result.SpecialSlotCount); got != want {
		return nil, fmt.Errorf("found %d hash slots but expected %d based on codeLimit=%d and pageSize=%v", got, want, result.CodeLimit, result.PageSize)
	}
	hashOffset := binary.BigEndian.Uint32(codeDirectoryBlob[16:])
	result.hashSlotsStart = codeDirectoryOffset + int(hashOffset)
	result.hashSlotsEnd = result.hashSlotsStart + len(result.HashData)
	return result, nil
}

func (cd *rewritableCodeDirectory) toRewrite(imageStart int64, codeEnd int64) (*MachOSignatureRewrite, error) {
	if imageStart < 0 {
		return nil, fmt.Errorf("negative image start")
	}
	if codeEnd < imageStart {
		return nil, fmt.Errorf("code end before image start")
	}
	rewrite := &MachOSignatureRewrite{
		ImageStart: imageStart,
		CodeEnd:    codeEnd,
	}
	if cd.PageSize != 0 {
		pageSize, ok := cd.PageSize.Bytes()
		if !ok || pageSize > math.MaxInt {
			return nil, fmt.Errorf("page size (%v) too large", cd.PageSize)
		}
		rewrite.PageSize = int(pageSize)
	}
	switch cd.HashType {
	case macho.HashTypeSHA1:
		rewrite.HashType = nix.SHA1
	case macho.HashTypeSHA256:
		rewrite.HashType = nix.SHA256
	default:
		return nil, fmt.Errorf("unsupported hash type %v", cd.HashType)
	}
	rewrite.HashOffset = codeEnd + int64(cd.hashSlotsStart)
	return rewrite, nil
}
