// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package zb

import (
	"context"
	"fmt"
	"runtime/cgo"
	"strings"

	"zombiezen.com/go/nix"
	"zombiezen.com/go/zb/internal/lua"
	"zombiezen.com/go/zb/internal/sortedset"
)

const derivationTypeName = "derivation"

func registerDerivationMetatable(l *lua.State) {
	lua.NewMetatable(l, derivationTypeName)
	err := lua.SetFuncs(l, 0, map[string]lua.Function{
		"__index":     indexDerivation,
		"__pairs":     derivationPairs,
		"__gc":        gcDerivation,
		"__tostring":  derivationToString,
		"__concat":    concatDerivation,
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
	}

	// Configure outputs.
	var h nix.Hash
	switch typ := l.RawField(1, "outputHash"); typ {
	case lua.TypeNil:
	case lua.TypeString:
		s, _ := l.ToString(-1)
		var err error
		h, err = nix.ParseHash(s)
		if err != nil {
			return 0, fmt.Errorf("outputHash argument: %v", err)
		}
	default:
		return 0, fmt.Errorf("outputHash argument: %v expected, got %v", lua.TypeString, typ)
	}
	l.Pop(1)

	switch typ := l.RawField(1, "outputHashMode"); typ {
	case lua.TypeNil:
		if !h.IsZero() {
			drv.Outputs = map[string]*DerivationOutput{
				defaultDerivationOutputName: FixedCAOutput(nix.FlatFileContentAddress(h)),
			}
		}
	case lua.TypeString:
		switch mode, _ := l.ToString(-1); mode {
		case "flat":
			drv.Outputs = map[string]*DerivationOutput{
				defaultDerivationOutputName: FixedCAOutput(nix.FlatFileContentAddress(h)),
			}
		case "recursive":
			drv.Outputs = map[string]*DerivationOutput{
				defaultDerivationOutputName: FixedCAOutput(nix.RecursiveFileContentAddress(h)),
			}
		default:
			return 0, fmt.Errorf("outputHashMode argument: invalid mode %q", mode)
		}
	default:
		return 0, fmt.Errorf("outputHashMode argument: %v expected, got %v", lua.TypeString, typ)
	}
	l.Pop(1)

	if h.IsZero() {
		// TODO(someday): Multiple outputs.
		drv.Outputs = map[string]*DerivationOutput{
			defaultDerivationOutputName: RecursiveFileFloatingCAOutput(nix.SHA256),
		}
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

	for outputName, outType := range drv.Outputs {
		switch outType.typ {
		case floatingCAOutputType:
			drv.Env[outputName] = hashPlaceholder(outputName)
		case fixedCAOutputType:
			p, ok := outType.Path(eval.storeDir, drv.Name, outputName)
			if !ok {
				panic("should have a path")
			}
			drv.Env[outputName] = string(p)
		default:
			panic(outputName + " has an unhandled output type")
		}
	}
	drvPath, err := writeDerivation(context.TODO(), drv)
	if err != nil {
		return 0, fmt.Errorf("derivation: %v", err)
	}

	l.PushStringContext(string(drvPath), []string{string(drvPath)})
	if err := l.SetField(tableCopyIndex, "drvPath", 0); err != nil {
		return 0, fmt.Errorf("derivation: %v", err)
	}
	for outputName, outType := range drv.Outputs {
		var placeholder string
		switch outType.typ {
		case floatingCAOutputType:
			placeholder = unknownCAOutputPlaceholder(drvPath, defaultDerivationOutputName)
		case fixedCAOutputType:
			// TODO(someday): We already computed this earlier.
			p, ok := outType.Path(eval.storeDir, drv.Name, outputName)
			if !ok {
				panic("should have a path")
			}
			placeholder = string(p)
		}
		l.PushStringContext(placeholder, []string{
			"!" + outputName + "!" + string(drvPath),
		})
		if err := l.SetField(tableCopyIndex, outputName, 0); err != nil {
			return 0, fmt.Errorf("derivation: %v", err)
		}
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
			l.Pop(1)
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
			outputName, drvPath, ok := strings.Cut(rest, "!")
			if !ok {
				return "", fmt.Errorf("internal error: malformed context %q", dep)
			}
			if drv.InputDerivations == nil {
				drv.InputDerivations = make(map[nix.StorePath]*sortedset.Set[string])
			}
			if drv.InputDerivations[nix.StorePath(drvPath)] == nil {
				drv.InputDerivations[nix.StorePath(drvPath)] = new(sortedset.Set[string])
			}
			drv.InputDerivations[nix.StorePath(drvPath)].Add(outputName)
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

// derivationToString handles the __tostring metamethod on derivations.
func derivationToString(l *lua.State) (int, error) {
	if _, err := toDerivation(l); err != nil {
		return 0, err
	}
	l.UserValue(1, 1) // Push derivation argument table.
	if _, err := l.Field(-1, "out", 0); err != nil {
		return 0, err
	}
	return 1, nil
}

// concatDerivation handles the __concat metamethod on derivations.
func concatDerivation(l *lua.State) (int, error) {
	l.SetTop(2)
	if testDerivation(l, 1) != nil {
		l.UserValue(1, 1) // Push derivation argument table.
		if _, err := l.Field(-1, "out", 0); err != nil {
			return 0, err
		}
		l.Replace(1)
		l.Pop(1)
	}
	if testDerivation(l, 2) != nil {
		l.UserValue(2, 1) // Push derivation argument table.
		if _, err := l.Field(-1, "out", 0); err != nil {
			return 0, err
		}
		l.Replace(2)
		l.Pop(1)
	}
	if err := l.Concat(2, 0); err != nil {
		return 0, err
	}
	return 1, nil
}
