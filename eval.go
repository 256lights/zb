// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package zb

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

	"zombiezen.com/go/nix"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
	"zombiezen.com/go/zb/internal/lua"
)

//go:embed prelude.lua
var preludeSource string

type Eval struct {
	l        lua.State
	storeDir nix.StoreDirectory
	cache    *sqlite.Conn
}

func NewEval(storeDir nix.StoreDirectory, cacheDB string) (_ *Eval, err error) {
	if err := os.MkdirAll(filepath.Dir(cacheDB), 0o777); err != nil {
		return nil, fmt.Errorf("zb: new eval: %v", err)
	}
	eval := &Eval{storeDir: storeDir}
	eval.cache, err = openCache(context.TODO(), cacheDB)
	if err != nil {
		return nil, fmt.Errorf("zb: new eval: %v", err)
	}
	defer func() {
		if err != nil {
			eval.l.Close()
			eval.cache.Close()
			err = fmt.Errorf("zb: new eval: %v", err)
		}
	}()

	registerDerivationMetatable(&eval.l)

	base := lua.NewOpenBase(io.Discard, loadfileFunction)
	if err := lua.Require(&eval.l, lua.GName, true, base); err != nil {
		return nil, err
	}

	// Wrap load function.
	if tp := eval.l.RawField(-1, "load"); tp != lua.TypeFunction {
		return nil, fmt.Errorf("load is not a function")
	}
	eval.l.PushClosure(1, loadFunction)
	eval.l.RawSetField(-2, "load")

	// Replace dofile.
	if tp := eval.l.RawField(-1, "loadfile"); tp != lua.TypeFunction {
		return nil, fmt.Errorf("loadfile is not a function")
	}
	eval.l.PushClosure(1, dofileFunction)
	eval.l.RawSetField(-2, "dofile")

	// Set other built-ins.
	err = lua.SetFuncs(&eval.l, 0, map[string]lua.Function{
		"derivation": eval.derivationFunction,
		"path":       eval.pathFunction,
		"toFile":     eval.toFileFunction,
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

func openCache(ctx context.Context, path string) (*sqlite.Conn, error) {
	conn, err := sqlite.OpenConn(path, sqlite.OpenReadWrite, sqlite.OpenCreate)
	if err != nil {
		return nil, err
	}
	conn.SetInterrupt(ctx.Done())
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA journal_mode=wal;", nil); err != nil {
		conn.Close()
		return nil, fmt.Errorf("open cache %s: enable write-ahead logging: %v", path, err)
	}
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA foreign_keys=on;", nil); err != nil {
		conn.Close()
		return nil, fmt.Errorf("open cache %s: enable write-ahead logging: %v", path, err)
	}

	if err := prepareCache(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("open cache %s: %v", path, err)
	}

	var schema sqlitemigration.Schema
	for i := 1; ; i++ {
		migration, err := fs.ReadFile(sqlFiles(), fmt.Sprintf("schema/%02d.sql", i))
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("open cache %s: read migrations: %v", path, err)
		}
		schema.Migrations = append(schema.Migrations, string(migration))
	}
	err = sqlitemigration.Migrate(ctx, conn, schema)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("open cache %s: %v", path, err)
	}

	return conn, nil
}

func prepareCache(conn *sqlite.Conn) error {
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
	errors[1] = eval.cache.Close()
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

func (eval *Eval) File(exprFile string, attrPaths []string) ([]any, error) {
	defer eval.l.SetTop(0)
	if err := loadFile(&eval.l, exprFile); err != nil {
		return nil, err
	}
	if err := eval.l.Call(0, 1, 0); err != nil {
		eval.l.Pop(1)
		return nil, err
	}
	return eval.attrPaths(attrPaths)
}

func (eval *Eval) Expression(expr string, attrPaths []string) ([]any, error) {
	defer eval.l.SetTop(0)
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
func loadfileFunction(l *lua.State) (int, error) {
	filename, err := lua.CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	if len(l.StringContext(1)) > 0 {
		l.PushNil()
		l.PushString("import from derivation not supported")
		return 2, nil
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
	if len(l.StringContext(1)) > 0 {
		return 0, errors.New("dofile: import from derivation not supported")
	}
	l.SetTop(1)

	// Perform path resolution here instead of at loadfile,
	// since loadfile would just obtain our call record.
	resolved, err := absSourcePath(l, filename)
	if err != nil {
		return 0, fmt.Errorf("dofile: %v", err)
	}
	if resolved != filename {
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
