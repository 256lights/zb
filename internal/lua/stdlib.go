// Copyright 2023 Ross Light
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of
// this software and associated documentation files (the “Software”), to deal in
// the Software without restriction, including without limitation the rights to
// use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
// the Software, and to permit persons to whom the Software is furnished to do so,
// subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED “AS IS”, WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
// FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
// COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
// IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
// CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
//
// SPDX-License-Identifier: MIT

package lua

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"os"

	"zombiezen.com/go/zb/internal/lua54"
)

// OpenLibraries opens all standard Lua libraries into the given state
// with their default settings.
func OpenLibraries(l *State) error {
	libs := []struct {
		name  string
		openf Function
	}{
		{GName, NewOpenBase(nil, nil)},
		{CoroutineLibraryName, OpenCoroutine},
		{TableLibraryName, OpenTable},
		{IOLibraryName, NewIOLibrary().OpenLibrary},
		{OSLibraryName, NewOSLibrary().OpenLibrary},
		{StringLibraryName, OpenString},
		{UTF8LibraryName, OpenUTF8},
		{MathLibraryName, NewOpenMath(nil)},
		{DebugLibraryName, OpenDebug},
		{PackageLibraryName, OpenPackage},
	}

	for _, lib := range libs {
		if err := Require(l, lib.name, true, lib.openf); err != nil {
			return err
		}
		l.Pop(1)
	}

	return nil
}

// NewOpenBase returns a [Function] that loads the basic library.
// The print function will write to the given out writer (or os.Stdout if nil).
// If loadfile is not nil, then loadfile will be replaced by the given implementation
// and dofile will use it to load files.
// The resulting function is intended to be used as an argument to [Require].
func NewOpenBase(out io.Writer, loadfile Function) Function {
	if out == nil {
		out = os.Stdout
	}
	return func(l *State) (int, error) {
		// Call stock luaopen_base.
		nArgs := l.Top()
		lua54.PushOpenBase(&l.state)
		l.Rotate(1, 1)
		if err := l.Call(nArgs, 1, 0); err != nil {
			return 0, err
		}

		// Override print function.
		l.PushClosure(0, func(l *State) (int, error) {
			n := l.Top()
			for i := 1; i <= n; i++ {
				s, err := ToString(l, i)
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
		})
		l.RawSetField(-2, "print")

		// Override loadfile and dofile if requested.
		if loadfile != nil {
			l.PushClosure(0, loadfile)
			l.PushValue(-1) // extra copy for dofile upvalue
			l.RawSetField(-3, "loadfile")

			l.PushClosure(1, func(l *State) (int, error) {
				if tp := l.Type(1); tp != TypeNone && tp != TypeNil && tp != TypeString {
					return 0, NewTypeError(l, 1, TypeString.String())
				}
				l.SetTop(1)

				// loadfile(filename)
				l.PushValue(UpvalueIndex(1))
				l.Rotate(1, 1)
				if err := l.Call(1, 2, 0); err != nil {
					return 0, err
				}
				if l.IsNil(-2) {
					msg, _ := ToString(l, -1)
					return 0, fmt.Errorf("dofile: %s", msg)
				}
				l.Pop(1)

				// Call the loaded function.
				if err := l.Call(0, MultipleReturns, 0); err != nil {
					return 0, err
				}
				return l.Top(), nil
			})
			l.RawSetField(-2, "dofile")
		}

		return 1, nil
	}
}

// OpenCoroutine loads the standard coroutine library.
// This function is intended to be used as an argument to [Require].
func OpenCoroutine(l *State) (int, error) {
	nArgs := l.Top()
	lua54.PushOpenCoroutine(&l.state)
	l.Rotate(1, 1)
	if err := l.Call(nArgs, MultipleReturns, 0); err != nil {
		return 0, err
	}
	return l.Top(), nil
}

// OpenTable loads the standard table library.
// This function is intended to be used as an argument to [Require].
func OpenTable(l *State) (int, error) {
	nArgs := l.Top()
	lua54.PushOpenTable(&l.state)
	l.Rotate(1, 1)
	if err := l.Call(nArgs, MultipleReturns, 0); err != nil {
		return 0, err
	}
	return l.Top(), nil
}

// OpenString loads the standard string library.
// This function is intended to be used as an argument to [Require].
func OpenString(l *State) (int, error) {
	nArgs := l.Top()
	lua54.PushOpenString(&l.state)
	l.Rotate(1, 1)
	if err := l.Call(nArgs, MultipleReturns, 0); err != nil {
		return 0, err
	}
	return l.Top(), nil
}

// OpenUTF8 loads the standard utf8 library.
// This function is intended to be used as an argument to [Require].
func OpenUTF8(l *State) (int, error) {
	nArgs := l.Top()
	lua54.PushOpenUTF8(&l.state)
	l.Rotate(1, 1)
	if err := l.Call(nArgs, MultipleReturns, 0); err != nil {
		return 0, err
	}
	return l.Top(), nil
}

// NewOpenMath returns a [Function] that loads the standard math library.
// If a [rand.Source] is provided,
// then it is used instead of Lua's built-in random number generator.
// The resulting function is intended to be used as an argument to [Require].
func NewOpenMath(src rand.Source) Function {
	var r *rand.Rand
	if src != nil {
		r = rand.New(src)
	}
	return func(l *State) (int, error) {
		// Call stock luaopen_math.
		nArgs := l.Top()
		lua54.PushOpenMath(&l.state)
		l.Rotate(1, 1)
		if err := l.Call(nArgs, 1, 0); err != nil {
			return 0, err
		}

		// Override random and randomseed, if appropriate.
		if r != nil {
			l.PushClosure(0, func(l *State) (int, error) {
				var lo, hi int64
				switch l.Top() {
				case 0:
					l.PushNumber(r.Float64())
					return 1, nil
				case 1: // only upper limit
					lo = 1
					var err error
					hi, err = CheckInteger(l, 1)
					if err != nil {
						return 0, err
					}
					if hi == 0 {
						l.PushInteger(int64(r.Uint64()))
						return 1, nil
					}
				case 2:
					var err error
					lo, err = CheckInteger(l, 1)
					if err != nil {
						return 0, err
					}
					hi, err = CheckInteger(l, 2)
					if err != nil {
						return 0, err
					}
				default:
					return 0, fmt.Errorf("%swrong number of arguments", Where(l, 1))
				}

				if lo > hi {
					return 0, NewArgError(l, 1, "interval is empty")
				}
				if uint64(hi-lo) >= 1<<63 {
					return 0, NewArgError(l, 1, "interval is too large")
				}
				l.PushInteger(lo + r.Int63n(hi-lo+1))
				return 1, nil
			})
			l.RawSetField(-2, "random")

			l.PushClosure(0, func(l *State) (int, error) {
				var x, y int64
				if l.IsNone(1) {
					var bits [16]byte
					if _, err := cryptorand.Read(bits[:]); err != nil {
						return 0, err
					}
					x = int64(binary.LittleEndian.Uint64(bits[:8]))
					y = int64(binary.LittleEndian.Uint64(bits[8:]))
				} else {
					var err error
					x, err = CheckInteger(l, 1)
					if err != nil {
						return 0, err
					}
					if !l.IsNoneOrNil(2) {
						y, err = CheckInteger(l, 2)
						if err != nil {
							return 0, err
						}
					}
				}
				r.Seed(x ^ y)
				l.PushInteger(x)
				l.PushInteger(y)
				return 2, nil
			})
			l.RawSetField(-2, "randomseed")
		}

		return 1, nil
	}
}

// OpenDebug loads the standard debug library.
// This function is intended to be used as an argument to [Require].
func OpenDebug(l *State) (int, error) {
	nArgs := l.Top()
	lua54.PushOpenDebug(&l.state)
	l.Rotate(1, 1)
	if err := l.Call(nArgs, MultipleReturns, 0); err != nil {
		return 0, err
	}
	return l.Top(), nil
}

// OpenPackage loads the standard package library.
// This function is intended to be used as an argument to [Require].
func OpenPackage(l *State) (int, error) {
	nArgs := l.Top()
	lua54.PushOpenPackage(&l.state)
	l.Rotate(1, 1)
	if err := l.Call(nArgs, MultipleReturns, 0); err != nil {
		return 0, err
	}
	return l.Top(), nil
}

func pushFileResult(l *State, err error) int {
	// TODO(someday): Test for syscall.Errno.
	if err == nil {
		l.PushBoolean(true)
		return 1
	}
	pushFail(l)
	l.PushString(err.Error())
	l.PushInteger(1)
	return 3
}

func pushFail(l *State) {
	l.PushNil()
}
