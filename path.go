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

	if !filepath.IsAbs(p) {
		// TODO(maybe): This is probably wonky with tail calls.
		debugInfo := l.Stack(1).Info("S")
		if debugInfo == nil {
			return 0, fmt.Errorf("path: no caller information available")
		}
		if source, ok := strings.CutPrefix(debugInfo.Source, "@"); ok {
			p = filepath.Join(filepath.Dir(source), filepath.FromSlash(p))
		} else {
			var err error
			p, err = filepath.Abs(filepath.FromSlash(p))
			if err != nil {
				return 0, fmt.Errorf("path: %w", err)
			}
		}
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
