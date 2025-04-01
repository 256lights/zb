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
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"zb.256lights.llc/pkg/internal/lua"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
)

const stdlibRegistryKey = "zb.256lights.llc/pkg/internal/frontend stdlib"

//go:embed prelude.luac
var preludeSource []byte

// Options is the set of parameters for [NewEval].
type Options struct {
	// Store is an open JSON-RPC client to the store server.
	Store Store
	// StoreDirectory is the store directory used by the store.
	StoreDirectory zbstore.Directory
	// CacheDBPath is the path to a database file used to speed up store imports.
	// If empty, an in-memory cache will be used.
	CacheDBPath string
	// LookupEnv is called for the Lua os.getenv function.
	// If nil, os.getenv will always return nil.
	LookupEnv func(ctx context.Context, key string) (string, bool)
}

// Store is the set of store operations that [Eval] needs.
//
// Exists reports whether the given path exists in the store.
//
// Import reads the `nix-store --export` data from the given reader
// and adds any objects from the stream into the store.
//
// Realize starts a build for the given derivation paths,
// waits for the build to finish,
// then returns the results of the build.
type Store interface {
	Exists(ctx context.Context, path string) (bool, error)
	Import(ctx context.Context, r io.Reader) error
	Realize(ctx context.Context, want sets.Set[zbstore.OutputReference]) ([]*zbstore.BuildResult, error)
}

type Eval struct {
	store     Store
	storeDir  zbstore.Directory
	cachePool *sqlitemigration.Pool
	lookupEnv func(ctx context.Context, key string) (string, bool)

	baseImportContext context.Context
	cancelImports     context.CancelFunc
	importGroup       sync.WaitGroup

	zygoteMutex sync.Mutex
	// zygote is a Lua state that populates its registry in [*Eval.initZygote].
	// New states are created by copying the registry into their own tables.
	zygote lua.State

	loadedMutex sync.Mutex
	// loadedState is a Lua state that has a table at the top of all the modules.
	loadedState lua.State
}

func NewEval(opts *Options) (_ *Eval, err error) {
	eval := &Eval{
		store:     opts.Store,
		storeDir:  opts.StoreDirectory,
		lookupEnv: opts.LookupEnv,
	}
	if eval.lookupEnv == nil {
		eval.lookupEnv = func(ctx context.Context, key string) (string, bool) {
			return "", false
		}
	}

	var schema sqlitemigration.Schema
	for i := 1; ; i++ {
		migration, err := fs.ReadFile(sqlFiles(), fmt.Sprintf("schema/%02d.sql", i))
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("zb: new eval: read migrations: %v", err)
		}
		schema.Migrations = append(schema.Migrations, string(migration))
	}
	if opts.CacheDBPath == "" {
		// Because we are limiting the pool size to 1,
		// this acts like a mutex on a single in-memory connection.
		// The SQLite library returns an error if we use ":memory:",
		// so we pick a different string and pass the [sqlite.OpenMemory] flag.
		eval.cachePool = sqlitemigration.NewPool("mycache", schema, sqlitemigration.Options{
			Flags:       sqlite.OpenReadWrite | sqlite.OpenMemory,
			PoolSize:    1,
			PrepareConn: prepareCache,
		})
	} else {
		if err := os.MkdirAll(filepath.Dir(opts.CacheDBPath), 0o777); err != nil {
			return nil, fmt.Errorf("zb: new eval: %v", err)
		}
		eval.cachePool = sqlitemigration.NewPool(opts.CacheDBPath, schema, sqlitemigration.Options{
			Flags:       sqlite.OpenCreate | sqlite.OpenReadWrite,
			PoolSize:    1,
			PrepareConn: prepareCache,
		})
	}

	if err := eval.initZygote(); err != nil {
		return nil, fmt.Errorf("zb: new eval: %v", err)
	}
	if err := eval.initState(&eval.loadedState); err != nil {
		return nil, fmt.Errorf("zb: new eval: %v", err)
	}
	eval.loadedState.CreateTable(0, 0)
	eval.baseImportContext, eval.cancelImports = context.WithCancel(context.Background())
	return eval, nil
}

func (eval *Eval) initZygote() error {
	ctx := context.Background()
	l := &eval.zygote

	if err := registerDerivationMetatable(ctx, l); err != nil {
		return err
	}
	if err := registerModuleMetatable(ctx, l); err != nil {
		return err
	}

	base := lua.NewOpenBase(&lua.BaseOptions{
		Output: io.Discard,
	})
	if err := lua.Require(ctx, l, lua.GName, true, base); err != nil {
		return err
	}

	// Set other built-ins.
	extraBaseFunctions := map[string]lua.Function{
		"await":      awaitFunction,
		"derivation": eval.derivationFunction,
		"import":     eval.importFunction,
		"toFile":     eval.toFileFunction,
		"path":       eval.pathFunction,
	}
	if err := lua.SetPureFunctions(ctx, l, 0, extraBaseFunctions); err != nil {
		return err
	}
	l.PushString(string(eval.storeDir))
	if err := l.SetField(ctx, -2, "storeDir"); err != nil {
		return err
	}

	// Wrap load function.
	if tp := l.RawField(-1, "load"); tp != lua.TypeFunction {
		return fmt.Errorf("load is not a function")
	}
	l.PushPureFunction(1, loadFunction)
	if err := l.RawSetField(-2, "load"); err != nil {
		return err
	}

	// Remove unwanted base functions.
	if err := clearFields(l, "print", "loadfile", "dofile", "collectgarbage"); err != nil {
		return err
	}

	// Pop base library.
	l.Pop(1)

	// Load other standard libraries.
	if err := lua.Require(ctx, l, lua.MathLibraryName, true, lua.NewOpenMath(nil)); err != nil {
		return err
	}
	if err := clearFields(l, "random", "randomseed"); err != nil {
		return err
	}
	l.Pop(1)
	if err := lua.Require(ctx, l, lua.StringLibraryName, true, lua.OpenString); err != nil {
		return err
	}
	if err := clearFields(l, "dump"); err != nil {
		return err
	}
	l.Pop(1)
	if err := lua.Require(ctx, l, lua.TableLibraryName, true, lua.OpenTable); err != nil {
		return err
	}
	l.Pop(1)
	if err := lua.Require(ctx, l, lua.UTF8LibraryName, true, lua.OpenUTF8); err != nil {
		return err
	}
	l.Pop(1)
	if err := lua.Require(ctx, l, lua.OSLibraryName, true, eval.openOS); err != nil {
		return err
	}
	l.Pop(1)

	// Run prelude.
	if err := l.Load(bytes.NewReader(preludeSource), lua.UnknownSource, "b"); err != nil {
		return err
	}
	if err := l.Call(ctx, 0, 0); err != nil {
		return err
	}

	// Copy globals to stdlib key.
	l.RawIndex(lua.RegistryIndex, lua.RegistryIndexGlobals)
	if err := l.RawSetField(lua.RegistryIndex, stdlibRegistryKey); err != nil {
		return err
	}

	// Freeze everything in registry and metatables.
	if err := l.Freeze(lua.RegistryIndex); err != nil {
		return err
	}
	var err error
	forEachSharedMetatableType(l, func() bool {
		if !l.Metatable(-1) {
			l.Pop(1)
			return true
		}
		err = l.Freeze(-1)
		l.Pop(2)
		return err == nil
	})
	if err != nil {
		return err
	}

	return nil
}

func clearFields(l *lua.State, fieldNames ...string) error {
	for _, k := range fieldNames {
		l.PushNil()
		if err := l.RawSetField(-2, k); err != nil {
			return err
		}
	}
	return nil
}

func (eval *Eval) openOS(ctx context.Context, l *lua.State) (int, error) {
	lua.NewPureLib(l, map[string]lua.Function{
		"getenv": func(ctx context.Context, l *lua.State) (int, error) {
			key, err := lua.CheckString(l, 1)
			if err != nil {
				return 0, err
			}
			if val, ok := eval.lookupEnv(ctx, key); ok {
				l.PushString(val)
			} else {
				l.PushNil()
			}
			return 1, nil
		},
	})
	return 1, nil
}

func (eval *Eval) newState() (*lua.State, error) {
	l := new(lua.State)
	if err := eval.initState(l); err != nil {
		return nil, err
	}
	return l, nil
}

func (eval *Eval) initState(l *lua.State) error {
	eval.zygoteMutex.Lock()
	eval.zygote.PushValue(lua.RegistryIndex)
	forEachSharedMetatableType(&eval.zygote, func() bool {
		if !eval.zygote.Metatable(-1) {
			eval.zygote.PushNil()
		}
		eval.zygote.Remove(-2)
		return true
	})
	err := l.XMove(&eval.zygote, eval.zygote.Top())
	eval.zygoteMutex.Unlock()
	if err != nil {
		return err
	}

	// Copy over metatables.
	const tableIndex = 1
	const firstMetatableIndex = 2
	forEachSharedMetatableType(l, func() bool {
		l.Rotate(firstMetatableIndex, -1) // Move next metatable to the top.
		err = l.SetMetatable(-2)
		l.Pop(1) // Pop off dummy value.
		return err == nil
	})
	if err != nil {
		return err
	}

	// Copy over entries into registry.
	l.PushNil()
	for l.Next(tableIndex) {
		if l.IsInteger(-2) {
			if i, _ := l.ToInteger(-2); i == lua.RegistryIndexGlobals {
				l.Pop(1)
				continue
			}
		}
		l.PushValue(-2)
		l.Insert(-2)

		if err := l.RawSet(lua.RegistryIndex); err != nil {
			return err
		}
	}
	l.Pop(1) // Pop the zygote registry.

	// Set up globals metatable.
	// Any unknown names will be looked up in the standard library registry key.
	// We don't set it to the standard library table directly
	// because if this value gets moved to a different state,
	// we want to respect the state's registry key.
	l.RawIndex(lua.RegistryIndex, lua.RegistryIndexGlobals)
	lua.NewPureLib(l, map[string]lua.Function{
		"__metatable": nil,
		"__index": func(ctx context.Context, l *lua.State) (int, error) {
			if l.Type(2) == lua.TypeString {
				if s, _ := l.ToString(2); s == lua.GName {
					l.SetTop(1)
					return 1, nil
				}
			}
			if tp, err := l.Field(ctx, lua.RegistryIndex, stdlibRegistryKey); err != nil {
				return 0, err
			} else if tp == lua.TypeNil {
				// If the state does not have a standard library in the registry (bug?),
				// then return nothing.
				return 1, nil
			}
			l.Insert(2)
			l.SetTop(3)
			if _, err := l.Table(ctx, 2); err != nil {
				return 0, err
			}
			return 1, nil
		},
	})
	if err := l.SetMetatable(-2); err != nil {
		return err
	}
	l.Pop(1)

	return nil
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
	eval.cancelImports()
	eval.importGroup.Wait()
	return eval.cachePool.Close()
}

func (eval *Eval) File(ctx context.Context, exprFile string, attrPaths []string) ([]any, error) {
	l, err := eval.newState()
	if err != nil {
		return nil, err
	}
	defer l.Close()

	l.PushClosure(0, messageHandler)
	l.PushPureFunction(0, eval.importFunction)
	l.PushString(exprFile)
	if err := l.PCall(ctx, 1, 2, -3); err != nil {
		return nil, err
	}
	if errMsg, _ := l.ToString(-1); errMsg != "" {
		return nil, errors.New(errMsg)
	}
	l.Pop(1)
	return evalAttrPaths(ctx, l, attrPaths)
}

func (eval *Eval) Expression(ctx context.Context, expr string, attrPaths []string) ([]any, error) {
	l, err := eval.newState()
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
	defer l.SetTop(l.Top())
	if len(paths) == 0 {
		l.PushValue(-1) // Modules can cause luaToGo to mutate the stack.
		x, err := luaToGo(ctx, l)
		if err != nil {
			return nil, err
		}
		return []any{x}, nil
	}

	result := make([]any, 0, len(paths))
	l.PushPureFunction(0, messageHandler)
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
		l.PushValue(-3)
		if err := l.PCall(ctx, 1, 1, -3); err != nil {
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
	for {
		// Resolve modules, if any.
		for {
			mod := testModule(l, -1)
			if mod == nil {
				break
			}
			l.Pop(1)
			if err := waitForModule(ctx, l, mod); err != nil {
				return nil, err
			}
		}

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
			return l.ToBoolean(-1), nil
		case lua.TypeString:
			s, _ := l.ToString(-1)
			return s, nil
		case lua.TypeTable:
			// Check first if table is an array.
			if arr, err := luaTableToGoSlice(ctx, l); err == nil {
				return arr, nil
			} else if err != errNotASequence {
				return nil, err
			}

			// Otherwise it's an object.
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
			if typ == lua.TypeUserdata {
				x, _ := l.ToUserdata(-1)
				return nil, fmt.Errorf("cannot convert %T userdata to Go", x)
			}
			return nil, fmt.Errorf("cannot convert %v to Go", typ)
		}
	}
}

var errNotASequence = errors.New("table is not a sequence")

func luaTableToGoSlice(ctx context.Context, l *lua.State) ([]any, error) {
	defer l.SetTop(l.Top())
	if !l.CheckStack(2) {
		return nil, errors.New("depth exceeded")
	}

	// Arrays must have all integer keys
	// except an "n" integer field is permitted
	// (to support nils).
	// This generally follows the pattern of table.pack.
	n := int64(-1)
	l.PushNil()
	for l.Next(-2) {
		switch l.Type(-2) {
		case lua.TypeNumber:
			// Only integer keys for a table.
			i, ok := l.ToInteger(-2)
			if !ok || i < 1 || i > math.MaxInt {
				return nil, errNotASequence
			}
		case lua.TypeString:
			// Only "n" allowed.
			if s, _ := l.ToString(-2); s != "n" {
				return nil, errNotASequence
			}
			var ok bool
			n, ok = l.ToInteger(-1)
			if !ok || n < 0 || n > math.MaxInt {
				return nil, errNotASequence
			}
		default:
			return nil, errNotASequence
		}
		l.Pop(1)
	}

	var result []any
	l.PushNil()
	for l.Next(-2) {
		i, ok := l.ToInteger(-2)
		if !ok {
			// "n" field.
			l.Pop(1)
			continue
		}
		result = appendZero(result, int(i-1)-len(result))
		x, err := luaToGo(ctx, l)
		if err != nil {
			return nil, fmt.Errorf("[%d]: %w", i, err)
		}
		result = append(result, x)
		l.Pop(1)
	}
	if n >= 0 {
		result = appendZero(result, int(n)-len(result))
	} else if len(result) == 0 {
		return nil, errNotASequence
	}
	return result, nil
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

// forEachSharedMetatableType pushes nil, false, 0, "", and a no-op function
// onto l's stack in sequence
// and calls f after each push.
// If f returns false, forEachSharedMetatableType skips pushing the remaining values.
// This can be used to do something for each Lua type that shares [metatables]
// among all values of the type.
//
// [metatables]: https://www.lua.org/manual/5.4/manual.html#2.4
func forEachSharedMetatableType(l *lua.State, f func() bool) {
	l.PushNil()
	if !f() {
		return
	}
	l.PushBoolean(false)
	if !f() {
		return
	}
	l.PushNumber(0)
	if !f() {
		return
	}
	l.PushString("")
	if !f() {
		return
	}
	l.PushClosure(0, func(ctx context.Context, l *lua.State) (int, error) {
		return 0, nil
	})
	f()
}

// appendZero appends n copies of the zero value to a slice.
func appendZero[S ~[]E, E any](s S, n int) S {
	return append(s, make(S, n)...)
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
