// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

// Package storetest provides utilities for interacting with the zb store in tests.
package storetest

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"

	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
	"zombiezen.com/go/zb/sortedset"
	"zombiezen.com/go/zb/zbstore"
)

// ExportFlatFile writes a fixed-hash flat file to the exporter with the given content.
func ExportFlatFile(exp *zbstore.Exporter, dir zbstore.Directory, name string, data []byte, ht nix.HashType) (zbstore.Path, error) {
	h := nix.NewHasher(ht)
	h.Write(data)
	ca := nix.FlatFileContentAddress(h.SumHash())
	p, err := exportFile(exp, dir, name, data, ca, nil)
	if err != nil {
		if p == "" {
			return "", err
		}
		return p, fmt.Errorf("export flat file %s: %v", p, err)
	}
	return p, nil
}

// ExportText writes a text file (e.g. a ".drv" file)
// to the exporter with the given content.
func ExportText(exp *zbstore.Exporter, dir zbstore.Directory, name string, data []byte, refs *sortedset.Set[zbstore.Path]) (zbstore.Path, error) {
	h := nix.NewHasher(nix.SHA256)
	h.Write(data)
	ca := nix.TextContentAddress(h.SumHash())
	trimmedRefs := trimRefs(data, zbstore.References{
		Others: *refs.Clone(),
	})
	p, err := exportFile(exp, dir, name, data, ca, &trimmedRefs.Others)
	if err != nil {
		if p == "" {
			return "", err
		}
		return p, fmt.Errorf("export text %s: %v", p, err)
	}
	return p, nil
}

// ExportDerivation writes a ".drv" file to the exporter.
func ExportDerivation(exp *zbstore.Exporter, drv *zbstore.Derivation) (zbstore.Path, error) {
	name := drv.Name + zbstore.DerivationExt
	data, err := drv.MarshalText()
	if err != nil {
		return "", fmt.Errorf("export derivation %s: %v", name, err)
	}
	h := nix.NewHasher(nix.SHA256)
	h.Write(data)
	ca := nix.TextContentAddress(h.SumHash())
	refs := drv.References().ToSet("")
	p, err := exportFile(exp, drv.Dir, name, data, ca, refs)
	if err != nil {
		if p == "" {
			return "", fmt.Errorf("export derivation %s: %v", name, err)
		}
		return p, fmt.Errorf("export derivation %s: %v", p, err)
	}
	return p, nil
}

func exportFile(exp *zbstore.Exporter, dir zbstore.Directory, name string, data []byte, ca zbstore.ContentAddress, refs *sortedset.Set[zbstore.Path]) (zbstore.Path, error) {
	refsClone := *refs.Clone()
	p, err := zbstore.FixedCAOutputPath(dir, name, ca, zbstore.References{Others: refsClone})
	if err != nil {
		return "", err
	}
	if err := SingleFileNAR(exp, data); err != nil {
		return p, err
	}
	err = exp.Trailer(&zbstore.ExportTrailer{
		StorePath:      p,
		ContentAddress: ca,
		References:     refsClone,
	})
	if err != nil {
		return p, err
	}
	return p, nil
}

// ExportSourceFile writes a file with the given content to the exporter.
func ExportSourceFile(exp *zbstore.Exporter, dir zbstore.Directory, tempDigest, name string, data []byte, refs zbstore.References) (zbstore.Path, error) {
	narBuffer := new(bytes.Buffer)
	if err := SingleFileNAR(narBuffer, data); err != nil {
		return "", err
	}
	return exportSource(exp, dir, tempDigest, name, narBuffer.Bytes(), refs)
}

// ExportSourceDir writes the given filesystem to the exporter.
func ExportSourceDir(exp *zbstore.Exporter, dir zbstore.Directory, tempDigest, name string, fsys fs.FS, refs zbstore.References) (zbstore.Path, error) {
	narBuffer := new(bytes.Buffer)
	if err := new(nar.Dumper).Dump(narBuffer, fsys, "."); err != nil {
		return "", err
	}
	return exportSource(exp, dir, tempDigest, name, narBuffer.Bytes(), refs)
}

func exportSource(exp *zbstore.Exporter, dir zbstore.Directory, tempDigest, name string, narBytes []byte, refs zbstore.References) (zbstore.Path, error) {
	if !refs.Self {
		tempDigest = ""
	}
	refs = trimRefs(narBytes, refs)

	ca, offsets, err := zbstore.SourceSHA256ContentAddress(tempDigest, bytes.NewReader(narBytes))
	if err != nil {
		return "", err
	}
	p, err := zbstore.FixedCAOutputPath(dir, name, ca, refs)
	if err != nil {
		return "", err
	}

	// Rewrite NAR in-place.
	newDigest := p.Digest()
	if tempDigest != "" && len(tempDigest) != len(newDigest) {
		return p, fmt.Errorf("export source %s: temporary digest %q is wrong size (expected %d)", p, tempDigest, len(newDigest))
	}
	for _, off := range offsets {
		copy(narBytes[off:int(off)+len(newDigest)], newDigest)
	}

	if _, err := exp.Write(narBytes); err != nil {
		return p, fmt.Errorf("export source %s: %v", p, err)
	}
	err = exp.Trailer(&zbstore.ExportTrailer{
		StorePath:      p,
		ContentAddress: ca,
		References:     *refs.ToSet(p),
	})
	if err != nil {
		return p, fmt.Errorf("export source %s: %v", p, err)
	}
	return p, nil
}

// SingleFileNAR writes a single non-executable file NAR to the given writer
// with the given file contents.
func SingleFileNAR(w io.Writer, data []byte) error {
	nw := nar.NewWriter(w)
	if err := nw.WriteHeader(&nar.Header{Size: int64(len(data))}); err != nil {
		return err
	}
	if _, err := nw.Write(data); err != nil {
		return err
	}
	if err := nw.Close(); err != nil {
		return err
	}
	return nil
}

func trimRefs(data []byte, refs zbstore.References) zbstore.References {
	firstMissing := -1
	for i, ref := range refs.Others.All() {
		if !bytes.Contains(data, []byte(ref.Digest())) {
			firstMissing = i
			break
		}
	}
	if firstMissing == -1 {
		return refs
	}

	newRefs := zbstore.References{
		Self: refs.Self,
	}
	newRefs.Others.Grow(refs.Others.Len() - 1)
	for i, ref := range refs.Others.All() {
		if i == firstMissing {
			continue
		}
		if i < firstMissing || bytes.Contains(data, []byte(ref.Digest())) {
			newRefs.Others.Add(ref)
		}
	}
	return newRefs
}
