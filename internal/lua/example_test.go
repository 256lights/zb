// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package lua_test

import (
	"context"
	"fmt"
	"log"
	"strings"

	lua "zb.256lights.llc/pkg/internal/lua"
)

func Example() {
	ctx := context.Background()

	// Create an execution environment
	// and make the standard libraries available.
	state := new(lua.State)
	defer state.Close()
	if err := lua.OpenLibraries(ctx, state); err != nil {
		log.Fatal(err)
	}

	// Load Lua code as a chunk/function.
	// Calling this function then executes it.
	const luaSource = `print("Hello, World!")`
	if err := state.Load(strings.NewReader(luaSource), luaSource, "t"); err != nil {
		log.Fatal(err)
	}
	if err := state.Call(ctx, 0, 0); err != nil {
		log.Fatal(err)
	}
	// Output:
	// Hello, World!
}

func ExampleState_Next() {
	ctx := context.Background()

	// Create an execution environment.
	state := new(lua.State)
	defer state.Close()

	// Create a table with a single pair to print.
	state.CreateTable(0, 1)
	state.PushString("bar")
	if err := state.RawSetField(-2, "foo"); err != nil {
		panic(err)
	}

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
		k, _, _ := lua.ToString(ctx, state, -1)
		state.Pop(1)

		// Format the value at index -1.
		v, _, _ := lua.ToString(ctx, state, -1)

		fmt.Printf("%s - %s\n", k, v)

		// Remove value, keeping key for the next iteration.
		state.Pop(1)
	}
	// Output:
	// foo - bar
}
