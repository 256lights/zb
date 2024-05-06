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

package lua_test

import (
	"fmt"
	"log"

	"zombiezen.com/go/zb/internal/lua"
)

func Example() {
	// Create an execution environment
	// and make the standard libraries available.
	state := new(lua.State)
	defer state.Close()
	if err := lua.OpenLibraries(state); err != nil {
		log.Fatal(err)
	}

	// Load Lua code as a chunk/function.
	// Calling this function then executes it.
	const luaSource = `print("Hello, World!")`
	if err := state.LoadString(luaSource, luaSource, "t"); err != nil {
		log.Fatal(err)
	}
	if err := state.Call(0, 0, 0); err != nil {
		log.Fatal(err)
	}
	// Output:
	// Hello, World!
}

func ExampleState_Next() {
	// Create an execution environment.
	state := new(lua.State)
	defer state.Close()

	// Create a table with a single pair to print.
	state.CreateTable(0, 1)
	state.PushString("bar")
	state.RawSetField(-2, "foo")

	// Iterate over table.
	tableIndex := state.AbsIndex(-1)
	state.PushNil()
	for state.Next(tableIndex) {
		// Format key at index -2.
		// We need to be careful not to use state.ToString on the key
		// without checking its type first,
		// since state.ToString may change the value on the stack.
		// We clone the value here to be safe.
		state.PushValue(-2)
		k, _ := lua.ToString(state, -1)
		state.Pop(1)

		// Format the value at index -1.
		v, _ := lua.ToString(state, -1)

		fmt.Printf("%s - %s\n", k, v)

		// Remove value, keeping key for the next iteration.
		state.Pop(1)
	}
	// Output:
	// foo - bar
}
