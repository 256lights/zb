// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

/*
Package mylua implements a Lua virtual machine.
It is similar to the de facto C Lua implementation,
but takes advantage of the Go runtime and garbage collector.

[State] is the main entrypoint for this package.
*/
package mylua
