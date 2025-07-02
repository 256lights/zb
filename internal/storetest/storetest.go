// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package storetest provides utilities for interacting with the zb store in tests.
package storetest

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"

	"zb.256lights.llc/pkg/bytebuffer"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
)

// ExportFlatFile writes a fixed-hash flat file to the exporter with the given content.
func ExportFlatFile(exp *zbstore.ExportWriter, dir zbstore.Directory, name string, data []byte, ht nix.HashType) (zbstore.Path, zbstore.ContentAddress, error) {
	h := nix.NewHasher(ht)
	h.Write(data)
	ca := nix.FlatFileContentAddress(h.SumHash())
	p, err := exportFile(exp, dir, name, data, ca, nil)
	if err != nil {
		if p == "" {
			return "", ca, err
		}
		return p, ca, fmt.Errorf("export flat file %s: %v", p, err)
	}
	return p, ca, nil
}

// ExportText writes a text file (e.g. a ".drv" file)
// to the exporter with the given content.
func ExportText(exp *zbstore.ExportWriter, dir zbstore.Directory, name string, data []byte, refs *sets.Sorted[zbstore.Path]) (zbstore.Path, zbstore.ContentAddress, error) {
	h := nix.NewHasher(nix.SHA256)
	h.Write(data)
	ca := nix.TextContentAddress(h.SumHash())
	trimmedRefs := trimRefs(data, zbstore.References{
		Others: *refs.Clone(),
	})
	p, err := exportFile(exp, dir, name, data, ca, &trimmedRefs.Others)
	if err != nil {
		if p == "" {
			return "", ca, err
		}
		return p, ca, fmt.Errorf("export text %s: %v", p, err)
	}
	return p, ca, nil
}

// ExportDerivation writes a ".drv" file to the exporter.
func ExportDerivation(exp *zbstore.ExportWriter, drv *zbstore.Derivation) (zbstore.Path, zbstore.ContentAddress, error) {
	name := drv.Name + zbstore.DerivationExt
	data, err := drv.MarshalText()
	if err != nil {
		return "", zbstore.ContentAddress{}, fmt.Errorf("export derivation %s: %v", name, err)
	}
	h := nix.NewHasher(nix.SHA256)
	h.Write(data)
	ca := nix.TextContentAddress(h.SumHash())
	refs := drv.References().ToSet("")
	p, err := exportFile(exp, drv.Dir, name, data, ca, refs)
	if err != nil {
		if p == "" {
			return "", ca, fmt.Errorf("export derivation %s: %v", name, err)
		}
		return p, ca, fmt.Errorf("export derivation %s: %v", p, err)
	}
	return p, ca, nil
}

func exportFile(exp *zbstore.ExportWriter, dir zbstore.Directory, name string, data []byte, ca zbstore.ContentAddress, refs *sets.Sorted[zbstore.Path]) (zbstore.Path, error) {
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

// SourceExportOptions is the set of common arguments to [ExportSourceFile], [ExportSourceDir], and [ExportSourceNAR].
type SourceExportOptions struct {
	Name       string
	Directory  zbstore.Directory
	References zbstore.References

	// TempDigest is the placeholder digest used in the source's content for self references.
	// TempDigest may be empty.
	TempDigest string
}

// ExportSourceFile writes a file with the given content to the exporter.
func ExportSourceFile(exp *zbstore.ExportWriter, data []byte, opts SourceExportOptions) (zbstore.Path, zbstore.ContentAddress, error) {
	narBuffer := new(bytes.Buffer)
	if err := SingleFileNAR(narBuffer, data); err != nil {
		return "", zbstore.ContentAddress{}, err
	}
	return ExportSourceNAR(exp, narBuffer.Bytes(), opts)
}

// ExportSourceDir writes the given filesystem to the exporter.
func ExportSourceDir(exp *zbstore.ExportWriter, fsys fs.FS, opts SourceExportOptions) (zbstore.Path, zbstore.ContentAddress, error) {
	narBuffer := new(bytes.Buffer)
	if err := new(nar.Dumper).Dump(narBuffer, fsys, "."); err != nil {
		return "", zbstore.ContentAddress{}, err
	}
	return ExportSourceNAR(exp, narBuffer.Bytes(), opts)
}

// ExportSourceNAR writes narBytes to the exporter.
func ExportSourceNAR(exp *zbstore.ExportWriter, narBytes []byte, opts SourceExportOptions) (zbstore.Path, zbstore.ContentAddress, error) {
	if !opts.References.Self {
		opts.TempDigest = ""
	}
	opts.References = trimRefs(narBytes, opts.References)

	ca, analysis, err := zbstore.SourceSHA256ContentAddress(bytes.NewReader(narBytes), &zbstore.ContentAddressOptions{
		Digest: opts.TempDigest,
	})
	if err != nil {
		return "", zbstore.ContentAddress{}, err
	}
	p, err := zbstore.FixedCAOutputPath(opts.Directory, opts.Name, ca, opts.References)
	if err != nil {
		return "", ca, err
	}

	// Rewrite NAR in-place.
	newDigest := p.Digest()
	if opts.TempDigest != "" && len(opts.TempDigest) != len(newDigest) {
		return p, ca, fmt.Errorf("export source %s: temporary digest %q is wrong size (expected %d)", p, opts.TempDigest, len(newDigest))
	}
	if err := zbstore.Rewrite(bytebuffer.New(narBytes), 0, newDigest, analysis.Rewrites); err != nil {
		return p, ca, fmt.Errorf("export source %s: %v", p, err)
	}

	if _, err := exp.Write(narBytes); err != nil {
		return p, ca, fmt.Errorf("export source %s: %v", p, err)
	}
	err = exp.Trailer(&zbstore.ExportTrailer{
		StorePath:      p,
		ContentAddress: ca,
		References:     *opts.References.ToSet(p),
	})
	if err != nil {
		return p, ca, fmt.Errorf("export source %s: %v", p, err)
	}
	return p, ca, nil
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
