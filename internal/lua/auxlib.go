// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"zb.256lights.llc/pkg/internal/luacode"
	"zb.256lights.llc/pkg/sets"
)

// LoadedTable is the key in the registry for table of loaded modules.
const LoadedTable = "_LOADED"

// Metafield pushes onto the stack the field event
// from the metatable of the object at index obj
// and returns the type of the pushed value.
// If the object does not have a metatable,
// or if the metatable does not have this field,
// pushes nothing and returns [TypeNil].
func Metafield(l *State, obj int, event string) Type {
	if !l.Metatable(obj) {
		return TypeNil
	}
	tt := l.RawField(-1, event)
	if tt == TypeNil {
		l.Pop(2) // remove metatable and metafield
	} else {
		l.Remove(-2) // remove only metatable
	}
	return tt
}

// CallMeta calls a metamethod.
//
// If the object at index obj has a metatable and this metatable has a field event,
// this function calls this field passing the object as its only argument.
// In this case this function returns true
// and pushes onto the stack the value returned by the call.
// If an error is raised during the call,
// CallMeta returns an error without pushing any value on the stack.
// If there is no metatable or no metamethod,
// this function returns false without pushing any value on the stack.
func CallMeta(ctx context.Context, l *State, obj int, event string) (bool, error) {
	obj = l.AbsIndex(obj)
	if Metafield(l, obj, event) == TypeNil {
		// No metafield.
		return false, nil
	}
	l.PushValue(obj)
	if err := l.Call(ctx, 1, 1); err != nil {
		return true, fmt.Errorf("lua: call metafield %q: %w", event, err)
	}
	return true, nil
}

// ToString converts any Lua value at the given index
// to a Go string in a reasonable format.
//
// If the value has a metatable with a __tostring field,
// then ToString calls the corresponding metamethod with the value as argument,
// and uses the result of the call as its result.
func ToString(ctx context.Context, l *State, idx int) (string, sets.Set[string], error) {
	idx = l.AbsIndex(idx)
	if hasMethod, err := CallMeta(ctx, l, idx, "__tostring"); err != nil {
		return "", nil, err
	} else if hasMethod {
		if !l.IsString(-1) {
			l.Pop(1)
			return "", nil, fmt.Errorf("lua: '__tostring' must return a string")
		}
		s, _ := l.ToString(-1)
		sctx := l.StringContext(-1)
		l.Pop(1)
		return s, sctx, nil
	}

	switch l.Type(idx) {
	case TypeNumber:
		var v luacode.Value
		if l.IsInteger(idx) {
			n, _ := l.ToInteger(idx)
			v = luacode.IntegerValue(n)
		} else {
			n, _ := l.ToNumber(idx)
			v = luacode.FloatValue(n)
		}
		s, _ := v.Unquoted()
		return s, nil, nil
	case TypeString:
		s, _ := l.ToString(idx)
		return s, l.StringContext(idx), nil
	case TypeBoolean, TypeNil:
		k, _ := ToConstant(l, idx)
		return k.String(), nil, nil
	default:
		var kind string
		var sctx sets.Set[string]
		if tt := Metafield(l, idx, typeNameMetafield); tt == TypeString {
			kind, _ = l.ToString(-1)
			sctx = l.StringContext(-1)
			l.Pop(1)
		} else {
			if tt != TypeNil {
				l.Pop(1)
			}
			kind = l.Type(idx).String()
		}
		return formatObject(kind, l.ID(idx)), sctx, nil
	}
}

func formatObject(kind string, id uint64) string {
	return fmt.Sprintf("%s: %#x", kind, id)
}

// ToConstant converts the nil, boolean, number, or string at the given index
// to a [luacode.Value].
func ToConstant(l *State, idx int) (_ luacode.Value, ok bool) {
	switch l.Type(idx) {
	case TypeNumber:
		if i, ok := l.ToInteger(idx); ok {
			return luacode.IntegerValue(i), true
		}
		n, _ := l.ToNumber(idx)
		return luacode.FloatValue(n), true
	case TypeString:
		s, _ := l.ToString(idx)
		return luacode.StringValue(s), true
	case TypeBoolean:
		return luacode.BoolValue(l.ToBoolean(idx)), true
	case TypeNil:
		return luacode.Value{}, true
	default:
		return luacode.Value{}, false
	}
}

// CheckString checks whether the function argument arg is a string
// and returns this string.
// This function uses [State.ToString] to get its result,
// so all conversions and caveats of that function apply here.
func CheckString(l *State, arg int) (string, error) {
	s, ok := l.ToString(arg)
	if !ok {
		return "", NewTypeError(l, arg, TypeString.String())
	}
	return s, nil
}

// CheckInteger checks whether the function argument arg is an integer
// (or can be converted to an integer)
// and returns this integer.
func CheckInteger(l *State, arg int) (int64, error) {
	d, ok := l.ToInteger(arg)
	if !ok {
		if l.IsNumber(arg) {
			return 0, NewArgError(l, arg, "number has no integer representation")
		}
		return 0, NewTypeError(l, arg, TypeNumber.String())
	}
	return d, nil
}

// CheckNumber checks whether the function argument arg is a number
// and returns this number.
func CheckNumber(l *State, arg int) (float64, error) {
	d, ok := l.ToNumber(arg)
	if !ok {
		return 0, NewTypeError(l, arg, TypeNumber.String())
	}
	return d, nil
}

// NewMetatable gets or creates a table in the registry
// to be used as a metatable for userdata.
// If the table is created, adds the pair __name = tname,
// and returns true.
// Regardless, the function pushes onto the stack
// the final value associated with tname in the registry.
func NewMetatable(l *State, tname string) bool {
	if Metatable(l, tname) != TypeNil {
		// Name already in use.
		return false
	}
	l.Pop(1)
	l.CreateTable(0, 2)
	l.PushString(tname)
	l.RawSetField(-2, typeNameMetafield) // metatable.__name = tname
	l.PushValue(-1)
	l.RawSetField(RegistryIndex, tname)
	return true
}

// Metatable pushes onto the stack the metatable associated with the name tname
// in the registry (see [NewMetatable]),
// or nil if there is no metatable associated with that name.
// Returns the type of the pushed value.
func Metatable(l *State, tname string) Type {
	return l.RawField(RegistryIndex, tname)
}

// SetMetatable sets the metatable of the object on the top of the stack
// as the metatable associated with name tname in the registry.
// [NewMetatable] can be used to create such a metatable.
func SetMetatable(l *State, tname string) {
	Metatable(l, tname)
	l.SetMetatable(-2)
}

// TestUserdata returns a copy of the Go value
// for the userdata at the given index.
// isUserdata is true if and only if the value at the given index
// is a userdata and has the type tname (see [NewMetatable]).
func TestUserdata(l *State, idx int, tname string) (_ any, isUserdata bool) {
	ud, isUserdata := l.ToUserdata(idx)
	if !isUserdata {
		return nil, false
	}
	if !l.Metatable(idx) {
		return nil, false
	}
	Metatable(l, tname)
	metatableMatch := l.RawEqual(-1, -2)
	l.Pop(2)
	if !metatableMatch {
		return nil, false
	}
	return ud, true
}

// CheckUserdata returns a copy of the Go value
// for the given userdata argument.
// CheckUserdata returns an error if the function argument arg
// is not a userdata of the type tname (see [NewMetatable]).
func CheckUserdata(l *State, arg int, tname string) (any, error) {
	data, ok := TestUserdata(l, arg, tname)
	if !ok {
		return nil, NewTypeError(l, arg, tname)
	}
	return data, nil
}

// Where returns a string identifying the current position of the control
// at the given level in the call stack.
// Typically this string has the following format (including a trailing space):
//
//	chunkname:currentline:
//
// Level 0 is the running function,
// level 1 is the function that called the running function, etc.
//
// This function is used to build a prefix for error messages.
func Where(l *State, level int) string {
	ar := l.Info(level)
	if ar == nil || ar.CurrentLine <= 0 {
		return ""
	}
	return fmt.Sprintf("%v:%d: ", ar.Source, ar.CurrentLine)
}

// Len returns the "length" of the value at the given index as an integer.
func Len(ctx context.Context, l *State, idx int) (int64, error) {
	if err := l.Len(ctx, idx); err != nil {
		l.Pop(1)
		return 0, err
	}
	n, ok := l.ToInteger(-1)
	l.Pop(1)
	if !ok {
		return 0, fmt.Errorf("lua: length: not an integer")
	}
	return n, nil
}

// NewLib creates a new table and registers there the functions in the map reg.
func NewLib(l *State, reg map[string]Function) {
	l.CreateTable(0, len(reg))
	err := setFuncs(l, 0, reg, func(l *State, idx int, k string) error {
		l.RawSetField(idx, k)
		return nil
	})
	if err != nil {
		panic(err)
	}
}

// SetFuncs registers all functions the map reg
// into the table on the top of the stack
// (below optional upvalues, see next).
// Any nils are registered as false.
//
// When nUp is not zero, all functions are created with nup upvalues,
// initialized with copies of the nUp values previously pushed on the stack
// on top of the library table.
// These values are popped from the stack after the registration.
func SetFuncs(ctx context.Context, l *State, nUp int, reg map[string]Function) error {
	return setFuncs(l, 0, reg, func(l *State, idx int, k string) error {
		return l.SetField(ctx, idx, k)
	})
}

func setFuncs(l *State, nUp int, reg map[string]Function, setField func(l *State, idx int, k string) error) error {
	if !l.CheckStack(nUp) {
		l.Pop(nUp)
		return errors.New("too many upvalues")
	}
	for name, f := range reg {
		if f == nil {
			l.PushBoolean(false)
		} else {
			for i := 0; i < nUp; i++ {
				l.PushValue(-nUp)
			}
			l.PushClosure(nUp, f)
		}
		if err := setField(l, -(nUp + 2), name); err != nil {
			l.Pop(nUp)
			return err
		}
	}
	l.Pop(nUp)
	return nil
}

// Subtable ensures that the value t[fname],
// where t is the value at index idx, is a table,
// and pushes that table onto the stack.
// Returns true if it finds a previous table there
// and false if it creates a new table.
func Subtable(ctx context.Context, l *State, idx int, fname string) (bool, error) {
	tp, err := l.Field(ctx, idx, fname)
	if err != nil {
		return false, err
	}
	if tp == TypeTable {
		return true, nil
	}
	l.Pop(1)
	idx = l.AbsIndex(idx)
	l.CreateTable(0, 0)
	l.PushValue(-1) // copy to be left at top
	err = l.SetField(ctx, idx, fname)
	if err != nil {
		l.Pop(1) // pop table
		return false, err
	}
	return false, nil
}

// Require loads a module using the given openf function.
// If package.loaded[modName] is not true,
// Require calls the function with the string modName as an argument
// and sets the call result to package.loaded[modName],
// as if that function has been called through require.
// If global is true, also stores the module into the global modName.
// Leaves a copy of the module on the stack.
func Require(ctx context.Context, l *State, modName string, global bool, openf Function) error {
	if _, err := Subtable(ctx, l, RegistryIndex, LoadedTable); err != nil {
		return fmt.Errorf("lua: require %q: %w", modName, err)
	}
	if _, err := l.Field(ctx, -1, modName); err != nil {
		return fmt.Errorf("lua: require %q: %w", modName, err)
	}
	if !l.ToBoolean(-1) {
		l.Pop(1) // remove field
		l.PushClosure(0, openf)
		l.PushString(modName)
		if err := l.Call(ctx, 1, 1); err != nil {
			return fmt.Errorf("lua: require %q: %w", modName, err)
		}
		l.PushValue(-1)
		if err := l.SetField(ctx, -3, modName); err != nil {
			return fmt.Errorf("lua: require %q: %w", modName, err)
		}
	}
	l.Remove(-2) // remove LOADED table
	if global {
		l.PushValue(-1) // copy of module
		if err := l.SetGlobal(ctx, modName); err != nil {
			return fmt.Errorf("lua: require %q: %w", modName, err)
		}
	}
	return nil
}

// NewArgError returns a new error reporting a problem with argument arg
// of the Go function that called it,
// using a standard message that includes msg as a comment.
func NewArgError(l *State, arg int, msg string) error {
	ar := l.Info(0)
	if ar == nil {
		// No stack frame.
		return fmt.Errorf("%sbad argument #%d (%s)", Where(l, 1), arg, msg)
	}
	if ar.NameWhat == "method" {
		arg-- // do not count 'self'
		if arg == 0 {
			// Error is in the self argument itself.
			return fmt.Errorf("%scalling '%s' on bad self (%s)", Where(l, 1), ar.Name, msg)
		}
	}
	if ar.Name == "" {
		// TODO(someday): Find global function.
		ar.Name = "?"
	}
	return fmt.Errorf("%sbad argument #%d to '%s' (%s)", Where(l, 1), arg, ar.Name, msg)
}

// NewTypeError returns a new type error for the argument arg
// of the Go function that called it, using a standard message;
// tname is a "name" for the expected type.
func NewTypeError(l *State, arg int, tname string) error {
	var typeArg string
	if Metafield(l, arg, typeNameMetafield) == TypeString {
		typeArg, _ = l.ToString(-1)
	} else if tp := l.Type(arg); tp == TypeLightUserdata {
		typeArg = "light userdata"
	} else {
		typeArg = tp.String()
	}
	return NewArgError(l, arg, fmt.Sprintf("%s expected, got %s", tname, typeArg))
}

// OpenLibraries opens all standard Lua libraries into the given state
// with their default settings.
func OpenLibraries(ctx context.Context, l *State) error {
	libs := []struct {
		name  string
		openf Function
	}{
		{GName, NewOpenBase(nil)},
		{TableLibraryName, OpenTable},
		{StringLibraryName, OpenString},
		{MathLibraryName, NewOpenMath(nil)},
		{UTF8LibraryName, OpenUTF8},
		// {IOLibraryName, NewIOLibrary().OpenLibrary},
		// {OSLibraryName, NewOSLibrary().OpenLibrary},
		// {DebugLibraryName, OpenDebug},
		// {PackageLibraryName, OpenPackage},
	}

	for _, lib := range libs {
		if err := Require(ctx, l, lib.name, true, lib.openf); err != nil {
			return err
		}
		l.Pop(1)
	}

	return nil
}

// Traceback creates a traceback of the call stack starting at the given level.
// Level 0 is the current running function,
// whereas level n+1 is the function that has called level n
// (except for tail calls, which do not count in the stack).
// msg is prepended to the traceback.
func Traceback(l *State, msg string, level int) string {
	const levels1 = 10
	const levels2 = 11

	last := lastLevel(l)
	limitToShow := -1
	if last-level > levels1+levels2 {
		limitToShow = levels1
	}

	sb := new(strings.Builder)
	if msg != "" {
		sb.WriteString(msg)
		sb.WriteByte('\n')
	}
	sb.WriteString("stack traceback:")
	for {
		ar := l.Info(level)
		if ar == nil {
			break
		}
		level++

		if limitToShow == 0 {
			n := last - level - levels2 + 1
			fmt.Fprintf(sb, "\n\t...\t(skipping %d levels)", n)
			level += n
		} else if limitToShow > 0 {
			limitToShow--
		}

		if ar.CurrentLine > 0 {
			fmt.Fprintf(sb, "\n\t%v:%d: in ", ar.Source, ar.CurrentLine)
		} else {
			fmt.Fprintf(sb, "\n\t%v: in ", ar.Source)
		}
		switch name, _ := globalFunctionName(l, level-1); {
		case name != "":
			sb.WriteString("function '")
			sb.WriteString(name)
			sb.WriteString("'")
		case ar.NameWhat != "":
			sb.WriteString(ar.NameWhat)
			sb.WriteString(" '")
			sb.WriteString(ar.Name)
			sb.WriteString("'")
		case ar.What == "main":
			sb.WriteString("main chunk")
		case ar.What == "Lua":
			fmt.Fprintf(sb, "function <%s:%d>", ar.Source, ar.LineDefined)
		default:
			sb.WriteString("?")
		}
		if ar.IsTailCall {
			sb.WriteString("\n\t(...tail calls...)")
		}
	}
	return sb.String()
}

func globalFunctionName(l *State, level int) (string, error) {
	defer l.SetTop(l.Top())

	if !l.FunctionForLevel(level) {
		return "", nil
	}
	funcIndex := l.Top()
	l.RawField(RegistryIndex, LoadedTable)
	if !l.CheckStack(6) {
		return "", errStackOverflow
	}
	name, ok := fieldNameOfObject(l, funcIndex, 2)
	if !ok {
		return "", nil
	}
	return strings.TrimPrefix(name, GName+"."), nil
}

// fieldNameOfObject searches a table recursively (up to depth levels)
// to see if it has a value that equals the value at objIndex,
// and if so, it returns the keys used to get to that value separated by dots.
func fieldNameOfObject(l *State, objIndex int, depth int) (string, bool) {
	if depth <= 0 || !l.IsTable(-1) {
		return "", false
	}

	defer l.SetTop(l.Top())
	l.PushNil()
	for l.Next(-2) {
		if l.Type(-2) != TypeString {
			// Ignore non-string keys.
			l.Pop(1)
			continue
		}

		if l.RawEqual(objIndex, -1) {
			key, _ := l.ToString(-2)
			return key, true
		}

		if subkey, found := fieldNameOfObject(l, objIndex, depth-1); found {
			key, _ := l.ToString(-2)
			return key + "." + subkey, true
		}
		l.Pop(1)
	}

	return "", false
}

func lastLevel(l *State) int {
	lowerLimit, upperLimit := 1, 1
	for l.Info(upperLimit) != nil {
		lowerLimit = upperLimit
		upperLimit *= 2
	}
	i := sort.Search(upperLimit-lowerLimit, func(i int) bool {
		return l.Info(lowerLimit+i) == nil
	})
	return lowerLimit + i
}
