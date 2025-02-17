// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"zb.256lights.llc/pkg/internal/lualex"
)

// GName is the name of the global table.
const GName = "_G"

// BaseOptions is the parameter type for [NewOpenBase].
type BaseOptions struct {
	// The “print” function will write to Output (or [os.Stdout] if nil).
	Output io.Writer
	// Warner handles calls to the “warn” function.
	Warner Warner
	// If LoadFile is not nil,
	// then the “loadfile” function will be replaced by the given implementation
	// and the “dofile” function will use it to load files.
	LoadFile Function
}

// NewOpenBase returns a [Function] that loads the basic library.
// The resulting function is intended to be used as an argument to [Require].
//
// All functions in the basic library are pure (as per [*State.PushPureFunction]) except:
//
//   - print
//   - loadfile (if opts.LoadFile is not nil)
//   - dofile (if opts.LoadFile is not nil)
//   - warn (if opts.Warner is not nil)
func NewOpenBase(opts *BaseOptions) Function {
	if opts == nil {
		opts = new(BaseOptions)
	}
	return func(ctx context.Context, l *State) (int, error) {
		// Open library into global table.
		l.RawIndex(RegistryIndex, RegistryIndexGlobals)

		pureFuncs := map[string]Function{
			"assert":       baseAssert,
			"error":        baseError,
			"getmetatable": baseGetMetatable,
			"ipairs":       baseIPairs,
			"load":         baseLoad,
			"next":         baseNext,
			"pairs":        basePairs,
			"pcall":        basePCall,
			"rawequal":     baseRawEqual,
			"rawget":       baseRawGet,
			"rawlen":       baseRawLen,
			"rawset":       baseRawSet,
			"select":       baseSelect,
			"setmetatable": baseSetMetatable,
			"tonumber":     baseToNumber,
			"tostring":     baseToString,
			"type":         baseType,
			"xpcall":       baseXPCall,
		}
		impureFuncs := map[string]Function{
			"print": newBasePrint(opts.Output),
		}

		if opts.LoadFile == nil {
			pureFuncs["loadfile"] = baseLoadfile
			pureFuncs["dofile"] = newBaseDofile(baseLoadfile)
		} else {
			impureFuncs["loadfile"] = opts.LoadFile
			impureFuncs["dofile"] = newBaseDofile(opts.LoadFile)
		}

		if opts.Warner == nil {
			pureFuncs["warn"] = newBaseWarn(nil)
		} else {
			impureFuncs["warn"] = newBaseWarn(opts.Warner)
		}

		if err := SetPureFunctions(ctx, l, 0, pureFuncs); err != nil {
			return 0, err
		}
		if err := SetFunctions(ctx, l, 0, impureFuncs); err != nil {
			return 0, err
		}

		// Set global _G.
		l.PushValue(-1)
		if err := l.SetField(ctx, -2, GName); err != nil {
			return 0, err
		}

		// Set global _VERSION.
		l.PushString(Version)
		if err := l.SetField(ctx, -2, "_VERSION"); err != nil {
			return 0, err
		}

		return 1, nil
	}
}

func baseAssert(ctx context.Context, l *State) (int, error) {
	if !l.ToBoolean(1) {
		if l.Type(1) == TypeNone {
			return 0, NewArgError(l, 1, "value expected")
		}
		l.Remove(1)
		l.PushString("assertion failed!") // default message
		l.SetTop(1)
		return baseError(ctx, l)
	}
	return l.Top(), nil
}

func newBaseDofile(loadfile Function) Function {
	return func(ctx context.Context, l *State) (int, error) {
		if tp := l.Type(1); tp != TypeNone && tp != TypeNil && tp != TypeString {
			return 0, NewTypeError(l, 1, TypeString.String())
		}
		l.SetTop(1)

		// loadfile(filename)
		l.PushClosure(0, loadfile)
		l.Insert(1)
		if err := l.Call(ctx, 1, 2); err != nil {
			return 0, err
		}
		if l.IsNil(-2) {
			msg, _, _ := ToString(ctx, l, -1)
			return 0, fmt.Errorf("dofile: %s", msg)
		}
		l.Pop(1)

		// Call the loaded function.
		if err := l.Call(ctx, 0, MultipleReturns); err != nil {
			return 0, err
		}
		return l.Top(), nil
	}
}

func baseError(ctx context.Context, l *State) (int, error) {
	level := int64(1)
	if !l.IsNoneOrNil(2) {
		var err error
		level, err = CheckInteger(l, 2)
		if err != nil {
			return 0, err
		}
	}
	l.SetTop(1)

	if l.Type(1) == TypeString && level > 0 {
		l.PushString(Where(l, int(level)))
		l.PushValue(1)
		if err := l.Concat(ctx, 2); err != nil {
			return 0, err
		}
	}
	// TODO(someday): Return error object from top of stack.
	msg, _ := l.ToString(-1)
	return 0, errors.New(msg)
}

func baseGetMetatable(ctx context.Context, l *State) (int, error) {
	if l.Type(1) == TypeNone {
		return 0, NewArgError(l, 1, "value expected")
	}
	if !l.Metatable(1) {
		l.PushNil()
		return 1, nil
	}
	Metafield(l, 1, "__metatable")
	return 1, nil
}

func baseSetMetatable(ctx context.Context, l *State) (int, error) {
	if got, want := l.Type(1), TypeTable; got != want {
		return 0, NewTypeError(l, 1, want.String())
	}
	if got := l.Type(2); got != TypeNil && got != TypeTable {
		return 0, NewTypeError(l, 2, "nil or table")
	}

	if Metafield(l, 1, "__metatable") != TypeNil {
		return 0, fmt.Errorf("%scannot change a protected metatable", Where(l, 1))
	}
	l.SetTop(2)
	if err := l.SetMetatable(1); err != nil {
		return 0, fmt.Errorf("%s%w", Where(l, 1), err)
	}
	return 1, nil
}

func baseIPairs(ctx context.Context, l *State) (int, error) {
	if l.Type(1) == TypeNone {
		return 0, NewArgError(l, 1, "value expected")
	}

	f := Function(func(ctx context.Context, l *State) (int, error) {
		i, err := CheckInteger(l, 2)
		if err != nil {
			return 0, err
		}
		i++
		l.PushInteger(i)
		if tp, err := l.Index(ctx, 1, i); err != nil {
			return 0, err
		} else if tp == TypeNil {
			return 1, nil
		}
		return 2, nil
	})

	l.PushPureFunction(0, f)
	l.PushValue(1)
	l.PushInteger(0)
	return 3, nil
}

func baseLoad(ctx context.Context, l *State) (int, error) {
	chunk, chunkIsString := l.ToString(1)
	var source Source
	hasSource := !l.IsNoneOrNil(2)
	if hasSource {
		sourceString, err := CheckString(l, 2)
		if err != nil {
			return 0, err
		}
		source = Source(sourceString)
	}
	mode := "bt"
	if !l.IsNoneOrNil(3) {
		var err error
		mode, err = CheckString(l, 3)
		if err != nil {
			return 0, err
		}
	}
	hasEnv := !l.IsNone(4)

	var r io.ByteScanner
	if chunkIsString {
		r = strings.NewReader(chunk)
		if !hasSource {
			source = LiteralSource(chunk)
		}
	} else if tp := l.Type(1); tp == TypeFunction {
		r = newLuaFunctionReader(ctx, l, 1)
		if !hasSource {
			source = AbstractSource("(load)")
		}
	} else {
		return 0, NewTypeError(l, 1, TypeFunction.String())
	}

	if err := l.Load(r, source, mode); err != nil {
		l.PushNil()
		l.PushString(err.Error())
		return 2, nil
	}
	if hasEnv {
		l.PushValue(4)
		if _, err := l.SetUpvalue(-2, 1); err != nil {
			l.Pop(1)
		}
	}
	return 1, nil
}

func baseLoadfile(ctx context.Context, l *State) (int, error) {
	var fname string
	if !l.IsNoneOrNil(1) {
		var err error
		fname, err = CheckString(l, 1)
		if err != nil {
			return 0, err
		}
	}
	var mode string
	if !l.IsNoneOrNil(2) {
		var err error
		fname, err = CheckString(l, 2)
		if err != nil {
			return 0, err
		}
	}
	hasEnv := !l.IsNone(3)

	if err := doLoadfile(l, fname, mode); err != nil {
		l.PushNil()
		l.PushString(err.Error())
		return 2, nil
	}
	if hasEnv {
		l.PushValue(3)
		if _, err := l.SetUpvalue(-2, 1); err != nil {
			l.Pop(1)
		}
	}
	return 1, nil
}

func doLoadfile(l *State, filename string, mode string) error {
	var source Source
	var r io.Reader
	if filename == "" {
		source = AbstractSource("stdin")
		r = os.Stdin
	} else {
		source = FilenameSource(filename)
		f, err := os.Open(filename)
		if err != nil {
			return err
		}
		r = f
		defer f.Close()
	}

	br := bufio.NewReader(r)
	skipFileComment(br)
	return l.Load(br, source, mode)
}

func skipFileComment(br *bufio.Reader) {
	bom := []byte{0xef, 0xbb, 0xbf}
	commentStart := []byte{'#'}

	buf, _ := br.Peek(len(bom) + len(commentStart))
	discard := 0
	if bytes.HasPrefix(buf, bom) {
		discard += len(bom)
		buf = buf[len(bom):]
	}
	if !bytes.HasPrefix(buf, commentStart) {
		br.Discard(discard)
		return
	}
	discard += len(commentStart)
	br.Discard(discard)
	for {
		b, err := br.ReadByte()
		if b == '\n' || err != nil {
			break
		}
	}
}

func baseNext(ctx context.Context, l *State) (int, error) {
	if got, want := l.Type(1), TypeTable; got != want {
		return 0, NewTypeError(l, 1, want.String())
	}
	l.SetTop(2)
	if !l.Next(1) {
		l.PushNil()
		return 1, nil
	}
	return 2, nil
}

func basePairs(ctx context.Context, l *State) (int, error) {
	if l.IsNone(1) {
		return 0, NewArgError(l, 1, "value expected")
	}
	if Metafield(l, 1, "__pairs") != TypeNil {
		l.PushValue(1) // self for metamethod
		if err := l.Call(ctx, 1, 3); err != nil {
			return 0, err
		}
		return 3, nil
	}
	l.PushPureFunction(0, baseNext)
	l.PushValue(1)
	l.PushNil()
	return 3, nil
}

func basePCall(ctx context.Context, l *State) (int, error) {
	if l.IsNone(1) {
		return 0, NewArgError(l, 1, "value expected")
	}

	// First result if no errors.
	l.PushBoolean(true)
	l.Insert(1)

	if err := l.PCall(ctx, l.Top()-2, MultipleReturns, 0); err != nil {
		l.PushBoolean(false)
		// TODO(someday): Push error object from err.
		l.PushString(err.Error())
		return 2, nil
	}
	return l.Top(), nil
}

func baseXPCall(ctx context.Context, l *State) (int, error) {
	if got, want := l.Type(2), TypeFunction; got != want {
		return 0, NewTypeError(l, 2, want.String())
	}

	// Stack layout at this point:
	//
	// 1: function
	// 2: message handler
	// 3 → top: arguments
	numArgs := l.Top() - 2

	// Stack layout after these calls:
	//
	// 1: message handler
	// 2: true
	// 3: function
	// 4 → top: arguments
	l.PushBoolean(true)
	l.Rotate(3, 1)

	if err := l.PCall(ctx, numArgs, MultipleReturns, 1); err != nil {
		l.PushBoolean(false)
		// TODO(someday): Push error object from err.
		l.PushString(err.Error())
		return 2, nil
	}
	return l.Top() - 1, nil
}

func newBasePrint(out io.Writer) Function {
	if out == nil {
		out = os.Stdout
	}
	return func(ctx context.Context, l *State) (int, error) {
		n := l.Top()
		for i := 1; i <= n; i++ {
			s, _, err := ToString(ctx, l, i)
			if err != nil {
				return 0, err
			}
			if i > 1 {
				io.WriteString(out, "\t")
			}
			io.WriteString(out, s)
		}
		io.WriteString(out, "\n")
		return 0, nil
	}
}

func baseRawEqual(ctx context.Context, l *State) (int, error) {
	if l.IsNone(1) {
		return 0, NewArgError(l, 1, "value expected")
	}
	if l.IsNone(2) {
		return 0, NewArgError(l, 2, "value expected")
	}
	l.PushBoolean(l.RawEqual(1, 2))
	return 1, nil
}

func baseRawLen(ctx context.Context, l *State) (int, error) {
	if tp := l.Type(1); tp != TypeTable && tp != TypeString {
		return 0, NewTypeError(l, 1, "table or string")
	}
	l.PushInteger(int64(l.RawLen(1)))
	return 1, nil
}

func baseRawGet(ctx context.Context, l *State) (int, error) {
	if got, want := l.Type(1), TypeTable; got != want {
		return 0, NewTypeError(l, 1, want.String())
	}
	if l.IsNone(2) {
		return 0, NewArgError(l, 2, "value expected")
	}
	l.SetTop(2)
	l.RawGet(1)
	return 1, nil
}

func baseRawSet(ctx context.Context, l *State) (int, error) {
	if got, want := l.Type(1), TypeTable; got != want {
		return 0, NewTypeError(l, 1, want.String())
	}
	if l.IsNone(2) {
		return 0, NewArgError(l, 2, "value expected")
	}
	if l.IsNone(3) {
		return 0, NewArgError(l, 3, "value expected")
	}
	l.SetTop(3)
	if err := l.RawSet(1); err != nil {
		return 0, err
	}
	return 1, nil
}

func baseSelect(ctx context.Context, l *State) (int, error) {
	n := int64(l.Top())
	if l.Type(1) == TypeString {
		if s, _ := l.ToString(1); s == "#" {
			l.PushInteger(n - 1)
			return 1, nil
		}
	}
	i, err := CheckInteger(l, 1)
	if err != nil {
		return 0, err
	}
	if i < 0 {
		i = n + i
	} else if i > n {
		i = n
	}
	if i < 1 {
		return 0, NewArgError(l, 1, "index out of range")
	}
	return int(n - i), nil
}

func baseToNumber(ctx context.Context, l *State) (int, error) {
	if !l.IsNoneOrNil(2) {
		// Parse integer by given base.
		base, err := CheckInteger(l, 2)
		if err != nil {
			return 0, err
		}
		if got, want := l.Type(1), TypeString; got != want {
			return 0, NewTypeError(l, 1, want.String())
		}
		s, _ := l.ToString(1)
		if !(2 <= base && base <= 36) {
			return 0, NewArgError(l, 2, "base out of range")
		}
		result, err := strconv.ParseInt(strings.TrimSpace(s), int(base), 64)
		if err != nil {
			l.PushNil()
			return 1, nil
		}
		l.PushInteger(result)
		return 1, nil
	}

	if l.Type(1) == TypeNumber {
		l.SetTop(1)
		return 1, nil
	}

	if s, ok := l.ToString(1); ok {
		if i, err := lualex.ParseInt(s); err == nil {
			l.PushInteger(i)
			return 1, nil
		}
		n, err := lualex.ParseNumber(s)
		if err != nil {
			l.PushNil()
			return 1, nil
		}
		l.PushNumber(n)
		return 1, nil
	}

	if l.IsNone(1) {
		return 0, NewArgError(l, 1, "value expected")
	}

	l.PushNil()
	return 1, nil
}

func baseToString(ctx context.Context, l *State) (int, error) {
	if l.IsNone(1) {
		return 0, NewArgError(l, 1, "value expected")
	}
	s, sctx, err := ToString(ctx, l, 1)
	if err != nil {
		return 0, err
	}
	l.PushStringContext(s, sctx)
	return 1, nil
}

func baseType(ctx context.Context, l *State) (int, error) {
	tp := l.Type(1)
	if tp == TypeNone {
		return 0, NewArgError(l, 1, "value expected")
	}
	l.PushString(tp.String())
	return 1, nil
}

// A Warner handles warnings from the basic “warn” Lua function.
//
// Warn handles a single warning argument.
// toBeContinued is true if there are more arguments to this call to “warn”.
type Warner interface {
	Warn(msg string, toBeContinued bool)
}

// WarnFunc is a function that implements [Warner].
type WarnFunc func(msg string, toBeContinued bool)

// Warn calls the function to implement [Warner].
func (f WarnFunc) Warn(msg string, toBeContinued bool) {
	f(msg, toBeContinued)
}

func newBaseWarn(w Warner) Function {
	return func(ctx context.Context, l *State) (int, error) {
		n := l.Top()
		for i := range max(n, 1) { // At least one argument.
			if _, err := CheckString(l, i+1); err != nil {
				return 0, err
			}
		}
		if w != nil {
			for i := range n {
				s, _ := l.ToString(i + 1)
				w.Warn(s, i < n-1)
			}
		}
		return 0, nil
	}
}

type luaFunctionReader struct {
	ctx       context.Context
	l         *State
	funcIndex int

	s   string
	i   int
	err error
}

func newLuaFunctionReader(ctx context.Context, l *State, i int) *luaFunctionReader {
	return &luaFunctionReader{
		ctx:       ctx,
		l:         l,
		funcIndex: i,
	}
}

func (r *luaFunctionReader) ReadByte() (byte, error) {
	if r.i < len(r.s) {
		b := r.s[r.i]
		r.i++
		return b, nil
	}
	if r.err != nil {
		return 0, r.err
	}

	if !r.l.CheckStack(2) {
		return 0, fmt.Errorf("%sreader function must return a string", Where(r.l, 1))
	}
	r.s, r.i = "", 0 // Prevent unreading.
	r.l.PushValue(1)
	r.err = r.l.Call(r.ctx, 0, 1)
	if r.err != nil {
		return 0, r.err
	}
	if r.l.IsNil(-1) {
		r.l.Pop(1)
		r.err = io.EOF
		return 0, r.err
	}
	if !r.l.IsString(-1) {
		r.l.Pop(1)
		r.err = fmt.Errorf("%sreader function must return a string", Where(r.l, 1))
		return 0, r.err
	}
	r.s, _ = r.l.ToString(-1)
	r.l.Pop(1)
	if len(r.s) == 0 {
		r.err = io.EOF
		return 0, r.err
	}
	r.i = 1
	return r.s[0], nil
}

func (r *luaFunctionReader) UnreadByte() error {
	if r.i <= 0 {
		return errors.New("cannot unread past beginning of last string returned")
	}
	r.i--
	return nil
}
