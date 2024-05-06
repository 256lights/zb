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

package lua54

import (
	"io"
	"runtime/cgo"
	"unsafe"
)

// This file is used to contain Go code exported to C.
// It's kept in a separate file with a minimal C preamble
// to avoid unintentional redefinitions.
// See the caveat in https://pkg.go.dev/cmd/cgo for more details.

// #include <stdlib.h>
// #include <stddef.h>
// #include "lua.h"
//
// void zombiezen_lua_pushstring(lua_State *L, _GoString_ s);
import "C"

//export zombiezen_lua_readercb
func zombiezen_lua_readercb(l *C.lua_State, data unsafe.Pointer, size *C.size_t) *C.char {
	r := (*cgo.Handle)(data).Value().(*reader)
	buf := unsafe.Slice((*byte)(unsafe.Pointer(r.buf)), readerBufferSize)
	n, err := r.r.Read(buf)
	*size = C.size_t(n)
	if n == 0 && err != nil && err != io.EOF {
		// We have a trampoline that intercepts a NULL return.
		// Push the error onto the stack.
		C.zombiezen_lua_pushstring(l, err.Error())
		return nil
	}
	return r.buf
}

type writerState struct {
	w   cgo.Handle
	n   int64
	err cgo.Handle
}

//export zombiezen_lua_writercb
func zombiezen_lua_writercb(l *C.lua_State, p unsafe.Pointer, size C.size_t, ud unsafe.Pointer) C.int {
	state := (*writerState)(ud)
	b := unsafe.Slice((*byte)(p), size)
	n, err := state.w.Value().(io.Writer).Write(b)
	state.n += int64(n)
	if err != nil {
		state.err = cgo.NewHandle(err)
		return 1
	}
	return 0
}

//export zombiezen_lua_gocb
func zombiezen_lua_gocb(l *C.lua_State) C.int {
	state := stateForCallback(l)
	defer func() {
		// Once the callback has finished, clear the State.
		// This prevents incorrect usage, especially with ActivationRecords.
		*state = State{}
	}()
	funcID := copyUint64(state, goClosureUpvalueIndex)
	f := state.data().closures[funcID]
	if f == nil {
		C.zombiezen_lua_pushstring(l, "Go closure upvalue corrupted")
		return -1
	}

	results, err := pcall(f, state)
	if err != nil {
		C.zombiezen_lua_pushstring(l, err.Error())
		return -1
	}
	if results < 0 {
		C.zombiezen_lua_pushstring(l, "Go callback returned negative results")
		return -1
	}
	return C.int(results)
}

//export zombiezen_lua_gcfunc
func zombiezen_lua_gcfunc(l *C.lua_State) C.int {
	state := stateForCallback(l)
	funcID := copyUint64(state, 1)
	if funcID != 0 {
		delete(state.data().closures, funcID)
		setUint64(state, 1, 0)
	}
	return 0
}
