package zb

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/cgo"
	"strings"

	"zombiezen.com/go/nix"
	"zombiezen.com/go/zb/internal/lua"
	"zombiezen.com/go/zb/internal/sortedset"
)

type Eval struct {
	l        lua.State
	storeDir nix.StoreDirectory
}

func NewEval(storeDir nix.StoreDirectory) *Eval {
	eval := &Eval{storeDir: storeDir}
	registerDerivationMetatable(&eval.l)

	base := lua.NewOpenBase(io.Discard, func(l *lua.State) (int, error) {
		return 0, fmt.Errorf("loadfile not supported")
	})
	if err := lua.Require(&eval.l, lua.GName, true, base); err != nil {
		panic(err)
	}
	err := lua.SetFuncs(&eval.l, 0, map[string]lua.Function{
		"derivation": eval.derivationFunction,
		"path":       eval.pathFunction,
	})
	if err != nil {
		panic(err)
	}
	eval.l.PushString(string(storeDir))
	if err := eval.l.SetField(-2, "storeDir", 0); err != nil {
		panic(err)
	}
	eval.l.Pop(1)

	return eval
}

func (eval *Eval) Close() error {
	return eval.l.Close()
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

const derivationTypeName = "derivation"

func registerDerivationMetatable(l *lua.State) {
	lua.NewMetatable(l, derivationTypeName)
	err := lua.SetFuncs(l, 0, map[string]lua.Function{
		"__index":     indexDerivation,
		"__pairs":     derivationPairs,
		"__gc":        gcDerivation,
		"__metatable": nil, // prevent Lua access to metatable
	})
	if err != nil {
		panic(err)
	}
	l.Pop(1)
}

func (eval *Eval) derivationFunction(l *lua.State) (int, error) {
	if !l.IsTable(1) {
		return 0, lua.NewTypeError(l, 1, lua.TypeTable.String())
	}
	drv := &Derivation{
		Dir: eval.storeDir,
		Env: make(map[string]string),
		Outputs: map[string]*DerivationOutput{
			"out": RecursiveFileFloatingCAOutput(nix.SHA256),
		},
	}

	// Start a copy of the table.
	l.CreateTable(0, int(l.RawLen(1)))
	tableCopyIndex := l.Top()

	// Obtain environment variables from extra pairs.
	l.PushNil()
	for l.Next(1) {
		if l.Type(-2) != lua.TypeString {
			// Skip this pair.
			l.Pop(1)
			continue
		}

		// Store copy of pair into table.
		l.PushValue(-2) // Push key.
		l.PushValue(-2) // Push value.
		// TODO(soon): Validate primitive or list.
		l.RawSet(tableCopyIndex)

		// Handle special pairs.
		k, _ := l.ToString(-2)
		switch k {
		case "name":
			if typ := l.Type(-1); typ != lua.TypeString {
				return 0, fmt.Errorf("name argument: %v expected, got %v", lua.TypeString, typ)
			}
			drv.Name, _ = l.ToString(-1)
		case "system":
			if typ := l.Type(-1); typ != lua.TypeString {
				return 0, fmt.Errorf("system argument: %v expected, got %v", lua.TypeString, typ)
			}
			drv.System, _ = l.ToString(-1)
		case "builder":
			if typ := l.Type(-1); typ != lua.TypeString {
				return 0, fmt.Errorf("system argument: %v expected, got %v", lua.TypeString, typ)
			}
			var err error
			drv.Builder, err = stringToEnvVar(l, drv, -1)
			if err != nil {
				return 0, fmt.Errorf("%s: %v", k, err)
			}
		case "args":
			if typ := l.Type(-1); typ != lua.TypeTable {
				return 0, fmt.Errorf("args argument: %v expected, got %v", lua.TypeTable, typ)
			}
			err := ipairs(l, -1, func(i int64) error {
				arg, err := stringToEnvVar(l, drv, -1)
				if err != nil {
					return fmt.Errorf("#%d: %v", i, err)
				}
				drv.Args = append(drv.Args, arg)
				return nil
			})
			if err != nil {
				return 0, fmt.Errorf("%s %v", k, err)
			}
		}

		v, err := toEnvVar(l, drv, -1, true)
		if err != nil {
			return 0, fmt.Errorf("%s: %v", k, err)
		}
		drv.Env[k] = v

		// Remove value, keeping key for the next iteration.
		l.Pop(1)
	}

	drv.Env[defaultDerivationOutputName] = hashPlaceholder(defaultDerivationOutputName)
	drvPath, err := writeDerivation(context.TODO(), drv)
	if err != nil {
		return 0, fmt.Errorf("derivation: %v", err)
	}
	l.PushStringContext(string(drvPath), []string{string(drvPath)})
	if err := l.SetField(tableCopyIndex, "drvPath", 0); err != nil {
		return 0, fmt.Errorf("derivation: %v", err)
	}

	placeholder := unknownCAOutputPlaceholder(drvPath, defaultDerivationOutputName)
	l.PushStringContext(placeholder, []string{
		"!" + defaultDerivationOutputName + "!" + string(drvPath),
	})
	if err := l.SetField(tableCopyIndex, defaultDerivationOutputName, 0); err != nil {
		return 0, fmt.Errorf("derivation: %v", err)
	}

	l.NewUserdataUV(8, 1)
	l.Rotate(-2, -1) // Swap userdata and argument table copy.
	l.SetUserValue(-2, 1)
	handle := cgo.NewHandle(drv)
	setUserdataHandle(l, -1, handle)

	lua.SetMetatable(l, derivationTypeName)

	return 1, nil
}

func toEnvVar(l *lua.State, drv *Derivation, idx int, allowLists bool) (string, error) {
	idx = l.AbsIndex(idx)
	switch typ := l.Type(idx); typ {
	case lua.TypeNil:
		return "", nil
	case lua.TypeBoolean:
		if !l.ToBoolean(idx) {
			return "", nil
		}
		return "1", nil
	case lua.TypeString, lua.TypeNumber:
		return stringToEnvVar(l, drv, idx)
	default:
		if hasMethod, err := lua.CallMeta(l, idx, "__tostring"); err != nil {
			return "", err
		} else if hasMethod {
			s, err := stringToEnvVar(l, drv, -1)
			if err != nil {
				return "", fmt.Errorf("__tostring result: %v", err)
			}
			return s, nil
		}

		// No __tostring? Then assume this is an array/list.
		if typ != lua.TypeTable {
			return "", fmt.Errorf("%v cannot be used as an environment variable", typ)
		}
		if !allowLists {
			return "", fmt.Errorf("sub-tables not permitted as environment variable values")
		}
		sb := new(strings.Builder)
		err := ipairs(l, idx, func(i int64) error {
			if i > 1 {
				sb.WriteString(" ")
			}
			s, err := toEnvVar(l, drv, -1, false)
			if err != nil {
				return fmt.Errorf("#%d: %v", i, err)
			}
			sb.WriteString(s)
			return nil
		})
		if err != nil {
			return "", err
		}
		return sb.String(), nil
	}
}

func stringToEnvVar(l *lua.State, drv *Derivation, idx int) (string, error) {
	if !l.IsString(idx) {
		return "", fmt.Errorf("%v is not a string", l.Type(idx))
	}
	l.PushValue(idx) // Clone so that we don't munge a number.
	defer l.Pop(1)
	s, _ := l.ToString(-1)
	for _, dep := range l.StringContext(-1) {
		if rest, isDrv := strings.CutPrefix(dep, "!"); isDrv {
			outName, drvPath, ok := strings.Cut(rest, "!")
			if !ok {
				return "", fmt.Errorf("internal error: malformed context %q", dep)
			}
			if drv.InputDerivations == nil {
				drv.InputDerivations = make(map[nix.StorePath]*sortedset.Set[string])
			}
			if drv.InputDerivations[nix.StorePath(drvPath)] == nil {
				drv.InputDerivations[nix.StorePath(drvPath)] = new(sortedset.Set[string])
			}
			drv.InputDerivations[nix.StorePath(drvPath)].Add(outName)
		} else {
			drv.InputSources.Add(nix.StorePath(dep))
		}
	}
	return s, nil
}

func toDerivation(l *lua.State) (*Derivation, error) {
	const idx = 1
	if _, err := lua.CheckUserdata(l, idx, derivationTypeName); err != nil {
		return nil, err
	}
	drv := testDerivation(l, idx)
	if drv == nil {
		return nil, lua.NewArgError(l, idx, "could not extract derivation")
	}
	return drv, nil
}

func testDerivation(l *lua.State, idx int) *Derivation {
	handle, _ := testUserdataHandle(l, idx, derivationTypeName)
	if handle == 0 {
		return nil
	}
	drv, _ := handle.Value().(*Derivation)
	return drv
}

// gcDerivation handles the __gc metamethod on derivations
// by releasing the [*derivation].
func gcDerivation(l *lua.State) (int, error) {
	const idx = 1
	handle, ok := testUserdataHandle(l, idx, derivationTypeName)
	if !ok {
		return 0, lua.NewTypeError(l, idx, derivationTypeName)
	}
	if handle == 0 {
		return 0, nil
	}
	handle.Delete()
	setUserdataHandle(l, idx, 0)
	return 0, nil
}

// indexDerivation handles the __index metamethod on derivations.
func indexDerivation(l *lua.State) (int, error) {
	if _, err := toDerivation(l); err != nil {
		return 0, err
	}
	l.UserValue(1, 1) // Push derivation argument table.
	l.PushValue(2)    // Copy key argument.
	if _, err := l.Table(-2, 0); err != nil {
		return 0, err
	}
	return 1, nil
}

// derivationPairs handles the __pairs metamethod on derivations.
func derivationPairs(l *lua.State) (int, error) {
	if _, err := toDerivation(l); err != nil {
		return 0, err
	}
	l.UserValue(1, 1) // Push derivation argument table.
	l.PushClosure(1, derivationPairNext)
	l.PushNil()
	l.PushNil()
	return 3, nil
}

// derivationPairNext is the iterator function returned by the derivation __pairs metamethod.
func derivationPairNext(l *lua.State) (int, error) {
	l.PushValue(2) // Move control value to top of stack.
	if !l.Next(lua.UpvalueIndex(1)) {
		l.PushNil()
		return 1, nil
	}
	return 2, nil
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
