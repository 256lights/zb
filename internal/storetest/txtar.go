// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package storetest

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"maps"
	"strings"

	"golang.org/x/tools/txtar"
	"zb.256lights.llc/pkg/internal/aterm"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
)

// TxtarObjects converts the source files and .drv files in a txtar archive to a slice of [*Object],
// rewriting their paths to the named directory.
// TxtarObjects also returns a map of file names to store paths.
func TxtarObjects(dir zbstore.Directory, files []txtar.File) ([]*Object, map[string]zbstore.Path, error) {
	objects := make([]*Object, 0, len(files))
	rewrites := make(map[string]zbstore.Path)

	for objectFiles := range groupFilesByObject(files) {
		objectName, _, hasSubpath := strings.Cut(objectFiles[0].Name, "/")
		fakePath, err := dir.Object(objectName)
		if err != nil {
			return objects, rewrites, err
		}

		// Special case: derivations.
		if !hasSubpath && strings.HasSuffix(objectName, zbstore.DerivationExt) {
			data, refs, err := rewriteTxtarDerivation(dir, objectFiles[0], maps.All(rewrites))
			if err != nil {
				return objects, rewrites, err
			}
			obj := &Object{
				ExportTrailer: zbstore.ExportTrailer{
					References:     *refs,
					ContentAddress: nix.TextContentAddress(nix.NewHash(nix.SHA256, new(sha256.Sum256(data))[:])),
				},
			}
			obj.StorePath, err = zbstore.FixedCAOutputPath(dir, fakePath.Name(), obj.ContentAddress, zbstore.References{
				Others: *refs,
			})
			if err != nil {
				return objects, rewrites, err
			}
			buf := new(bytes.Buffer)
			if err := SingleFileNAR(buf, data); err != nil {
				return objects, rewrites, fmt.Errorf("%s: %v", objectFiles[0].Name, err)
			}
			obj.NAR = buf.Bytes()
			objects = append(objects, obj)
			rewrites[objectName] = obj.StorePath
			continue
		}

		buf := new(bytes.Buffer)
		nw := nar.NewWriter(buf)
		for _, file := range objectFiles {
			if err := copyTxtarToNAR(nw, file); err != nil {
				return objects, rewrites, err
			}
		}
		if err := nw.Close(); err != nil {
			return objects, rewrites, fmt.Errorf("%s: %v", objectFiles[0].Name, err)
		}
		// TODO(someday): References.
		obj := &Object{NAR: buf.Bytes()}
		obj.ContentAddress, _, err = zbstore.SourceSHA256ContentAddress(bytes.NewReader(buf.Bytes()), nil)
		if err != nil {
			return objects, rewrites, fmt.Errorf("%s: %v", objectFiles[0].Name, err)
		}
		obj.StorePath, err = zbstore.FixedCAOutputPath(dir, fakePath.Name(), obj.ContentAddress, zbstore.References{})
		if err != nil {
			return objects, rewrites, fmt.Errorf("%s: %v", objectFiles[0].Name, err)
		}
		objects = append(objects, obj)
		rewrites[objectName] = obj.StorePath
	}
	return objects, rewrites, nil
}

func groupFilesByObject(files []txtar.File) iter.Seq[[]txtar.File] {
	return func(yield func([]txtar.File) bool) {
		for i := 0; i < len(files); {
			firstFileIndex := i
			objectName, _, hasSubpath := strings.Cut(files[firstFileIndex].Name, "/")
			i++
			if hasSubpath {
				prefix := files[firstFileIndex].Name[:len(objectName)+len("/")]
				for i < len(files) && strings.HasPrefix(files[i].Name, prefix) {
					i++
				}
			}
			if !yield(files[firstFileIndex:i]) {
				return
			}
		}
	}
}

func copyTxtarToNAR(nw *nar.Writer, file txtar.File) error {
	_, subpath, _ := strings.Cut(file.Name, "/")
	h := &nar.Header{Path: subpath}
	data := bytes.ReplaceAll(file.Data, []byte("\r"), nil) // Convert CRLF to LF.
	isDir := strings.HasSuffix(file.Name, "/")
	if isDir {
		h.Mode = fs.ModeDir
	} else {
		h.Size = int64(len(data))
	}
	if err := nw.WriteHeader(h); err != nil {
		return fmt.Errorf("serialize %s to nar: %v", file.Name, err)
	}
	if !isDir {
		if _, err := nw.Write(data); err != nil {
			return fmt.Errorf("serialize %s to nar: %v", file.Name, err)
		}
	}
	return nil
}

func rewriteTxtarDerivation(dir zbstore.Directory, file txtar.File, rewrites iter.Seq2[string, zbstore.Path]) ([]byte, *sets.Sorted[zbstore.Path], error) {
	drvName, isDrv := strings.CutSuffix(file.Name, zbstore.DerivationExt)
	if !isDrv {
		return file.Data, nil, fmt.Errorf("%s: not a %s file", file.Name, zbstore.DerivationExt)
	}
	data, err := MinimizeDerivation(file.Data)
	if err != nil {
		return file.Data, nil, fmt.Errorf("%s: %v", file.Name, err)
	}
	drv := &zbstore.Derivation{Name: drvName}
	if err := drv.UnmarshalText(data); err != nil {
		return file.Data, nil, fmt.Errorf("%s: %v", file.Name, err)
	}
	oldRefs := drv.References().ToSet("")

	if drv.Dir != "" {
		var replacements []string
		for oldBase, newPath := range rewrites {
			oldPath, err := drv.Dir.Object(oldBase)
			if err != nil {
				return file.Data, oldRefs, fmt.Errorf("%s: cannot replace %q: %v", file.Name, oldBase, err)
			}
			replacements = append(replacements, string(oldPath), string(newPath))
			if drv.InputSources.Has(oldPath) {
				drv.InputSources.Delete(oldPath)
				drv.InputSources.Add(newPath)
			}
			for outputName := range drv.InputDerivations[oldPath].Values() {
				oldPlaceholder := zbstore.UnknownCAOutputPlaceholder(zbstore.OutputReference{
					DrvPath:    oldPath,
					OutputName: outputName,
				})
				newPlaceholder := zbstore.UnknownCAOutputPlaceholder(zbstore.OutputReference{
					DrvPath:    newPath,
					OutputName: outputName,
				})
				replacements = append(replacements, oldPlaceholder, newPlaceholder)
			}
			if outputNames, ok := drv.InputDerivations[oldPath]; ok {
				drv.InputDerivations[newPath] = outputNames
				delete(drv.InputDerivations, oldPath)
			}
		}
		drv = drv.ReplaceStrings(strings.NewReplacer(replacements...))
	}
	drv.Dir = dir

	rewritten, err := drv.MarshalText()
	if err != nil {
		return file.Data, oldRefs, fmt.Errorf("%s: %v", file.Name, err)
	}
	return rewritten, drv.References().ToSet(""), nil
}

// MinimizeDerivation removes all whitespace between tokens in the derivation data.
// If an error is encountered, then data is returned as-is along with the error.
func MinimizeDerivation(data []byte) ([]byte, error) {
	const prefix = "Derive"
	atermData, ok := bytes.CutPrefix(data, []byte(prefix))
	if !ok {
		return data, fmt.Errorf("minimize derivation: line 1: data does not start with %q", prefix)
	}
	noWhitespace := make([]byte, 0, len(data))
	noWhitespace = append(noWhitespace, prefix...)
	atermReader := bytes.NewReader(atermData)
	atermScanner := aterm.NewScanner(atermReader)
	atermScanner.AllowWhitespace()
	for first := true; ; {
		tok, err := atermScanner.ReadToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return data, fmt.Errorf("minimize derivation: line %d: %v", readerLineNumber(atermReader), err)
		}
		if !first && tok.Kind != aterm.RParen && tok.Kind != aterm.RBracket {
			noWhitespace = append(noWhitespace, ',')
		}
		first = tok.Kind == aterm.LParen || tok.Kind == aterm.LBracket
		noWhitespace, err = tok.AppendText(noWhitespace)
		if err != nil {
			return data, fmt.Errorf("minimize derivation: line %d: %v", readerLineNumber(atermReader), err)
		}
	}
	readEnd := len(data) - atermReader.Len()
	if !isBlank(data[readEnd:]) {
		return data, fmt.Errorf("minimize derivation: line %d: trailing data", readerLineNumber(atermReader))
	}
	return noWhitespace, nil
}

func readerLineNumber(r *bytes.Reader) int {
	pos, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		panic(err)
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		panic(err)
	}
	lineno := 1
	for range pos {
		b, err := r.ReadByte()
		if err != nil {
			panic(err)
		}
		if b == '\n' {
			lineno++
		}
	}
	return lineno
}

func isBlank(s []byte) bool {
	for _, b := range s {
		if b != ' ' && b != '\t' && b != '\n' && b != '\r' {
			return false
		}
	}
	return true
}
