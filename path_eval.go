// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package zb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
	"zombiezen.com/go/zb/internal/jsonrpc"
	"zombiezen.com/go/zb/internal/lua"
	"zombiezen.com/go/zb/zbstore"
)

func (eval *Eval) pathFunction(l *lua.State) (nResults int, err error) {
	ctx := context.TODO()

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

	p, err = absSourcePath(l, p)
	if err != nil {
		return 0, fmt.Errorf("path: %v", err)
	}
	if name == "" {
		name = filepath.Base(p)
	}

	if err := walkPath(eval.cache, p); err != nil {
		return 0, fmt.Errorf("path: %v", err)
	}
	defer func() {
		sqlitex.ExecuteScriptFS(eval.cache, sqlFiles(), "walk/drop.sql", nil)
		// TODO(soon): Log error.
	}()

	// If we already imported and it exists in the store, don't do an import.
	if prevStorePath, err := eval.checkStamp(p, name); err != nil {
		log.Debugf(ctx, "%v", err)
	} else {
		var exists bool
		err := jsonrpc.Do(ctx, eval.store, zbstore.ExistsMethod, &exists, &zbstore.ExistsRequest{
			Path: string(prevStorePath),
		})
		if err != nil {
			log.Debugf(ctx, "Unable to query store path %s: %v", prevStorePath, err)
		} else if exists {
			log.Debugf(ctx, "Using existing store path %s", prevStorePath)
			l.PushStringContext(string(prevStorePath), []string{string(prevStorePath)})
			return 1, nil
		}
	}

	exporter, closeExport, err := startExport(context.TODO(), eval.store)
	if err != nil {
		return 0, fmt.Errorf("path: %v", err)
	}
	defer closeExport(false)

	pr, pw := io.Pipe()
	caChan := make(chan nix.ContentAddress)
	go func() {
		defer close(caChan)
		ca, _ := zbstore.SourceSHA256ContentAddress("", pr)
		caChan <- ca
	}()

	w := nar.NewWriter(io.MultiWriter(pw, exporter))
	err = sqlitex.ExecuteTransientFS(eval.cache, sqlFiles(), "walk/iterate.sql", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			fpath := stmt.GetText("path")
			var subpath string
			if fpath != p {
				var ok bool
				subpath, ok = strings.CutPrefix(fpath, p+string(filepath.Separator))
				if !ok {
					return fmt.Errorf("can't make %s relative to %s", fpath, p)
				}
				subpath = filepath.ToSlash(subpath)
			}

			mode := fs.FileMode(stmt.GetInt64("mode"))

			switch mode.Type() {
			case fs.ModeDir:
				err := w.WriteHeader(&nar.Header{
					Path: subpath,
					Mode: fs.ModeDir | 0o777,
				})
				if err != nil {
					return err
				}
			case fs.ModeSymlink:
				err := w.WriteHeader(&nar.Header{
					Path:       subpath,
					Mode:       fs.ModeSymlink | 0o777,
					LinkTarget: stmt.GetText("link_target"),
				})
				if err != nil {
					return err
				}
			default:
				size := stmt.GetInt64("size")
				err := w.WriteHeader(&nar.Header{
					Path: subpath,
					Mode: mode.Perm(),
					Size: size,
				})
				if err != nil {
					return err
				}
				f, err := os.Open(fpath)
				if err != nil {
					return err
				}
				defer f.Close()

				n, err := io.Copy(w, f)
				if err != nil {
					return err
				}
				if n != size {
					return fmt.Errorf("%s changed size during import", fpath)
				}
			}

			return nil
		},
	})
	if err != nil {
		pw.CloseWithError(err)
		<-caChan
		return 0, fmt.Errorf("path: %v", err)
	}
	if err := w.Close(); err != nil {
		pw.CloseWithError(err)
		<-caChan
		return 0, fmt.Errorf("path: %v", err)
	}

	pw.Close()
	ca := <-caChan

	storePath, err := zbstore.FixedCAOutputPath(eval.storeDir, name, ca, zbstore.References{})
	if err != nil {
		return 0, fmt.Errorf("path: %v", err)
	}
	err = exporter.Trailer(&zbstore.ExportTrailer{
		StorePath:      storePath,
		ContentAddress: ca,
	})
	if err != nil {
		return 0, fmt.Errorf("path: %v", err)
	}
	if err := closeExport(true); err != nil {
		return 0, fmt.Errorf("path: %v", err)
	}

	err = func() (err error) {
		endFn, err := sqlitex.ImmediateTransaction(eval.cache)
		if err != nil {
			return err
		}
		defer endFn(&err)
		return updateCache(eval.cache, storePath)
	}()
	if err != nil {
		return 0, fmt.Errorf("path: updating cache: %v", err)
	}

	l.PushStringContext(string(storePath), []string{string(storePath)})
	return 1, nil
}

func (eval *Eval) toFileFunction(l *lua.State) (int, error) {
	ctx := context.TODO()

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
	var refs zbstore.References
	for _, dep := range l.StringContext(2) {
		if strings.HasPrefix(dep, "!") {
			return 0, fmt.Errorf("toFile %q: cannot depend on derivation outputs", name)
		}
		refs.Others.Add(zbstore.Path(dep))
	}

	ca := nix.TextContentAddress(h.SumHash())
	storePath, err := zbstore.FixedCAOutputPath(eval.storeDir, name, ca, refs)
	if err != nil {
		return 0, fmt.Errorf("toFile %q: %v", name, err)
	}

	var exists bool
	err = jsonrpc.Do(ctx, eval.store, zbstore.ExistsMethod, &exists, &zbstore.ExistsRequest{
		Path: string(storePath),
	})
	if err != nil {
		log.Debugf(ctx, "Unable to query store path %s: %v", storePath, err)
	} else if exists {
		// Already exists: no need to re-import.
		log.Debugf(context.TODO(), "Using existing store path %s", storePath)
		l.PushStringContext(string(storePath), []string{string(storePath)})
		return 1, nil
	}

	exporter, closeExport, err := startExport(context.TODO(), eval.store)
	if err != nil {
		return 0, fmt.Errorf("toFile %q: %v", name, err)
	}
	defer closeExport(false)
	err = writeSingleFileNAR(exporter, strings.NewReader(s), int64(len(s)))
	if err != nil {
		return 0, fmt.Errorf("toFile %q: %v", name, err)
	}
	err = exporter.Trailer(&zbstore.ExportTrailer{
		StorePath:      storePath,
		References:     refs.Others,
		ContentAddress: ca,
	})
	if err != nil {
		return 0, fmt.Errorf("toFile %q: %v", name, err)
	}
	if err := closeExport(true); err != nil {
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

// checkStamp returns the store path of a previous import,
// if the cache still matches the metadata of the files on disk.
// path must be a cleaned, absolute path.
// name is the intended name of the store object.
// [Eval.walkPath] must be called before calling checkStamp.
func (eval *Eval) checkStamp(path, name string) (_ zbstore.Path, err error) {
	var found zbstore.Path
	err = sqlitex.ExecuteTransientFS(eval.cache, sqlFiles(), "find.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":name": name,
		},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			p, err := zbstore.ParsePath(stmt.GetText("path"))
			if err != nil || p.Dir() != eval.storeDir {
				// Skip.
				return nil
			}
			if found != "" {
				return fmt.Errorf("multiple store paths found for %s (hash collision): %s and %s", path, found, p)
			}
			found = p
			return nil
		},
	})
	if err != nil {
		return "", fmt.Errorf("check stamp for %s: find match: %v", path, err)
	}
	if found == "" {
		return "", fmt.Errorf("check stamp for %s: no match", path)
	}
	return found, nil
}

// walkPath creates a temporary table on the connection called "curr"
// and inserts the paths and their stamps into the table.
// walkPath only operates on the TEMP schema.
func walkPath(conn *sqlite.Conn, path string) (err error) {
	ctx := context.TODO()
	defer func() {
		if err != nil {
			err = fmt.Errorf("walk %s: %v", path, err)
		}
	}()

	rootInfo, err := os.Lstat(path)
	if err != nil {
		return err
	}

	defer sqlitex.Save(conn)(&err)

	err = sqlitex.ExecuteScriptFS(conn, sqlFiles(), "walk/create.sql", nil)
	if err != nil {
		return fmt.Errorf("create temp table: %v", err)
	}
	insertStmt, err := sqlitex.PrepareTransientFS(conn, sqlFiles(), "walk/insert.sql")
	if err != nil {
		return err
	}
	defer insertStmt.Finalize()

	if rootInfo.Mode().Type() == os.ModeSymlink {
		// If the root is a symlink, we don't want to walk it:
		// we want to use it directly.
		rootStamp, err := stamp(path, rootInfo)
		if err != nil {
			return err
		}
		insertStmt.SetText(":path", path)
		insertStmt.SetInt64(":mode", int64(rootInfo.Mode()))
		insertStmt.SetInt64(":size", -1)
		insertStmt.SetText(":stamp", rootStamp)
		log.Debugf(ctx, "walk %s stamp=%s", path, rootStamp)
		if _, err := insertStmt.Step(); err != nil {
			return err
		}
	} else {
		err = filepath.WalkDir(path, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			entryStamp, err := stamp(path, info)
			if err != nil {
				return err
			}

			insertStmt.SetText(":path", path)
			insertStmt.SetInt64(":mode", int64(info.Mode()))
			if info.Mode().IsRegular() {
				insertStmt.SetInt64(":size", info.Size())
			} else {
				insertStmt.SetInt64(":size", -1)
			}
			insertStmt.SetText(":stamp", entryStamp)
			log.Debugf(ctx, "walk %s stamp=%s", path, entryStamp)
			_, err = insertStmt.Step()
			insertStmt.ClearBindings()
			insertStmt.Reset()
			if err != nil {
				return err
			}

			return nil
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func updateCache(conn *sqlite.Conn, storePath zbstore.Path) (err error) {
	defer sqlitex.Save(conn)(&err)

	err = sqlitex.ExecuteScriptFS(conn, sqlFiles(), "invalidate.sql", nil)
	if err != nil {
		return fmt.Errorf("update cache for %s: %v", storePath, err)
	}

	err = sqlitex.ExecuteTransientFS(conn, sqlFiles(), "upsert_store_object.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":path": storePath,
		},
	})
	if err != nil {
		return fmt.Errorf("update cache for %s: %v", storePath, err)
	}

	var mappingID int64
	err = sqlitex.ExecuteTransientFS(conn, sqlFiles(), "new_mapping.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":path": storePath,
		},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			mappingID = stmt.GetInt64("mapping_id")
			return nil
		},
	})
	if err != nil {
		return fmt.Errorf("update cache for %s: %v", storePath, err)
	}

	err = sqlitex.ExecuteScriptFS(conn, sqlFiles(), "copy_walk.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":mapping_id": mappingID,
		},
	})
	if err != nil {
		return fmt.Errorf("update cache for %s: %v", storePath, err)
	}

	return nil
}

// collatePath compares two operating-system-native path strings,
// returning -1 if a < b,
// returning 1 if a > b,
// or returning 0 if a == b.
func collatePath(a, b string) int {
	a = filepath.Clean(a)
	b = filepath.Clean(b)

	for i := 0; i < len(a) && i < len(b); i++ {
		switch aSep, bSep := os.IsPathSeparator(a[i]), os.IsPathSeparator(b[i]); {
		case aSep && !bSep:
			return -1
		case !aSep && bSep:
			return 1
		case !aSep && !bSep && a[i] != b[i]:
			if a[i] < b[i] {
				return -1
			} else {
				return 1
			}
		}
	}

	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	default:
		return 0
	}
}

func startExport(ctx context.Context, store *jsonrpc.Client) (exporter *zbstore.Exporter, closeFunc func(ok bool) error, err error) {
	conn, releaseConn, err := storeCodec(ctx, store)
	if err != nil {
		return nil, nil, fmt.Errorf("export to store: %v", err)
	}
	pr, pw := io.Pipe()
	done := make(chan error)
	go func() {
		err := conn.Export(pr)
		pr.Close()
		done <- err
		close(done)
	}()

	exporter = zbstore.NewExporter(pw)
	var once sync.Once
	closeFunc = func(ok bool) error {
		var errs [3]error
		errs[0] = errors.New("already closed")

		once.Do(func() {
			if ok {
				errs[0] = exporter.Close()
				if errs[0] != nil {
					errs[1] = pw.CloseWithError(errs[0])
				} else {
					errs[1] = pw.Close()
				}
			} else {
				errs[0] = pw.CloseWithError(errors.New("export interrupted"))
			}
			errs[2] = <-done
			releaseConn()
		})

		for _, err := range errs {
			if err != nil {
				return err
			}
		}
		return nil
	}
	return exporter, closeFunc, nil
}

func storeCodec(ctx context.Context, client *jsonrpc.Client) (codec *zbstore.ClientCodec, release func(), err error) {
	generic, release, err := client.Codec(ctx)
	if err != nil {
		return nil, nil, err
	}
	codec, ok := generic.(*zbstore.ClientCodec)
	if !ok {
		release()
		return nil, nil, fmt.Errorf("store connection is %T (want %T)", generic, (*zbstore.ClientCodec)(nil))
	}
	return codec, release, nil
}

func stamp(path string, info fs.FileInfo) (string, error) {
	if info.Mode().Type() == os.ModeSymlink {
		target, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		return "link:" + target, nil
	}
	return stampFileInfo(info), nil
}

func stampFileInfo(info fs.FileInfo) string {
	if info.IsDir() {
		// Directories change too much; detect only existence.
		return "dir"
	}
	modTime := info.ModTime()
	uid, gid := owner(info)
	return fmt.Sprintf("%d.%06d-%d-%d-%d-%d-%d",
		modTime.Unix(),
		modTime.UTC().Nanosecond()/1000,
		info.Size(),
		inode(info),
		info.Mode(),
		uid,
		gid,
	)
}
