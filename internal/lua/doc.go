// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

/*
Package lua implements a Lua 5.4 virtual machine.
It is similar to the de facto C Lua implementation,
but takes advantage of the Go runtime and garbage collector.

[State] is the main entrypoint for this package,
and [OpenLibraries] is used to load the standard library.

# Relation to the C API

Methods on [State] are generally equivalent to C functions that start with “lua_” (the [Lua C API]);
functions in this package are generally equivalent to C functions that start with “luaL_” (the [auxiliary library]).

However, there are some differences:

  - Error handling is handled using the standard Go error type.
    This is documented in more depth on [State],
    but importantly, note that [*State.Call] and other functions that can call Lua code
    will not push an error object on the stack.
  - This package does not provide to-be-closed slots to functions implemented in Go.
    It is assumed that such functions will use “defer” to handle cleanup.
    This eliminates the need to check for errors in functions like [*State.Pop].
  - Coroutines are not implemented. Maybe one day.
  - There is no light userdata, despite there being a [TypeLightUserdata] constant.
    Full userdata holds an “any” value, which is more flexible in Go.
  - There is no lua_topointer, but you can use [*State.ID] for similar purposes.
  - [State] does not have to be closed: the Go garbage collector fully manages its resources.

# Differences from de facto C implementation

  - The “__gc” (finalizer) metamethod is never called.
    Use “__close” instead, as the semantics are well-defined.
  - Weak tables (i.e. the “__mode” metafield) are not supported.
  - The “string” library has differences; see [OpenString] for more details.

[Lua C API]: https://www.lua.org/manual/5.4/manual.html#4
[auxiliary library]: https://www.lua.org/manual/5.4/manual.html#5
*/
package lua
