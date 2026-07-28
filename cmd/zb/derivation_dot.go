// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"slices"

	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/nix/nar"
)

type derivationLoader func(context.Context, []zbstore.Path) (map[zbstore.Path]*zbstore.Derivation, error)

func collectDerivationClosure(ctx context.Context, roots map[string]*zbstore.Derivation, load derivationLoader) (map[string]*zbstore.Derivation, error) {
	closure := maps.Clone(roots)
	for {
		missingSet := make(map[zbstore.Path]struct{})
		for _, drv := range closure {
			for inputPath := range drv.InputDerivations {
				if _, ok := closure[string(inputPath)]; !ok {
					missingSet[inputPath] = struct{}{}
				}
			}
		}
		if len(missingSet) == 0 {
			return closure, nil
		}

		missing := slices.Collect(maps.Keys(missingSet))
		slices.Sort(missing)
		loaded, err := load(ctx, missing)
		if err != nil {
			return nil, err
		}
		for _, path := range missing {
			drv := loaded[path]
			if drv == nil {
				return nil, fmt.Errorf("load derivation %s: %w", path, fs.ErrNotExist)
			}
			closure[string(path)] = drv
		}
	}
}

func loadLocalDerivations(_ context.Context, paths []zbstore.Path) (map[zbstore.Path]*zbstore.Derivation, error) {
	result := make(map[zbstore.Path]*zbstore.Derivation, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(string(path))
		if err != nil {
			return nil, err
		}
		name, isDerivation := path.DerivationName()
		if !isDerivation {
			return nil, fmt.Errorf("%s is not a derivation", path)
		}
		drv, err := zbstore.ParseDerivation(path.Dir(), name, data)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %v", path, err)
		}
		result[path] = drv
	}
	return result, nil
}

func loadLocalOrStoreDerivations(ctx context.Context, storeClient jsonrpc.Handler, importer *zbstorerpc.DeferredImporter, paths []zbstore.Path) (map[zbstore.Path]*zbstore.Derivation, error) {
	result := make(map[zbstore.Path]*zbstore.Derivation, len(paths))
	var remotePaths []zbstore.Path
	for _, path := range paths {
		data, err := os.ReadFile(string(path))
		if errors.Is(err, fs.ErrNotExist) {
			remotePaths = append(remotePaths, path)
			continue
		}
		if err != nil {
			return nil, err
		}
		name, isDerivation := path.DerivationName()
		if !isDerivation {
			return nil, fmt.Errorf("%s is not a derivation", path)
		}
		drv, err := zbstore.ParseDerivation(path.Dir(), name, data)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %v", path, err)
		}
		result[path] = drv
	}
	if len(remotePaths) == 0 {
		return result, nil
	}

	receiver := &derivationExportReceiver{
		derivations: make(map[zbstore.Path]*zbstore.Derivation, len(remotePaths)),
	}
	importer.SetImporter(zbstorerpc.NewReceiverImporter(receiver))
	err := jsonrpc.Do(ctx, storeClient, zbstorerpc.ExportMethod, nil, &zbstorerpc.ExportRequest{
		Paths:             remotePaths,
		ExcludeReferences: true,
	})
	if err != nil {
		return nil, err
	}
	if receiver.err != nil {
		return nil, receiver.err
	}
	maps.Copy(result, receiver.derivations)
	return result, nil
}

type derivationExportReceiver struct {
	buf         bytes.Buffer
	derivations map[zbstore.Path]*zbstore.Derivation
	err         error
}

func (r *derivationExportReceiver) Write(p []byte) (int, error) {
	if r.err != nil {
		return len(p), nil
	}
	return r.buf.Write(p)
}

func (r *derivationExportReceiver) ReceiveNAR(trailer *zbstore.ExportTrailer) {
	defer r.buf.Reset()
	if r.err != nil {
		return
	}

	name, isDerivation := trailer.StorePath.DerivationName()
	if !isDerivation {
		r.err = fmt.Errorf("%s is not a derivation", trailer.StorePath)
		return
	}
	nr := nar.NewReader(bytes.NewReader(r.buf.Bytes()))
	header, err := nr.Next()
	if err != nil {
		r.err = fmt.Errorf("read %s: %v", trailer.StorePath, err)
		return
	}
	if !header.Mode.IsRegular() {
		r.err = fmt.Errorf("read %s: not a regular file", trailer.StorePath)
		return
	}
	data, err := io.ReadAll(nr)
	if err != nil {
		r.err = fmt.Errorf("read %s: %v", trailer.StorePath, err)
		return
	}
	if _, err := nr.Next(); err != io.EOF {
		if err == nil {
			r.err = fmt.Errorf("read %s: more than one file", trailer.StorePath)
		} else {
			r.err = fmt.Errorf("read %s: %v", trailer.StorePath, err)
		}
		return
	}
	drv, err := zbstore.ParseDerivation(trailer.StorePath.Dir(), name, data)
	if err != nil {
		r.err = fmt.Errorf("parse %s: %v", trailer.StorePath, err)
		return
	}
	r.derivations[trailer.StorePath] = drv
}

func marshalDerivationDOT(derivations map[string]*zbstore.Derivation) []byte {
	paths := slices.Collect(maps.Keys(derivations))
	slices.Sort(paths)

	var buf bytes.Buffer
	buf.WriteString("digraph {\n")
	buf.WriteString("\tgraph [ranksep=2.0];\n")
	for _, path := range paths {
		buf.WriteByte('\t')
		appendDOTQuoted(&buf, path)
		buf.WriteString(" [label=")
		appendDOTQuoted(&buf, derivations[path].Name)
		buf.WriteString(", shape=box];\n")
	}
	for _, path := range paths {
		inputs := slices.Collect(maps.Keys(derivations[path].InputDerivations))
		slices.Sort(inputs)
		for _, inputPath := range inputs {
			buf.WriteByte('\t')
			appendDOTQuoted(&buf, path)
			buf.WriteString(" -> ")
			appendDOTQuoted(&buf, string(inputPath))
			buf.WriteString(";\n")
		}
	}
	buf.WriteString("}\n")
	return buf.Bytes()
}

func appendDOTQuoted(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			buf.WriteString(`\\`)
		case '"':
			buf.WriteString(`\"`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			fmt.Fprint(buf, string(r))
		}
	}
	buf.WriteByte('"')
}
