// Copyright (C) 1994-2024 Lua.org, PUC-Rio.
// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

//go:generate stringer -type=TagMethod -linecomment -output=tag_methods_string.go

package luacode

// TagMethod is an enumeration of built-in metamethods.
type TagMethod uint8

// Metamethods.
const (
	TagMethodIndex    TagMethod = 0 // __index
	TagMethodNewIndex TagMethod = 1 // __newindex
	TagMethodGC       TagMethod = 2 // __gc
	TagMethodMode     TagMethod = 3 // __mode
	TagMethodLen      TagMethod = 4 // __len
	// TagMethodEq is the equality (==) operation.
	// TagMethodEq is the last tag method with fast access.
	TagMethodEq TagMethod = 5 // __eq

	TagMethodAdd    TagMethod = 6  // __add
	TagMethodSub    TagMethod = 7  // __sub
	TagMethodMul    TagMethod = 8  // __mul
	TagMethodMod    TagMethod = 9  // __mod
	TagMethodPow    TagMethod = 10 // __pow
	TagMethodDiv    TagMethod = 11 // __div
	TagMethodIDiv   TagMethod = 12 // __idiv
	TagMethodBAnd   TagMethod = 13 // __band
	TagMethodBOr    TagMethod = 14 // __bor
	TagMethodBXor   TagMethod = 15 // __bxor
	TagMethodSHL    TagMethod = 16 // __shl
	TagMethodSHR    TagMethod = 17 // __shr
	TagMethodUNM    TagMethod = 18 // __unm
	TagMethodBNot   TagMethod = 19 // __bnot
	TagMethodLT     TagMethod = 20 // __lt
	TagMethodLE     TagMethod = 21 // __le
	TagMethodConcat TagMethod = 22 // __concat
	TagMethodCall   TagMethod = 23 // __call
	TagMethodClose  TagMethod = 24 // __close
)
