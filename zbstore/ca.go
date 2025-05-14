// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package zbstore

import (
	"fmt"
	"io"
	"iter"
	"strings"

	"zb.256lights.llc/pkg/internal/detect"
	"zb.256lights.llc/pkg/sets"
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
	// Offsets is the set of byte offsets in the NAR serialization where self-reference digests occur.
	Offsets []int64
	// Paths is the set of paths in the NAR serialization that contain self-reference digests.
	Paths sets.Sorted[string]
}

// HasSelfReferences reports whether the analysis is non-empty.
func (analysis *SelfReferenceAnalysis) HasSelfReferences() bool {
	return analysis != nil && (len(analysis.Offsets) > 0 || analysis.Paths.Len() > 0)
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
	hmr := new(detect.HashModuloReader)
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
			analysis.Paths.Add(hdr.Path)
			for i := range indexSeq(hdr.LinkTarget, digest) {
				analysis.Offsets = append(analysis.Offsets, hdr.ContentOffset+int64(i))
			}
			hdr.LinkTarget = strings.ReplaceAll(hdr.LinkTarget, digest, digestReplacement)
		}
		if err := nw.WriteHeader(hdr); err != nil {
			return nix.ContentAddress{}, analysis, err
		}

		if !hdr.Mode.IsRegular() {
			continue
		}
		hmr.Reset(digest, digestReplacement, nr)
		_, err = io.Copy(nw, hmr)
		if offsets := hmr.Offsets(); len(offsets) > 0 {
			analysis.Paths.Add(hdr.Path)
			for _, off := range offsets {
				analysis.Offsets = append(analysis.Offsets, hdr.ContentOffset+off)
			}
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

	for _, off := range analysis.Offsets {
		fmt.Fprintf(h, "|%d", off)
	}
	return nix.RecursiveFileContentAddress(h.SumHash()), analysis, nil
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
