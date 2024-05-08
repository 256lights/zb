package zb

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
	"zombiezen.com/go/zb/internal/lua"
)

func (eval *Eval) pathFunction(l *lua.State) (int, error) {
	var p string
	var name string
	switch l.Type(1) {
	case lua.TypeString:
		p, _ = l.ToString(1)
	case lua.TypeTable:
		typ, err := l.Field(1, "path", 0)
		if err != nil {
			return 0, fmt.Errorf("path: %v", err)
		}
		if typ == lua.TypeNil {
			return 0, lua.NewArgError(l, 1, "missing path")
		}
		p, err = lua.ToString(l, -1)
		if err != nil {
			return 0, fmt.Errorf("path: %v", err)
		}
		l.Pop(1)

		typ, err = l.Field(1, "name", 0)
		if err != nil {
			return 0, fmt.Errorf("path: %v", err)
		}
		if typ != lua.TypeNil {
			name, _ = lua.ToString(l, -1)
		}
		l.Pop(1)
	default:
		return 0, lua.NewTypeError(l, 1, "string or table")
	}

	p, err := absSourcePath(l, p)
	if err != nil {
		return 0, fmt.Errorf("path: %v", err)
	}
	if name == "" {
		name = filepath.Base(p)
	}

	imp, err := startImport(context.TODO())
	if err != nil {
		return 0, fmt.Errorf("path: %w", err)
	}
	defer imp.Close()

	h := nix.NewHasher(nix.SHA256)
	if err := nar.DumpPath(io.MultiWriter(h, imp), p); err != nil {
		return 0, fmt.Errorf("path: %w", err)
	}
	sum := h.SumHash()
	storePath, err := fixedCAOutputPath(eval.storeDir, name, nix.RecursiveFileContentAddress(sum), storeReferences{})
	if err != nil {
		return 0, fmt.Errorf("path: %w", err)
	}
	err = imp.Trailer(&nixExportTrailer{
		storePath: storePath,
	})
	if err != nil {
		return 0, fmt.Errorf("path: %w", err)
	}
	if err := imp.Close(); err != nil {
		return 0, fmt.Errorf("path: %w", err)
	}
	l.PushStringContext(string(storePath), []string{string(storePath)})
	return 1, nil
}

func (eval *Eval) toFileFunction(l *lua.State) (int, error) {
	name, err := lua.CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	s, err := lua.CheckString(l, 2)
	if err != nil {
		return 0, err
	}

	h := nix.NewHasher(nix.SHA256)
	h.WriteString(s)
	var refs storeReferences
	for _, dep := range l.StringContext(2) {
		if strings.HasPrefix(dep, "!") {
			return 0, fmt.Errorf("toFile %q: cannot depend on derivation outputs", name)
		}
		refs.others.Add(nix.StorePath(dep))
	}

	storePath, err := fixedCAOutputPath(eval.storeDir, name, nix.TextContentAddress(h.SumHash()), refs)
	if err != nil {
		return 0, fmt.Errorf("toFile %q: %v", name, err)
	}

	imp, err := startImport(context.TODO())
	if err != nil {
		return 0, fmt.Errorf("toFile %q: %v", name, err)
	}
	defer imp.Close()
	err = writeSingleFileNAR(imp, strings.NewReader(s), int64(len(s)))
	if err != nil {
		return 0, fmt.Errorf("toFile %q: %v", name, err)
	}
	err = imp.Trailer(&nixExportTrailer{
		storePath:  storePath,
		references: refs.others,
	})
	if err != nil {
		return 0, fmt.Errorf("toFile %q: %v", name, err)
	}
	if err := imp.Close(); err != nil {
		return 0, fmt.Errorf("toFile %q: %v", name, err)
	}

	l.PushStringContext(string(storePath), []string{string(storePath)})
	return 1, nil
}

func writeSingleFileNAR(w io.Writer, r io.Reader, sz int64) error {
	nw := nar.NewWriter(w)
	if err := nw.WriteHeader(&nar.Header{Size: sz}); err != nil {
		return err
	}
	if _, err := io.Copy(nw, r); err != nil {
		return err
	}
	if err := nw.Close(); err != nil {
		return err
	}
	return nil
}

// absSourcePath takes a source path passed as an argument from Lua to Go
// and resolves it relative to the calling function.
func absSourcePath(l *lua.State, path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	// TODO(maybe): This is probably wonky with tail calls.
	debugInfo := l.Stack(1).Info("S")
	if debugInfo == nil {
		return "", fmt.Errorf("resolve path: no caller information available")
	}
	source, ok := strings.CutPrefix(debugInfo.Source, "@")
	if !ok {
		// Not loaded from a file. Use working directory.
		//
		// TODO(someday): This is intended for --expr evaluation,
		// but would take place for any chunk loaded with the "load" built-in.
		// Perhaps an allow-list of sources?
		path, err := filepath.Abs(filepath.FromSlash(path))
		if err != nil {
			return "", fmt.Errorf("resolve path: %w", err)
		}
		return path, nil
	}

	return filepath.Join(filepath.Dir(source), filepath.FromSlash(path)), nil
}
