// Copyright (C) 1994-2024 Lua.org, PUC-Rio.
// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

// TagMethod is an enumeration of built-in metamethods.
type TagMethod uint8

// Metamethods.
const (
	TagMethodIndex TagMethod = iota
	TagMethodNewIndex
	TagMethodGC
	TagMethodMode
	TagMethodLen
	TagMethodEq // last tag method with fast access
	TagMethodAdd
	TagMethodSub
	TagMethodMul
	TagMethodMod
	TagMethodPow
	TagMethodDiv
	TagMethodIDiv
	TagMethodBAnd
	TagMethodBOr
	TagMethodBXor
	TagMethodSHL
	TagMethodSHR
	TagMethodUNM
	TagMethodBNot
	TagMethodLT
	TagMethodLE
	TagMethodConcat
	TagMethodCall
	TagMethodClose

	numTagMethods = iota
)
