// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package frontend

import (
	"context"
	"embed"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	slashpath "path"
	"path/filepath"
	"runtime/cgo"
	"strings"

	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/lua"
	"zb.256lights.llc/pkg/internal/xiter"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
)

//go:embed prelude.lua
var preludeSource string

type Eval struct {
	l         lua.State
	store     *jsonrpc.Client
	storeDir  zbstore.Directory
	cachePool *sqlitemigration.Pool
}

func NewEval(storeDir zbstore.Directory, store *jsonrpc.Client, cacheDB string) (_ *Eval, err error) {
	if err := os.MkdirAll(filepath.Dir(cacheDB), 0o777); err != nil {
		return nil, fmt.Errorf("zb: new eval: %v", err)
	}
	var schema sqlitemigration.Schema
	for i := 1; ; i++ {
		migration, err := fs.ReadFile(sqlFiles(), fmt.Sprintf("schema/%02d.sql", i))
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("zb new eval: read migrations: %v", err)
		}
		schema.Migrations = append(schema.Migrations, string(migration))
	}

	eval := &Eval{
		store:    store,
		storeDir: storeDir,
		cachePool: sqlitemigration.NewPool(cacheDB, schema, sqlitemigration.Options{
			Flags:       sqlite.OpenCreate | sqlite.OpenReadWrite,
			PoolSize:    1,
			PrepareConn: prepareCache,
		}),
	}
	defer func() {
		if err != nil {
			eval.l.Close()
			eval.cachePool.Close()
			err = fmt.Errorf("zb: new eval: %v", err)
		}
	}()

	registerDerivationMetatable(&eval.l)

	base := lua.NewOpenBase(io.Discard, nil)
	if err := lua.Require(&eval.l, lua.GName, true, base); err != nil {
		return nil, err
	}

	// Wrap load function.
	if tp := eval.l.RawField(-1, "load"); tp != lua.TypeFunction {
		return nil, fmt.Errorf("load is not a function")
	}
	eval.l.PushClosure(1, loadFunction)
	eval.l.RawSetField(-2, "load")

	// Clear loadfile and dofile for now.
	// (They get added back in [Eval.initScope].)
	eval.l.PushBoolean(false)
	eval.l.RawSetField(-2, "loadfile")
	eval.l.PushBoolean(false)
	eval.l.RawSetField(-2, "dofile")

	// Set other built-ins.
	err = lua.SetFuncs(&eval.l, 0, map[string]lua.Function{
		"baseNameOf": func(l *lua.State) (int, error) {
			path, err := lua.CheckString(l, 1)
			if err != nil {
				return 0, err
			}
			if path == "" {
				l.PushString("")
				return 1, nil
			}
			l.PushString(slashpath.Base(path))
			return 1, nil
		},
	})
	if err != nil {
		return nil, err
	}
	eval.l.PushString(string(storeDir))
	if err := eval.l.SetField(-2, "storeDir", 0); err != nil {
		return nil, err
	}

	// Pop base library.
	eval.l.Pop(1)

	// Load other standard libraries.
	if err := lua.Require(&eval.l, lua.TableLibraryName, true, lua.OpenTable); err != nil {
		return nil, err
	}

	// Run prelude.
	if err := eval.l.LoadString(preludeSource, "=(prelude)", "t"); err != nil {
		return nil, err
	}
	if err := eval.l.Call(0, 0, 0); err != nil {
		return nil, err
	}

	return eval, nil
}

func (eval *Eval) pushG() error {
	if !eval.l.CheckStack(2) {
		return fmt.Errorf("access %s.%s: stack overflow", lua.LoadedTable, lua.GName)
	}
	if tp := lua.Metatable(&eval.l, lua.LoadedTable); tp != lua.TypeTable {
		eval.l.Pop(1)
		return fmt.Errorf("%s is a %v instead of a table", lua.LoadedTable, tp)
	}
	if tp, err := eval.l.Field(-1, lua.GName, 0); err != nil {
		eval.l.Pop(2)
		return fmt.Errorf("field %s.%s: %v", lua.LoadedTable, lua.GName, err)
	} else if tp != lua.TypeTable {
		eval.l.Pop(2)
		return fmt.Errorf("%s.%s is a %v instead of a table", lua.LoadedTable, lua.GName, tp)
	}
	eval.l.Remove(-2)
	return nil
}

func (eval *Eval) initScope(ctx context.Context, cache *sqlite.Conn) (cleanup func(), err error) {
	if err := eval.pushG(); err != nil {
		return nil, err
	}
	fmap := map[string]lua.Function{
		"derivation": func(l *lua.State) (int, error) {
			return eval.derivationFunction(ctx, l)
		},
		"loadfile": func(l *lua.State) (int, error) {
			return eval.loadfileFunction(ctx, l)
		},
		"path": func(l *lua.State) (int, error) {
			return eval.pathFunction(ctx, cache, l)
		},
		"toFile": func(l *lua.State) (int, error) {
			return eval.toFileFunction(ctx, l)
		},
	}
	err = lua.SetFuncs(&eval.l, 0, fmap)
	if err != nil {
		eval.l.Pop(1)
		return nil, fmt.Errorf("add global functions: %v", err)
	}
	for k := range fmap {
		fmap[k] = nil
	}

	// dofile needs loadfile as its first upvalue.
	eval.l.RawField(-1, "loadfile")
	eval.l.PushClosure(1, dofileFunction)
	eval.l.RawSetField(-2, "dofile")
	fmap["dofile"] = nil

	return func() {
		if err := eval.pushG(); err != nil {
			return
		}
		lua.SetFuncs(&eval.l, 0, fmap)
		eval.l.Pop(1)
	}, nil
}

func prepareCache(conn *sqlite.Conn) error {
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA journal_mode=wal;", nil); err != nil {
		return fmt.Errorf("enable write-ahead logging: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA foreign_keys=on;", nil); err != nil {
		return fmt.Errorf("enable foreign keys: %v", err)
	}

	err := conn.CreateFunction("store_path_name", &sqlite.FunctionImpl{
		NArgs: 1,
		Scalar: func(ctx sqlite.Context, args []sqlite.Value) (sqlite.Value, error) {
			if args[0].Type() == sqlite.TypeNull {
				return sqlite.Value{}, nil
			}
			p, err := nix.ParseStorePath(args[0].Text())
			if err != nil {
				// A non-store path has no name. Return null.
				return sqlite.Value{}, nil
			}
			return sqlite.TextValue(p.Name()), nil
		},
		Deterministic: true,
		AllowIndirect: true,
	})
	if err != nil {
		return err
	}

	if err := conn.SetCollation("PATH", collatePath); err != nil {
		return err
	}

	return nil
}

func (eval *Eval) Close() error {
	var errors [2]error
	errors[0] = eval.l.Close()
	errors[1] = eval.cachePool.Close()
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

func (eval *Eval) File(ctx context.Context, exprFile string, attrPaths []string) ([]any, error) {
	cache, err := eval.cachePool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer eval.cachePool.Put(cache)

	defer eval.l.SetTop(0)
	cleanup, err := eval.initScope(ctx, cache)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if err := loadFile(&eval.l, exprFile); err != nil {
		return nil, err
	}
	if err := eval.l.Call(0, 1, 0); err != nil {
		eval.l.Pop(1)
		return nil, err
	}
	return eval.attrPaths(attrPaths)
}

func (eval *Eval) Expression(ctx context.Context, expr string, attrPaths []string) ([]any, error) {
	cache, err := eval.cachePool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer eval.cachePool.Put(cache)

	defer eval.l.SetTop(0)
	cleanup, err := eval.initScope(ctx, cache)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if err := loadExpression(&eval.l, expr); err != nil {
		return nil, err
	}
	if err := eval.l.Call(0, 1, 0); err != nil {
		eval.l.Pop(1)
		return nil, err
	}
	return eval.attrPaths(attrPaths)
}

// attrPaths evaluates all the attribute paths given
// against the value on the top of the stack.
func (eval *Eval) attrPaths(paths []string) ([]any, error) {
	if len(paths) == 0 {
		x, err := luaToGo(&eval.l)
		if err != nil {
			return nil, err
		}
		return []any{x}, nil
	}

	result := make([]any, 0, len(paths))
	for _, p := range paths {
		expr := "local x = ...; return x"
		if !strings.HasPrefix(p, "[") {
			expr += "."
		}
		expr += p + ";"
		if err := eval.l.LoadString(expr, expr, "t"); err != nil {
			eval.l.Pop(1)
			return result, fmt.Errorf("%s: %v", p, err)
		}
		eval.l.PushValue(-2)
		if err := eval.l.Call(1, 1, 0); err != nil {
			eval.l.Pop(1)
			return result, fmt.Errorf("%s: %v", p, err)
		}
		x, err := luaToGo(&eval.l)
		eval.l.Pop(1)
		if err != nil {
			return result, fmt.Errorf("%s: %v", p, err)
		}
		result = append(result, x)
	}
	return result, nil
}

func luaToGo(l *lua.State) (any, error) {
	switch typ := l.Type(-1); typ {
	case lua.TypeNil:
		return nil, nil
	case lua.TypeNumber:
		if l.IsInteger(-1) {
			i, _ := l.ToInteger(-1)
			return i, nil
		}
		n, _ := l.ToNumber(-1)
		return n, nil
	case lua.TypeBoolean:
		return l.IsBoolean(-1), nil
	case lua.TypeString:
		s, _ := l.ToString(-1)
		return s, nil
	case lua.TypeTable:
		// Try first for an array.
		var arr []any
		err := ipairs(l, -1, func(i int64) error {
			x, err := luaToGo(l)
			if err != nil {
				return fmt.Errorf("#%d: %v", i, err)
			}
			arr = append(arr, x)
			return nil
		})
		if err != nil {
			if len(arr) > 0 {
				return arr, err
			}
			return nil, err
		}
		if len(arr) > 0 {
			return arr, nil
		}

		// It's an object.
		m := make(map[string]any)
		l.PushNil()
		for l.Next(-2) {
			if l.Type(-2) != lua.TypeString {
				l.Pop(1)
				continue
			}

			k, _ := l.ToString(-2)
			v, err := luaToGo(l)
			if err != nil {
				l.Pop(2)
				return nil, fmt.Errorf("[%q]: %v", k, err)
			}
			l.Pop(1)
			m[k] = v
		}
		return m, nil
	default:
		drv := testDerivation(l, -1)
		if drv != nil {
			return drv, nil
		}
		return nil, fmt.Errorf("cannot convert %v to Go", typ)
	}
}

func testUserdataHandle(l *lua.State, idx int, tname string) (cgo.Handle, bool) {
	data := lua.TestUserdata(l, idx, tname)
	if len(data) != 8 {
		return 0, false
	}
	return cgo.Handle(binary.LittleEndian.Uint64(data)), true
}

func setUserdataHandle(l *lua.State, idx int, handle cgo.Handle) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(handle))
	l.SetUserdata(idx, 0, buf[:])
}

func loadFile(l *lua.State, path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("load file: %w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("load file: %w", err)
	}
	defer f.Close()

	if err := l.Load(f, "@"+path, "t"); err != nil {
		l.Pop(1)
		return fmt.Errorf("load file %s: %w", path, err)
	}
	return nil
}

func loadExpression(l *lua.State, expr string) error {
	if err := l.LoadString("return "+expr+";", expr, "t"); err == nil {
		return nil
	}
	l.Pop(1)
	if err := l.LoadString(expr, expr, "t"); err != nil {
		l.Pop(1)
		return err
	}
	return nil
}

func ipairs(l *lua.State, idx int, f func(i int64) error) error {
	idx = l.AbsIndex(idx)
	top := l.Top()
	defer l.SetTop(top)
	for i := int64(1); ; i++ {
		l.PushInteger(i)
		typ, err := l.Table(idx, 0)
		if err != nil {
			return fmt.Errorf("#%d: %w", i, err)
		}
		if typ == lua.TypeNil {
			return nil
		}
		err = f(i)
		l.SetTop(top)
		if err != nil {
			return err
		}
	}
}

// loadFunction is a wrapper around the load builtin function
// that forces the mode to be "t".
func loadFunction(l *lua.State) (int, error) {
	const maxLoadArgs = 4
	const modeArg = 3

	l.SetTop(maxLoadArgs)
	switch l.Type(modeArg) {
	case lua.TypeNil:
		l.PushString("t")
		l.Replace(modeArg)
	case lua.TypeString:
		if s, _ := l.ToString(modeArg); s != "t" {
			l.PushNil()
			l.PushString(fmt.Sprintf("load only supports text chunks (got %q)", s))
			return 2, nil
		}
	default:
		l.PushNil()
		l.PushString("only permitted mode for load is 't'")
		return 2, nil
	}
	l.PushValue(lua.UpvalueIndex(1))
	l.Insert(1)
	if err := l.Call(maxLoadArgs, lua.MultipleReturns, 0); err != nil {
		return 0, err
	}
	return l.Top(), nil
}

// loadfileFunction is the global loadfile function implementation.
func (eval *Eval) loadfileFunction(ctx context.Context, l *lua.State) (int, error) {
	filename, err := lua.CheckString(l, 1)
	if err != nil {
		return 0, err
	}

	// TODO(someday): If we have dependencies and we're using a non-local store,
	// export the store object and read it.
	var rewrites []string
	for _, dep := range l.StringContext(1) {
		rawOutRef, isDrvOutput := strings.CutPrefix(dep, derivationOutputContextPrefix)
		if !isDrvOutput {
			continue
		}
		outputRef, err := zbstore.ParseOutputReference(rawOutRef)
		if err != nil {
			l.PushNil()
			l.PushString(fmt.Sprintf("internal error: parse string context: %v", err))
			return 2, nil
		}
		resp := new(zbstore.RealizeResponse)
		// TODO(someday): Only realize single output.
		// TODO(someday): Batch.
		err = jsonrpc.Do(ctx, eval.store, zbstore.RealizeMethod, resp, &zbstore.RealizeRequest{
			DrvPath: outputRef.DrvPath,
		})
		if err != nil {
			l.PushNil()
			l.PushString(fmt.Sprintf("realize %v: %v", outputRef, err))
			return 2, nil
		}
		output, err := xiter.Single(resp.OutputsByName(outputRef.OutputName))
		if err != nil {
			l.PushNil()
			l.PushString(fmt.Sprintf("realize %v: outputs: %v", outputRef, err))
			return 2, nil
		}
		if !output.Path.Valid || output.Path.X == "" {
			l.PushNil()
			l.PushString(fmt.Sprintf("realize %v: build failed", outputRef))
			return 2, nil
		}
		placeholder := zbstore.UnknownCAOutputPlaceholder(outputRef)
		rewrites = append(rewrites, placeholder, string(output.Path.X))
	}
	if len(rewrites) > 0 {
		filename = strings.NewReplacer(rewrites...).Replace(filename)
	}

	const modeArg = 2
	switch l.Type(modeArg) {
	case lua.TypeNil, lua.TypeNone:
	case lua.TypeString:
		if s, _ := l.ToString(modeArg); s != "t" {
			l.PushNil()
			l.PushString(fmt.Sprintf("loadfile only supports text chunks (got %q)", s))
			return 2, nil
		}
	default:
		l.PushNil()
		l.PushString("only permitted mode for loadfile is 't'")
		return 2, nil
	}

	const envArg = 3
	hasEnv := l.Type(envArg) != lua.TypeNone

	filename, err = absSourcePath(l, filename)
	if err != nil {
		l.PushNil()
		l.PushString(err.Error())
		return 2, nil
	}
	if err := loadFile(l, filename); err != nil {
		l.PushNil()
		l.PushString(err.Error())
		return 2, nil
	}

	if hasEnv {
		l.PushValue(envArg)
		if _, ok := l.SetUpvalue(-2, 1); !ok {
			// Remove env if not used.
			l.Pop(1)
		}
	}

	return 1, nil
}

// dofileFunction is the global dofile function implementation.
// It assumes that a loadfile function is its first upvalue.
func dofileFunction(l *lua.State) (int, error) {
	filename, err := lua.CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	l.SetTop(1)

	// Perform path resolution here instead of at loadfile,
	// since loadfile would just obtain our call record.
	resolved, err := absSourcePath(l, filename)
	if err != nil {
		return 0, fmt.Errorf("dofile: %v", err)
	}
	if resolved != filename {
		// Relative paths will not be store paths.
		l.PushString(resolved)
		l.Replace(1)
	}

	// loadfile(filename)
	l.PushValue(lua.UpvalueIndex(1))
	l.Insert(1)
	if err := l.Call(1, 2, 0); err != nil {
		return 0, fmt.Errorf("dofile: %v", err)
	}
	if l.IsNil(-2) {
		msg, _ := lua.ToString(l, -1)
		return 0, fmt.Errorf("dofile: %s", msg)
	}
	l.Pop(1)

	// Call the loaded function.
	if err := l.Call(0, lua.MultipleReturns, 0); err != nil {
		return 0, fmt.Errorf("dofile %s: %v", resolved, err)
	}
	return l.Top(), nil
}

//go:embed cache_sql
var rawSqlFiles embed.FS

func sqlFiles() fs.FS {
	fsys, err := fs.Sub(rawSqlFiles, "cache_sql")
	if err != nil {
		panic(err)
	}
	return fsys
}
