// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

//go:generate go run ../../cmd/zb-luac --source =(prelude) -o prelude.luac prelude.lua

package frontend

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	slashpath "path"
	"path/filepath"
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

const cacheConnRegistryKey = "zb.256lights.llc/pkg/internal/frontend cacheConn"

//go:embed prelude.luac
var preludeSource []byte

type Eval struct {
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

	return &Eval{
		store:    store,
		storeDir: storeDir,
		cachePool: sqlitemigration.NewPool(cacheDB, schema, sqlitemigration.Options{
			Flags:       sqlite.OpenCreate | sqlite.OpenReadWrite,
			PoolSize:    1,
			PrepareConn: prepareCache,
		}),
	}, nil
}

func (eval *Eval) newState(ctx context.Context, cache *sqlite.Conn) (*lua.State, error) {
	l := new(lua.State)
	if err := registerDerivationMetatable(ctx, l); err != nil {
		return nil, err
	}

	base := lua.NewOpenBase(&lua.BaseOptions{
		Output: io.Discard,
	})
	if err := lua.Require(ctx, l, lua.GName, true, base); err != nil {
		return nil, err
	}

	// Set other built-ins.
	extraBaseFunctions := map[string]lua.Function{
		"baseNameOf": func(ctx context.Context, l *lua.State) (int, error) {
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
		"derivation": eval.derivationFunction,
		"loadfile":   eval.loadfileFunction,
		"toFile":     eval.toFileFunction,
		"path":       eval.pathFunction,
	}
	if err := lua.SetFunctions(ctx, l, 0, extraBaseFunctions); err != nil {
		return nil, err
	}
	l.PushString(string(eval.storeDir))
	if err := l.SetField(ctx, -2, "storeDir"); err != nil {
		return nil, err
	}

	// Wrap load function.
	if tp := l.RawField(-1, "load"); tp != lua.TypeFunction {
		return nil, fmt.Errorf("load is not a function")
	}
	l.PushClosure(1, loadFunction)
	l.RawSetField(-2, "load")

	// dofile needs loadfile as its first upvalue.
	l.RawField(-1, "loadfile")
	l.PushClosure(1, dofileFunction)
	l.RawSetField(-2, "dofile")

	// Pop base library.
	l.Pop(1)

	// Load other standard libraries.
	if err := lua.Require(ctx, l, lua.MathLibraryName, true, lua.NewOpenMath(nil)); err != nil {
		return nil, err
	}
	l.PushNil()
	l.RawSetField(-2, "random")
	l.PushNil()
	l.RawSetField(-2, "randomseed")
	l.Pop(1)
	if err := lua.Require(ctx, l, lua.StringLibraryName, true, lua.OpenString); err != nil {
		return nil, err
	}
	l.PushNil()
	l.RawSetField(-2, "dump")
	l.Pop(1)
	if err := lua.Require(ctx, l, lua.TableLibraryName, true, lua.OpenTable); err != nil {
		return nil, err
	}
	l.Pop(1)
	if err := lua.Require(ctx, l, lua.UTF8LibraryName, true, lua.OpenUTF8); err != nil {
		return nil, err
	}
	l.Pop(1)

	// Run prelude.
	if err := l.Load(bytes.NewReader(preludeSource), lua.UnknownSource, "b"); err != nil {
		return nil, err
	}
	if err := l.Call(ctx, 0, 0); err != nil {
		return nil, err
	}

	// Stash cache connection.
	l.NewUserdata(cache, 0)
	if err := l.RawSetField(lua.RegistryIndex, cacheConnRegistryKey); err != nil {
		return nil, err
	}

	return l, nil
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
	return eval.cachePool.Close()
}

func (eval *Eval) File(ctx context.Context, exprFile string, attrPaths []string) ([]any, error) {
	cache, err := eval.cachePool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer eval.cachePool.Put(cache)

	l, err := eval.newState(ctx, cache)
	if err != nil {
		return nil, err
	}
	defer l.Close()

	l.PushClosure(0, messageHandler)
	if err := loadFile(l, exprFile); err != nil {
		return nil, err
	}
	if err := l.PCall(ctx, 0, 1, -2); err != nil {
		return nil, err
	}
	return evalAttrPaths(ctx, l, attrPaths)
}

func (eval *Eval) Expression(ctx context.Context, expr string, attrPaths []string) ([]any, error) {
	cache, err := eval.cachePool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer eval.cachePool.Put(cache)

	l, err := eval.newState(ctx, cache)
	if err != nil {
		return nil, err
	}
	defer l.Close()

	l.PushPureFunction(0, messageHandler)
	if err := loadExpression(l, expr); err != nil {
		return nil, err
	}
	if err := l.PCall(ctx, 0, 1, -2); err != nil {
		return nil, err
	}
	return evalAttrPaths(ctx, l, attrPaths)
}

// evalAttrPaths evaluates all the attribute paths given
// against the value on the top of the stack.
func evalAttrPaths(ctx context.Context, l *lua.State, paths []string) ([]any, error) {
	if len(paths) == 0 {
		x, err := luaToGo(ctx, l)
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
		if err := l.Load(strings.NewReader(expr), lua.LiteralSource(expr), "t"); err != nil {
			l.Pop(1)
			return result, fmt.Errorf("%s: %v", p, err)
		}
		l.PushValue(-2)
		if err := l.Call(ctx, 1, 1); err != nil {
			return result, fmt.Errorf("%s: %v", p, err)
		}
		x, err := luaToGo(ctx, l)
		l.Pop(1)
		if err != nil {
			return result, fmt.Errorf("%s: %v", p, err)
		}
		result = append(result, x)
	}
	return result, nil
}

func luaToGo(ctx context.Context, l *lua.State) (any, error) {
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
		err := ipairs(ctx, l, -1, func(i int64) error {
			x, err := luaToGo(ctx, l)
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
			v, err := luaToGo(ctx, l)
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

	if err := l.Load(bufio.NewReader(f), lua.FilenameSource(path), "t"); err != nil {
		return fmt.Errorf("load file %s: %w", path, err)
	}
	return nil
}

func loadExpression(l *lua.State, expr string) error {
	if err := l.Load(strings.NewReader("return "+expr+";"), lua.LiteralSource(expr), "t"); err == nil {
		return nil
	}
	if err := l.Load(strings.NewReader(expr), lua.LiteralSource(expr), "t"); err != nil {
		return err
	}
	return nil
}

func ipairs(ctx context.Context, l *lua.State, idx int, f func(i int64) error) error {
	idx = l.AbsIndex(idx)
	top := l.Top()
	defer l.SetTop(top)
	for i := int64(1); ; i++ {
		typ, err := l.Index(ctx, idx, i)
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
func loadFunction(ctx context.Context, l *lua.State) (int, error) {
	const maxLoadArgs = 4
	const modeArg = 3

	l.SetTop(maxLoadArgs)
	switch l.Type(modeArg) {
	case lua.TypeNil:
		l.PushString("t")
		if err := l.Replace(modeArg); err != nil {
			return 0, err
		}
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
	if err := l.Call(ctx, maxLoadArgs, lua.MultipleReturns); err != nil {
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
	filenameContext := l.StringContext(1)

	// TODO(someday): If we have dependencies and we're using a non-local store,
	// export the store object and read it.
	var rewrites []string
	for dep := range filenameContext {
		c, err := parseContextString(dep)
		if err != nil {
			l.PushNil()
			l.PushString(fmt.Sprintf("internal error: %v", err))
			return 2, nil
		}
		if c.outputReference.IsZero() {
			continue
		}
		resp := new(zbstore.RealizeResponse)
		// TODO(someday): Only realize single output.
		// TODO(someday): Batch.
		err = jsonrpc.Do(ctx, eval.store, zbstore.RealizeMethod, resp, &zbstore.RealizeRequest{
			DrvPath: c.outputReference.DrvPath,
		})
		if err != nil {
			l.PushNil()
			l.PushString(fmt.Sprintf("realize %v: %v", c.outputReference, err))
			return 2, nil
		}
		output, err := xiter.Single(resp.OutputsByName(c.outputReference.OutputName))
		if err != nil {
			l.PushNil()
			l.PushString(fmt.Sprintf("realize %v: outputs: %v", c.outputReference, err))
			return 2, nil
		}
		if !output.Path.Valid || output.Path.X == "" {
			l.PushNil()
			l.PushString(fmt.Sprintf("realize %v: build failed", c.outputReference))
			return 2, nil
		}
		placeholder := zbstore.UnknownCAOutputPlaceholder(c.outputReference)
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

	filename, err = absSourcePath(l, filename, filenameContext)
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
		if _, err := l.SetUpvalue(-2, 1); err != nil {
			// Remove env if not used.
			l.Pop(1)
		}
	}

	return 1, nil
}

// dofileFunction is the global dofile function implementation.
// It assumes that a loadfile function is its first upvalue.
func dofileFunction(ctx context.Context, l *lua.State) (int, error) {
	filename, err := lua.CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	l.SetTop(1)

	// Perform path resolution here instead of at loadfile,
	// since loadfile would just obtain our call record.
	resolved, err := absSourcePath(l, filename, l.StringContext(1))
	if err != nil {
		return 0, fmt.Errorf("dofile: %v", err)
	}
	if resolved != filename {
		// Relative paths will not be store paths.
		l.PushString(resolved)
		if err := l.Replace(1); err != nil {
			return 0, err
		}
	}

	// loadfile(filename)
	l.PushValue(lua.UpvalueIndex(1))
	l.Insert(1)
	if err := l.Call(ctx, 1, 2); err != nil {
		return 0, fmt.Errorf("dofile: %v", err)
	}
	if l.IsNil(-2) {
		msg, _, _ := lua.ToString(ctx, l, -1)
		return 0, fmt.Errorf("dofile: %s", msg)
	}
	l.Pop(1)

	// Call the loaded function.
	if err := l.Call(ctx, 0, lua.MultipleReturns); err != nil {
		return 0, fmt.Errorf("dofile %s: %v", resolved, err)
	}
	return l.Top(), nil
}

func messageHandler(ctx context.Context, l *lua.State) (int, error) {
	msg, ok := l.ToString(1)
	sctx := l.StringContext(1)
	if !ok {
		hasMeta, err := lua.CallMeta(ctx, l, 1, "__tostring")
		if err != nil {
			return 0, err
		}
		if hasMeta && l.Type(-1) == lua.TypeString {
			return 1, nil
		}
		msg = fmt.Sprintf("(error object is a %v value)", l.Type(1))
	}

	l.PushStringContext(lua.Traceback(l, msg, 1), sctx)
	return 1, nil
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
