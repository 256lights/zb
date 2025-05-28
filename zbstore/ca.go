// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package zbstore

import (
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"iter"
	"slices"
	"strings"

	"zb.256lights.llc/pkg/internal/detect"
	"zb.256lights.llc/pkg/internal/macho"
	"zb.256lights.llc/pkg/internal/xio"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
)

// A ContentAddress is a content-addressibility assertion.
type ContentAddress = nix.ContentAddress

// FixedCAOutputPath computes the path of a store object
// with the given directory, name, content address, and reference set.
func FixedCAOutputPath(dir Directory, name string, ca nix.ContentAddress, refs References) (Path, error) {
	if err := ValidateContentAddress(ca, refs); err != nil {
		return "", fmt.Errorf("compute fixed output path for %s: %v", name, err)
	}
	h := ca.Hash()
	switch {
	case ca.IsText():
		return makeStorePath(dir, "text", h, name, refs)
	case IsSourceContentAddress(ca):
		return makeStorePath(dir, "source", h, name, refs)
	default:
		h2 := nix.NewHasher(nix.SHA256)
		h2.WriteString("fixed:out:")
		h2.WriteString(methodOfContentAddress(ca).prefix())
		h2.WriteString(h.Base16())
		h2.WriteString(":")
		return makeStorePath(dir, "output:out", h2.SumHash(), name, References{})
	}
}

// ValidateContentAddress checks whether the combination of the content address
// and set of references is one that will be accepted by a zb store.
// If not, it returns an error describing the issue.
func ValidateContentAddress(ca nix.ContentAddress, refs References) error {
	htype := ca.Hash().Type()
	isFixedOutput := ca.IsFixed() && !IsSourceContentAddress(ca)
	switch {
	case ca.IsZero():
		return fmt.Errorf("null content address")
	case ca.IsText() && htype != nix.SHA256:
		return fmt.Errorf("text must be content-addressed by %v (got %v)", nix.SHA256, htype)
	case refs.Self && ca.IsText():
		return fmt.Errorf("self-references not allowed in text")
	case !refs.IsEmpty() && isFixedOutput:
		return fmt.Errorf("references not allowed in fixed output")
	default:
		return nil
	}
}

// SelfReferenceAnalysis holds additional information about self-references
// computed by [SourceSHA256ContentAddress].
type SelfReferenceAnalysis struct {
	// Rewrites is the set of rewrites for the NAR serialization
	// required to account for self-reference digests.
	// They should be in ascending order of ReadOffset.
	Rewrites []Rewriter
	// Paths is the set of paths in the NAR serialization that contain self-reference digests.
	// They should be in ascending order of ContentOffset.
	Paths []nar.Header
}

// HasSelfReferences reports whether the analysis is non-empty.
func (analysis *SelfReferenceAnalysis) HasSelfReferences() bool {
	return analysis != nil && (len(analysis.Rewrites) > 0 || len(analysis.Paths) > 0)
}

// Path returns the header for the given path.
func (analysis *SelfReferenceAnalysis) Path(name string) *nar.Header {
	i := slices.IndexFunc(analysis.Paths, func(hdr nar.Header) bool {
		return hdr.Path == name
	})
	if i < 0 {
		return nil
	}
	return &analysis.Paths[i]
}

// RewritesInRange returns a slice of analysis.Rewrites
// whose ReadOffset values are in the range [start, end).
// If analysis.Rewrites is not in ascending order of ReadOffset,
// then RewritesInRange may not return correct results.
func (analysis *SelfReferenceAnalysis) RewritesInRange(start, end int64) []Rewriter {
	if end < start {
		return nil
	}
	firstRewrite, _ := slices.BinarySearchFunc(analysis.Rewrites, start, compareReadOffset)
	rewrites := analysis.Rewrites[firstRewrite:]
	lastRewrite, _ := slices.BinarySearchFunc(rewrites, end, compareReadOffset)
	return rewrites[:lastRewrite]
}

// SourceSHA256ContentAddress computes the content address of a "source" store object,
// given its temporary path digest (as given by [Path.Digest])
// and its NAR serialization.
// The digest is used to detect self-references:
// if the store object is known to not contain self-references,
// digest may be the empty string.
//
// See [IsSourceContentAddress] for an explanation of "source" store objects.
func SourceSHA256ContentAddress(digest string, sourceNAR io.Reader) (ca nix.ContentAddress, analysis *SelfReferenceAnalysis, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("compute source content address: %v", err)
		}
	}()

	analysis = new(SelfReferenceAnalysis)
	h := nix.NewHasher(nix.SHA256)
	if digest == "" {
		// If there are no self-references, we only have to hash the NAR.
		_, err = io.Copy(h, sourceNAR)
		if err != nil {
			return nix.ContentAddress{}, analysis, err
		}
		h.WriteString("|")
		return nix.RecursiveFileContentAddress(h.SumHash()), analysis, nil
	}

	nr := nar.NewReader(sourceNAR)
	nw := nar.NewWriter(h)
	digestReplacement := strings.Repeat("\x00", len(digest))
	for {
		hdr, err := nr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nix.ContentAddress{}, analysis, err
		}
		if strings.Contains(hdr.Path, digest) {
			return nix.ContentAddress{}, analysis, fmt.Errorf("path %s contains self-reference", hdr.Path)
		}
		if strings.Contains(hdr.LinkTarget, digest) {
			hdrClone := *hdr
			hdrClone.LinkTarget = ""
			analysis.Paths = append(analysis.Paths, hdrClone)
			for i := range indexSeq(hdr.LinkTarget, digest) {
				analysis.Rewrites = append(analysis.Rewrites, SelfReferenceOffset(hdr.ContentOffset+int64(i)))
			}
			hdr.LinkTarget = strings.ReplaceAll(hdr.LinkTarget, digest, digestReplacement)
		}
		if err := nw.WriteHeader(hdr); err != nil {
			return nix.ContentAddress{}, analysis, err
		}

		if !hdr.Mode.IsRegular() {
			continue
		}
		initialRewritesLength := len(analysis.Rewrites)
		analysis.Rewrites, err = filterFileForContentAddress(nw, analysis.Rewrites, hdr.ContentOffset, nr, digest)
		if len(analysis.Rewrites) > initialRewritesLength {
			analysis.Paths = append(analysis.Paths, *hdr)
		}
		if err != nil {
			return nix.ContentAddress{}, analysis, err
		}
	}
	if err := nw.Close(); err != nil {
		return nix.ContentAddress{}, analysis, err
	}

	// This single pipe separator differentiates this content addressing algorithm
	// from Nix's implementation as of Nix commit 2ed075ffc0f4e22f6bc6c083ef7c84e77c687605.
	// I believe it to be more correct in avoiding potential hash collisions.
	h.WriteString("|")

	var buf []byte
	for _, r := range analysis.Rewrites {
		selfRef, ok := r.(SelfReference)
		if !ok {
			continue
		}
		var err error
		buf = append(buf[:0], '|')
		buf, err = selfRef.AppendReferenceText(buf)
		if err != nil {
			return nix.ContentAddress{}, analysis, err
		}
		if text := buf[1:]; bytes.ContainsAny(text, "|") {
			return nix.ContentAddress{}, analysis, fmt.Errorf("self-reference serialization %q contains '|'", text)
		}
		h.Write(buf)
	}
	return nix.RecursiveFileContentAddress(h.SumHash()), analysis, nil
}

// filterFileForContentAddress finds all the rewrites in the store object file
// and writes a version of the file to dst
// where all the rewritten sections are replaced with zero bytes.
// The new rewrites are appended to the rewrites slice
// and filterFileForContentAddress returns the extended slice.
func filterFileForContentAddress(dst io.Writer, rewrites []Rewriter, baseOffset int64, src io.Reader, digest string) ([]Rewriter, error) {
	buf := make([]byte, 1024)
	n, err := io.ReadAtLeast(src, buf, macho.MagicNumberSize)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return rewrites, err
	}
	buf = buf[:n]
	digestReplacement := strings.Repeat("\x00", len(digest))
	hmr := detect.NewHashModuloReader(digest, digestReplacement, io.MultiReader(bytes.NewReader(buf), src))

	switch {
	case macho.IsSingleArchitecture(buf):
		codeLimit, cd, err := zeroOutMachOCodeSignature(dst, hmr)
		if err != nil {
			return rewrites, err
		}
		if cd == nil {
			rewrites = appendOffsetLocations(rewrites, baseOffset, hmr.Offsets())
			return rewrites, nil
		}
		rw, err := cd.toRewrite(baseOffset, baseOffset+codeLimit)
		if err != nil {
			// toRewrite errors are bounds checks, so if this bombs, then abort loudly.
			return rewrites, fmt.Errorf("parse mach-o binary: internal error: %v", err)
		}

		offsets := hmr.Offsets()
		rewrites = slices.Grow(rewrites, len(offsets)+1)
		i := 0
		for ; i < len(offsets) && baseOffset+offsets[i] < rw.ReadOffset(); i++ {
			rewrites = append(rewrites, SelfReferenceOffset(baseOffset+offsets[i]))
		}
		rewrites = append(rewrites, rw)
		rewrites = appendOffsetLocations(rewrites, baseOffset, offsets[i:])
		return rewrites, nil

	case macho.IsUniversal(buf):
		var err error
		initialLocationsLength := len(rewrites)
		rewrites, err = zeroOutUniversalMachOCodeSignatures(dst, rewrites, baseOffset, hmr)
		rewrites = appendOffsetLocations(rewrites, baseOffset, hmr.Offsets())
		slices.SortFunc(rewrites[initialLocationsLength:], compareReadOffsets)
		return rewrites, err

	default:
		_, err := io.Copy(dst, hmr)
		return appendOffsetLocations(rewrites, baseOffset, hmr.Offsets()), err
	}
}

func appendOffsetLocations(dst []Rewriter, base int64, offsets []int64) []Rewriter {
	for _, off := range offsets {
		dst = append(dst, SelfReferenceOffset(base+off))
	}
	return dst
}

const maxMachOBufferSize = 4 * 1024 * 1024

type referenceReader interface {
	io.Reader
	Offsets() []int64
}

func zeroOutUniversalMachOCodeSignatures(dst io.Writer, locations []Rewriter, baseOffset int64, src referenceReader) ([]Rewriter, error) {
	// Read index.
	wc := new(xio.WriteCounter)
	sink := io.MultiWriter(wc, dst)
	entries, err := macho.ReadUniversalHeader(io.TeeReader(src, sink))
	if err != nil {
		_, err := io.Copy(sink, src)
		return locations, err
	}

	// We want to rewrite as we go,
	// but there is no guarantee that the index is in sorted order.
	// TODO(maybe): Check for overlap before rewriting?
	slices.SortStableFunc(entries, func(ent1, ent2 macho.UniversalFileEntry) int {
		return cmp.Compare(ent1.Offset, ent2.Offset)
	})

	for len(entries) > 0 {
		gap := int64(entries[0].Offset) - int64(*wc)
		if gap < 0 {
			// Skip any images that we've already advanced past.
			entries = entries[1:]
			continue
		}
		if gap > 0 {
			// Copy the gap bytes through verbatim.
			if _, err := io.CopyN(sink, src, gap); err != nil {
				if err == io.EOF {
					err = nil
				}
				return locations, err
			}
		}

		imageStart := baseOffset + int64(*wc)
		codeLimit, cd, err := zeroOutMachOCodeSignature(sink, &referenceLimitedReader{
			referenceReader: src,
			N:               int64(entries[0].Size),
		})
		if err != nil {
			return locations, err
		}
		if cd != nil {
			rw, err := cd.toRewrite(imageStart, imageStart+codeLimit)
			if err != nil {
				// toRewrite errors are bounds checks, so if this bombs, then abort loudly.
				return locations, fmt.Errorf("parse mach-o binary: internal error: %v", err)
			}
			locations = append(locations, rw)
		}
		entries = entries[1:]
	}

	// Copy any trailing bytes.
	_, err = io.Copy(sink, src)
	return locations, err
}

type referenceLimitedReader struct {
	referenceReader
	N int64
}

func (l *referenceLimitedReader) Read(p []byte) (n int, err error) {
	if l.N <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > l.N {
		p = p[0:l.N]
	}
	n, err = l.referenceReader.Read(p)
	l.N -= int64(n)
	return
}

// zeroOutMachOCodeSignature copies the data from src to dst.
// If and only if src can be parsed as a single-architecture Mach-O file
// containing self-references
// with an ad hoc code signature,
// then zeroOutMachOCodeSignature will zero out the code signature's hash slots
// and return a non-nil [*rewritableCodeDirectory].
// zeroOutMachOCodeSignature will only return an error if there is an I/O error.
func zeroOutMachOCodeSignature(dst io.Writer, src referenceReader) (codeLimit int64, cd *rewritableCodeDirectory, err error) {
	initialOffsetsLength := len(src.Offsets())
	counter := new(xio.WriteCounter)
	tr := io.TeeReader(src, io.MultiWriter(counter, dst))
	header, err := macho.ReadFileHeader(tr)
	if err != nil {
		_, err := io.Copy(dst, src)
		return 0, nil, err
	}
	textSegment, signatureCommand, err := scanMachOCommands(header.ByteOrder, header.NewCommandReader(tr))
	if err != nil || int64(signatureCommand.DataOffset) < int64(*counter) {
		_, err := io.Copy(dst, src)
		return 0, nil, err
	}

	// Copy all data until the beginning of the signature region.
	if _, err := io.CopyN(dst, src, int64(signatureCommand.DataOffset)-int64(*counter)); err != nil {
		return 0, nil, err
	}

	// If we don't have any self-references before the signature,
	// then we don't have to rewrite anything.
	if len(src.Offsets()) <= initialOffsetsLength {
		_, err := io.Copy(dst, src)
		return 0, nil, err
	}

	// Read signature blob.
	signatureBytes, blobReadError := readCodeSignatureBlob(src)
	var magic macho.CodeSignatureMagic
	if len(signatureBytes) >= 4 {
		magic = macho.CodeSignatureMagic(binary.BigEndian.Uint32(signatureBytes))
	}
	if blobReadError != nil || magic != macho.CodeSignatureMagicEmbeddedSignature {
		if _, err := dst.Write(signatureBytes); err != nil {
			return 0, nil, err
		}
		if _, err := io.Copy(dst, src); err != nil {
			return 0, nil, err
		}
		return 0, nil, nil
	}

	// Parse superblob and check whether it matches the expected format.
	// TODO(maybe): Validate __TEXT segment fields.
	cd, err = findRewritableCodeDirectory(signatureBytes)
	validSignature := false
	_ = textSegment
	if err != nil {
		log.Debugf(context.TODO(), "Process Mach-O code signature: %v", err)
	} else if cd.CodeLimit != uint64(signatureCommand.DataOffset) {
		log.Debugf(context.TODO(), "Process Mach-O code signature: codeLimit (%#x) != code signature offset (%#x)", cd.CodeLimit, signatureCommand.DataOffset)
	} else {
		validSignature = true
	}
	if !validSignature {
		if _, err := dst.Write(signatureBytes); err != nil {
			return 0, nil, err
		}
		if _, err := io.Copy(dst, src); err != nil {
			return 0, nil, err
		}
		return 0, nil, nil
	}

	// Perform zeroing.
	clear(signatureBytes[cd.hashSlotsStart:cd.hashSlotsEnd])
	if _, err := dst.Write(signatureBytes); err != nil {
		return int64(signatureCommand.DataOffset), cd, err
	}

	// Copy trailing bytes, if any.
	if _, err := io.Copy(dst, src); err != nil {
		return int64(signatureCommand.DataOffset), cd, err
	}
	return int64(signatureCommand.DataOffset), cd, nil
}

// readCodeSignatureBlob reads the bytes of a serialized [macho.CodeSignatureBlob] from an [io.Reader].
// Partially read blobs will be returned.
func readCodeSignatureBlob(src io.Reader) ([]byte, error) {
	var header [macho.CodeSignatureBlobMinSize]byte
	if n, err := io.ReadFull(src, header[:]); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return header[:n], err
	}
	size := binary.BigEndian.Uint32(header[4:])
	if size < macho.CodeSignatureBlobMinSize || size > maxMachOBufferSize {
		return header[:], fmt.Errorf("read mach-o code signature blob: invalid size %d", size)
	}

	blobBytes := make([]byte, size)
	copy(blobBytes, header[:])
	n, err := io.ReadFull(src, blobBytes[len(header):])
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return blobBytes[:len(header)+n], err
}

// scanMachOCommands searches the load commands necessary for code signing.
// If there is not exactly one __TEXT segment
// and one LC_CODE_SIGNATURE command,
// then scanMachOCommands returns an error.
func scanMachOCommands(byteOrder binary.ByteOrder, commands *macho.CommandReader) (*macho.SegmentCommand, *macho.LinkeditDataCommand, error) {
	const textSegmentName = "__TEXT"
	buf := make([]byte, macho.LoadCommandMinSize)
	var textSegment *macho.SegmentCommand
	var signatureCommand *macho.LinkeditDataCommand
	for commands.Next() {
		buf = buf[:macho.LoadCommandMinSize]
		if _, err := io.ReadFull(commands, buf); err != nil {
			return nil, nil, err
		}
		c, ok := commands.Command()
		if !ok {
			return nil, nil, fmt.Errorf("internal error: missing command despite already reading bytes")
		}
		size, ok := commands.Size()
		if !ok {
			return nil, nil, fmt.Errorf("invalid command size")
		}

		switch c {
		case macho.LoadCmdSegment, macho.LoadCmdSegment64:
			if size > maxMachOBufferSize {
				return nil, nil, fmt.Errorf("%v command too large", c)
			}
			buf = slices.Grow(buf, int(size-macho.LoadCommandMinSize))
			buf = buf[:size]
			if _, err := io.ReadFull(commands, buf[macho.LoadCommandMinSize:]); err != nil {
				return nil, nil, err
			}
			newSegment := new(macho.SegmentCommand)
			if err := newSegment.UnmarshalMachO(byteOrder, buf); err != nil {
				return nil, nil, err
			}
			if newSegment.Name() == textSegmentName {
				if textSegment != nil {
					return nil, nil, fmt.Errorf("multiple " + textSegmentName + " sections")
				}
				textSegment = newSegment
			}
		case macho.LoadCmdCodeSignature:
			if signatureCommand != nil {
				return nil, nil, fmt.Errorf("multiple %v commands", c)
			}
			if size > maxMachOBufferSize {
				return nil, nil, fmt.Errorf("command too large")
			}
			buf = slices.Grow(buf, int(size-macho.LoadCommandMinSize))
			buf = buf[:size]
			if _, err := io.ReadFull(commands, buf[macho.LoadCommandMinSize:]); err != nil {
				return nil, nil, err
			}
			signatureCommand = new(macho.LinkeditDataCommand)
			if err := signatureCommand.UnmarshalMachO(byteOrder, buf); err != nil {
				return nil, nil, err
			}
		}
	}
	if err := commands.Err(); err != nil {
		return nil, nil, err
	}
	if textSegment == nil {
		return nil, nil, fmt.Errorf("missing " + textSegmentName + " segment")
	}
	if signatureCommand == nil {
		return nil, nil, fmt.Errorf("missing %v command", macho.LoadCmdCodeSignature)
	}
	return textSegment, signatureCommand, nil
}

// IsSourceContentAddress reports whether the given content address describes a "source" store object.
// "Source" store objects are those that are hashed by their NAR serialization
// and do not have a fixed (non-SHA-256) hash.
// This typically means source files imported using the "path" function,
// but can also mean content-addressed build artifacts.
func IsSourceContentAddress(ca nix.ContentAddress) bool {
	return ca.IsRecursiveFile() && ca.Hash().Type() == nix.SHA256
}

type contentAddressMethod int8

const (
	textIngestionMethod contentAddressMethod = 1 + iota
	flatFileIngestionMethod
	recursiveFileIngestionMethod
)

func methodOfContentAddress(ca nix.ContentAddress) contentAddressMethod {
	switch {
	case ca.IsText():
		return textIngestionMethod
	case ca.IsRecursiveFile():
		return recursiveFileIngestionMethod
	default:
		return flatFileIngestionMethod
	}
}

func (m contentAddressMethod) prefix() string {
	switch m {
	case textIngestionMethod:
		return "text:"
	case flatFileIngestionMethod:
		return ""
	case recursiveFileIngestionMethod:
		return "r:"
	default:
		panic("unknown content address method")
	}
}

func indexSeq(s, substr string) iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := 0; ; {
			j := strings.Index(s[i:], substr)
			if j < 0 {
				break
			}
			if !yield(i + j) {
				break
			}
			i += j + len(substr)
		}
	}
}
