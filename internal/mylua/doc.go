// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

/*
Package mylua implements a Lua virtual machine.
It is similar to the de facto C Lua implementation,
but takes advantage of the Go runtime and garbage collector.

Each [State] is a separate interpreter instance.

# Error Handling

Unlike the C Lua implementation,
this package's virtual machine uses Go's error type
instead of managing objects on the stack.
TODO(someday): Support message handlers that take in error objects.
*/
package mylua
