// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package mylua

import (
	"errors"
	"fmt"
	"strconv"
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
func CallMeta(l *State, obj int, event string) (bool, error) {
	obj = l.AbsIndex(obj)
	if Metafield(l, obj, event) == TypeNil {
		// No metafield.
		return false, nil
	}
	l.PushValue(obj)
	if err := l.Call(1, 1, 0); err != nil {
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
func ToString(l *State, idx int) (string, error) {
	idx = l.AbsIndex(idx)
	if hasMethod, err := CallMeta(l, idx, "__tostring"); err != nil {
		return "", err
	} else if hasMethod {
		if !l.IsString(-1) {
			l.Pop(1)
			return "", fmt.Errorf("lua: '__tostring' must return a string")
		}
		s, _ := l.ToString(idx)
		l.Pop(1)
		return s, nil
	}

	switch l.Type(idx) {
	case TypeNumber:
		if l.IsInteger(idx) {
			n, _ := l.ToInteger(idx)
			return strconv.FormatInt(n, 10), nil
		}
		n, _ := l.ToNumber(idx)
		return strconv.FormatFloat(n, 'g', -1, 64), nil
	case TypeString:
		s, _ := l.ToString(idx)
		return s, nil
	case TypeBoolean:
		if l.ToBoolean(idx) {
			return "true", nil
		} else {
			return "false", nil
		}
	case TypeNil:
		return "nil", nil
	default:
		var kind string
		if tt := Metafield(l, idx, "__name"); tt == TypeString {
			kind, _ = l.ToString(-1)
			l.Pop(1)
		} else {
			if tt != TypeNil {
				l.Pop(1)
			}
			kind = l.Type(idx).String()
		}
		return fmt.Sprintf("%s: %#x", kind, l.ID(idx)), nil
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
	l.RawSetField(-2, "__name") // metatable.__name = tname
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
func Len(l *State, idx int) (int64, error) {
	if err := l.Len(idx, 0); err != nil {
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
func NewLib(l *State, reg map[string]Function) error {
	l.CreateTable(0, len(reg))
	return SetFuncs(l, 0, reg)
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
func SetFuncs(l *State, nUp int, reg map[string]Function) error {
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
		if err := l.SetField(-(nUp + 2), name, 0); err != nil {
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
func Subtable(l *State, idx int, fname string) (bool, error) {
	tp, err := l.Field(idx, fname, 0)
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
	err = l.SetField(idx, fname, 0)
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
func Require(l *State, modName string, global bool, openf Function) error {
	if _, err := Subtable(l, RegistryIndex, LoadedTable); err != nil {
		return fmt.Errorf("lua: require %q: %w", modName, err)
	}
	if _, err := l.Field(-1, modName, 0); err != nil {
		return fmt.Errorf("lua: require %q: %w", modName, err)
	}
	if !l.ToBoolean(-1) {
		l.Pop(1) // remove field
		l.PushClosure(0, openf)
		l.PushString(modName)
		if err := l.Call(1, 1, 0); err != nil {
			return fmt.Errorf("lua: require %q: %w", modName, err)
		}
		l.PushValue(-1)
		if err := l.SetField(-3, modName, 0); err != nil {
			return fmt.Errorf("lua: require %q: %w", modName, err)
		}
	}
	l.Remove(-2) // remove LOADED table
	if global {
		l.PushValue(-1) // copy of module
		if err := l.SetGlobal(modName, 0); err != nil {
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
	if Metafield(l, arg, "__name") == TypeString {
		typeArg, _ = l.ToString(-1)
	} else if tp := l.Type(arg); tp == TypeLightUserdata {
		typeArg = "light userdata"
	} else {
		typeArg = tp.String()
	}
	return NewArgError(l, arg, fmt.Sprintf("%s expected, got %s", tname, typeArg))
}
