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

/*
Package lua provides low-level bindings for [Lua 5.4].

# Relationship to C API

This package attempts to be a mostly one-to-one mapping with the [Lua C API].
The methods on [State] and [ActivationRecord] are the primitive functions
(i.e. the library functions that start with "lua_").
Functions in this package mostly correspond to the [auxiliary library]
(i.e. the library functions that start with "luaL_"),
but are pure Go reimplementations of these functions,
usually with Go-specific niceties.

[Lua 5.4]: https://www.lua.org/versions.html#5.4
[Lua C API]: https://www.lua.org/manual/5.4/manual.html#4
[auxiliary library]: https://www.lua.org/manual/5.4/manual.html#5
*/
package lua

import (
	"io"
	"unsafe"

	"zombiezen.com/go/zb/internal/lua54"
)

// Version number.
const (
	VersionNum        = lua54.VersionNum
	VersionReleaseNum = lua54.VersionReleaseNum
)

// Version strings.
const (
	// Version is the version string without the final "release" number.
	Version = lua54.Version
	// Release is the full version string.
	Release = lua54.Release
	// Copyright is the full version string with a copyright notice.
	Copyright = lua54.Copyright
	// Authors is a string listing the authors of Lua.
	Authors = lua54.Copyright

	VersionMajor   = lua54.VersionMajor
	VersionMinor   = lua54.VersionMinor
	VersionRelease = lua54.VersionRelease
)

// RegistryIndex is a pseudo-index to the [registry],
// a predefined table that can be used by any Go or C code
// to store whatever Lua values it needs to store.
//
// [registry]: https://www.lua.org/manual/5.4/manual.html#4.3
const RegistryIndex int = lua54.RegistryIndex

// MultipleReturns is the option for multiple returns in [State.Call].
const MultipleReturns int = lua54.MultipleReturns

// UpvalueIndex returns the pseudo-index that represents the i-th upvalue
// of the running function.
// If i is outside the range [1, 255], UpvalueIndex panics.
func UpvalueIndex(i int) int {
	return lua54.UpvalueIndex(i)
}

// Predefined keys in the registry.
const (
	// RegistryIndexMainThread is the index at which the registry has the main thread of the state.
	RegistryIndexMainThread int64 = lua54.RegistryIndexMainThread
	// RegistryIndexGlobals is the index at which the registry has the global environment.
	RegistryIndexGlobals int64 = lua54.RegistryIndexGlobals

	// LoadedTable is the key in the registry for the table of loaded modules.
	LoadedTable = lua54.LoadedTable
	// PreloadTable is the key in the registry for the table of preloaded loaders.
	PreloadTable = lua54.PreloadTable
)

// Type is an enumeration of Lua data types.
type Type lua54.Type

// TypeNone is the value returned from [State.Type]
// for a non-valid but acceptable index.
const TypeNone Type = Type(lua54.TypeNone)

// Value types.
const (
	TypeNil           Type = Type(lua54.TypeNil)
	TypeBoolean       Type = Type(lua54.TypeBoolean)
	TypeLightUserdata Type = Type(lua54.TypeLightUserdata)
	TypeNumber        Type = Type(lua54.TypeNumber)
	TypeString        Type = Type(lua54.TypeString)
	TypeTable         Type = Type(lua54.TypeTable)
	TypeFunction      Type = Type(lua54.TypeFunction)
	TypeUserdata      Type = Type(lua54.TypeUserdata)
	TypeThread        Type = Type(lua54.TypeThread)
)

// String returns the name of the type encoded by the value tp.
func (tp Type) String() string {
	return lua54.Type(tp).String()
}

// State represents a Lua execution thread.
// The zero value is a state with a single main thread,
// an empty stack, and an empty environment.
//
// Methods that take in stack indices have a notion of
// [valid and acceptable indices].
// If a method receives a stack index that is not within range,
// it will panic.
// Methods may also panic if there is insufficient stack space.
// Use [State.CheckStack]
// to ensure that the State has sufficient stack space before making calls,
// but note that any new State or called function
// will support pushing at least 20 values.
//
// [valid and acceptable indices]: https://www.lua.org/manual/5.4/manual.html#4.1.2
type State struct {
	state lua54.State
}

// Close releases all resources associated with the state.
// Making further calls to the State will create a new execution environment.
func (l *State) Close() error {
	return l.state.Close()
}

// AbsIndex converts the acceptable index idx
// into an equivalent absolute index
// (that is, one that does not depend on the stack size).
// AbsIndex panics if idx is not an acceptable index.
func (l *State) AbsIndex(idx int) int {
	return l.state.AbsIndex(idx)
}

// Top returns the index of the top element in the stack.
// Because indices start at 1,
// this result is equal to the number of elements in the stack;
// in particular, 0 means an empty stack.
func (l *State) Top() int {
	return l.state.Top()
}

// SetTop accepts any index, or 0, and sets the stack top to this index.
// If the new top is greater than the old one,
// then the new elements are filled with nil.
// If idx is 0, then all stack elements are removed.
func (l *State) SetTop(idx int) {
	l.state.SetTop(idx)
}

// Pop pops n elements from the stack.
func (l *State) Pop(n int) {
	l.state.Pop(n)
}

// PushValue pushes a copy of the element at the given index onto the stack.
func (l *State) PushValue(idx int) {
	l.state.PushValue(idx)
}

// Rotate rotates the stack elements
// between the valid index idx and the top of the stack.
// The elements are rotated n positions in the direction of the top, for a positive n,
// or -n positions in the direction of the bottom, for a negative n.
// If the absolute value of n is greater than the size of the slice being rotated,
// or if idx is a pseudo-index,
// Rotate panics.
func (l *State) Rotate(idx, n int) {
	l.state.Rotate(idx, n)
}

// Insert moves the top element into the given valid index,
// shifting up the elements above this index to open space.
// If idx is a pseudo-index, Insert panics.
func (l *State) Insert(idx int) {
	l.state.Insert(idx)
}

// Remove removes the element at the given valid index,
// shifting down the elements above this index to fill the gap.
// This function cannot be called with a pseudo-index,
// because a pseudo-index is not an actual stack position.
func (l *State) Remove(idx int) {
	l.state.Remove(idx)
}

// Copy copies the element at index fromIdx into the valid index toIdx,
// replacing the value at that position.
// Values at other positions are not affected.
func (l *State) Copy(fromIdx, toIdx int) {
	l.state.Copy(fromIdx, toIdx)
}

// Replace moves the top element into the given valid index without shifting any element
// (therefore replacing the value at that given index),
// and then pops the top element.
func (l *State) Replace(idx int) {
	l.state.Replace(idx)
}

// CheckStack ensures that the stack has space for at least n extra elements,
// that is, that you can safely push up to n values into it.
// It returns false if it cannot fulfill the request,
// either because it would cause the stack to be greater than a fixed maximum size
// (typically at least several thousand elements)
// or because it cannot allocate memory for the extra space.
// This function never shrinks the stack;
// if the stack already has space for the extra elements, it is left unchanged.
func (l *State) CheckStack(n int) bool {
	return l.state.CheckStack(n)
}

// IsNumber reports if the value at the given index is a number
// or a string convertible to a number.
func (l *State) IsNumber(idx int) bool {
	return l.state.IsNumber(idx)
}

// IsString reports if the value at the given index is a string
// or a number (which is always convertible to a string).
func (l *State) IsString(idx int) bool {
	return l.state.IsString(idx)
}

// IsNativeFunction reports if the value at the given index is a C or Go function.
func (l *State) IsNativeFunction(idx int) bool {
	return l.state.IsNativeFunction(idx)
}

// IsInteger reports if the value at the given index is an integer
// (that is, the value is a number and is represented as an integer).
func (l *State) IsInteger(idx int) bool {
	return l.state.IsInteger(idx)
}

// IsUserdata reports if the value at the given index is a userdata (either full or light).
func (l *State) IsUserdata(idx int) bool {
	return l.state.IsUserdata(idx)
}

// Type returns the type of the value in the given valid index,
// or [TypeNone] for a non-valid but acceptable index.
func (l *State) Type(idx int) Type {
	return Type(l.state.Type(idx))
}

// IsFunction reports if the value at the given index is a function (any of C, Go, or Lua).
func (l *State) IsFunction(idx int) bool {
	return l.state.IsFunction(idx)
}

// IsTable reports if the value at the given index is a table.
func (l *State) IsTable(idx int) bool {
	return l.state.IsTable(idx)
}

// IsNil reports if the value at the given index is nil.
func (l *State) IsNil(idx int) bool {
	return l.state.IsNil(idx)
}

// IsBoolean reports if the value at the given index is a boolean.
func (l *State) IsBoolean(idx int) bool {
	return l.state.IsBoolean(idx)
}

// IsThread reports if the value at the given index is a thread.
func (l *State) IsThread(idx int) bool {
	return l.state.IsThread(idx)
}

// IsNone reports if the index is not valid.
func (l *State) IsNone(idx int) bool {
	return l.state.IsNone(idx)
}

// IsNoneOrNil reports if the index is not valid or the value at this index is nil.
func (l *State) IsNoneOrNil(idx int) bool {
	return l.state.IsNoneOrNil(idx)
}

// ToNumber converts the Lua value at the given index to a floating point number.
// The Lua value must be a number or a [string convertible to a number];
// otherwise, ToNumber returns (0, false).
// ok is true if the operation succeeded.
//
// [string convertible to a number]: https://www.lua.org/manual/5.4/manual.html#3.4.3
func (l *State) ToNumber(idx int) (n float64, ok bool) {
	return l.state.ToNumber(idx)
}

// ToInteger converts the Lua value at the given index to a signed 64-bit integer.
// The Lua value must be an integer, a number, or a [string convertible to an integer];
// otherwise, ToInteger returns (0, false).
// ok is true if the operation succeeded.
//
// [string convertible to an integer]: https://www.lua.org/manual/5.4/manual.html#3.4.3
func (l *State) ToInteger(idx int) (n int64, ok bool) {
	return l.state.ToInteger(idx)
}

// ToBoolean converts the Lua value at the given index to a boolean value.
// Like all tests in Lua,
// ToBoolean returns true for any Lua value different from false and nil;
// otherwise it returns false.
func (l *State) ToBoolean(idx int) bool {
	return l.state.ToBoolean(idx)
}

// ToString converts the Lua value at the given index to a Go string.
// The Lua value must be a string or a number; otherwise, the function returns ("", false).
// If the value is a number, then ToString also changes the actual value in the stack to a string.
// (This change confuses [State.Next]
// when ToString is applied to keys during a table traversal.)
func (l *State) ToString(idx int) (s string, ok bool) {
	return l.state.ToString(idx)
}

// StringContext returns any context values associated with the string at the given index.
// If the Lua value is not a string, the function returns nil.
func (l *State) StringContext(idx int) []string {
	return l.state.StringContext(idx)
}

// RawLen returns the raw "length" of the value at the given index:
// for strings, this is the string length;
// for tables, this is the result of the length operator ('#') with no metamethods;
// for userdata, this is the size of the block of memory allocated for the userdata.
// For other values, RawLen returns 0.
func (l *State) RawLen(idx int) uint64 {
	return l.state.RawLen(idx)
}

// CopyUserdata copies bytes from the userdata's block of bytes
// to dst if the value at the given index is a full userdata.
// It returns the number of bytes copied.
func (l *State) CopyUserdata(dst []byte, idx, start int) int {
	return l.state.CopyUserdata(dst, idx, start)
}

// ToPointer converts the value at the given index to a generic pointer
// and returns its numeric address.
// The value can be a userdata, a table, a thread, a string, or a function;
// otherwise, ToPointer returns 0.
// Different objects will give different addresses.
// Typically this function is used only for hashing and debug information.
func (l *State) ToPointer(idx int) uintptr {
	return l.state.ToPointer(idx)
}

// RawEqual reports whether the two values in the given indices
// are primitively equal (that is, equal without calling the __eq metamethod).
func (l *State) RawEqual(idx1, idx2 int) bool {
	return l.state.RawEqual(idx1, idx2)
}

// PushNil pushes a nil value onto the stack.
func (l *State) PushNil() {
	l.state.PushNil()
}

// PushNumber pushes a floating point number onto the stack.
func (l *State) PushNumber(n float64) {
	l.state.PushNumber(n)
}

// PushInteger pushes an integer onto the stack.
func (l *State) PushInteger(n int64) {
	l.state.PushInteger(n)
}

// PushString pushes a string onto the stack.
func (l *State) PushString(s string) {
	l.state.PushString(s)
}

// PushString pushes a string onto the stack
// with the given context arguments.
func (l *State) PushStringContext(s string, context []string) {
	l.state.PushStringContext(s, context)
}

// PushBoolean pushes a boolean onto the stack.
func (l *State) PushBoolean(b bool) {
	l.state.PushBoolean(b)
}

// PushLightUserdata pushes a light userdata onto the stack.
//
// Userdata represent C or Go values in Lua.
// A light userdata represents a pointer.
// It is a value (like a number): you do not create it, it has no individual metatable,
// and it is not collected (as it was never created).
// A light userdata is equal to "any" light userdata with the same address.
func (l *State) PushLightUserdata(p uintptr) {
	l.state.PushLightUserdata(p)
}

// A Function is a callback for Lua function implemented in Go.
// A Go function receives its arguments from Lua in its stack in direct order
// (the first argument is pushed first).
// So, when the function starts,
// [State.Top] returns the number of arguments received by the function.
// The first argument (if any) is at index 1 and its last argument is at index [State.Top].
// To return values to Lua, a Go function just pushes them onto the stack,
// in direct order (the first result is pushed first),
// and returns in Go the number of results.
// Any other value in the stack below the results will be properly discarded by Lua.
// Like a Lua function, a Go function called by Lua can also return many results.
// To raise an error, return a Go error
// and the string result of its Error() method will be used as the error object.
type Function func(*State) (int, error)

// PushClosure pushes a Go closure onto the stack.
// n is how many upvalues this function will have,
// popped off the top of the stack.
// (When there are multiple upvalues, the first value is pushed first.)
// If n is negative or greater than 254, then PushClosure panics.
//
// Under the hood, PushClosure uses the first Lua upvalue
// to store a reference to the Go function.
// [UpvalueIndex] already compensates for this,
// so the first upvalue you push with PushClosure
// can be accessed with UpvalueIndex(1).
// As such, this implementation detail is largely invisible
// except in debug interfaces like [State.Upvalue] and [State.SetUpvalue].
// No assumptions should be made about the content of the first upvalue,
// as it is subject to change,
// but it is guaranteed that PushClosure will use exactly one upvalue.
func (l *State) PushClosure(n int, f Function) {
	// This should be safe because State and lua54.State are identical in layout.
	g := *(*lua54.Function)(unsafe.Pointer(&f))
	l.state.PushClosure(n, g)
}

// Global pushes onto the stack the value of the global with the given name,
// returning the type of that value.
//
// As in Lua, this function may trigger a metamethod on the globals table
// for the "index" event.
// If there is any error, Global catches it,
// pushes a single value on the stack (the error object),
// and returns an error with [TypeNil].
//
// If msgHandler is 0,
// then the error object returned on the stack is exactly the original error object.
// Otherwise, msgHandler is the stack index of a message handler.
// (This index cannot be a pseudo-index.)
// In case of runtime errors, this handler will be called with the error object
// and its return value will be the object returned on the stack by Table.
// Typically, the message handler is used to add more debug information to the error object,
// such as a stack traceback.
// Such information cannot be gathered after the return of Table,
// since by then the stack has unwound.
func (l *State) Global(name string, msgHandler int) (Type, error) {
	tp, err := l.state.Global(name, msgHandler)
	return Type(tp), err
}

// Table pushes onto the stack the value t[k],
// where t is the value at the given index
// and k is the value on the top of the stack.
// Returns the type of the pushed value.
//
// This function pops the key from the stack,
// pushing the resulting value in its place.
//
// As in Lua, this function may trigger a metamethod for the "index" event.
// If there is any error, Table catches it,
// pushes a single value on the stack (the error object),
// and returns an error with [TypeNil].
// Table always removes the key from the stack.
//
// If msgHandler is 0,
// then the error object returned on the stack is exactly the original error object.
// Otherwise, msgHandler is the stack index of a message handler.
// (This index cannot be a pseudo-index.)
// In case of runtime errors, this handler will be called with the error object
// and its return value will be the object returned on the stack by Table.
// Typically, the message handler is used to add more debug information to the error object,
// such as a stack traceback.
// Such information cannot be gathered after the return of Table,
// since by then the stack has unwound.
func (l *State) Table(idx, msgHandler int) (Type, error) {
	tp, err := l.state.Table(idx, msgHandler)
	return Type(tp), err
}

// Field pushes onto the stack the value t[k],
// where t is the value at the given index.
// See [State.Table] for further information.
func (l *State) Field(idx int, k string, msgHandler int) (Type, error) {
	tp, err := l.state.Field(idx, k, msgHandler)
	return Type(tp), err
}

// RawGet pushes onto the stack t[k],
// where t is the value at the given index
// and k is the value on the top of the stack.
// This function pops the key from the stack,
// pushing the resulting value in its place.
//
// RawGet does a raw access (i.e. without metamethods).
// The value at idx must be a table.
func (l *State) RawGet(idx int) Type {
	return Type(l.state.RawGet(idx))
}

// RawIndex pushes onto the stack the value t[n],
// where t is the table at the given index.
// The access is raw, that is, it does not use the __index metavalue.
// Returns the type of the pushed value.
func (l *State) RawIndex(idx int, n int64) Type {
	return Type(l.state.RawIndex(idx, n))
}

// RawField pushes onto the stack t[k],
// where t is the value at the given index.
//
// RawField does a raw access (i.e. without metamethods).
// The value at idx must be a table.
func (l *State) RawField(idx int, k string) Type {
	return Type(l.state.RawField(idx, k))
}

// CreateTable creates a new empty table and pushes it onto the stack.
// nArr is a hint for how many elements the table will have as a sequence;
// nRec is a hint for how many other elements the table will have.
// Lua may use these hints to preallocate memory for the new table.
func (l *State) CreateTable(nArr, nRec int) {
	l.state.CreateTable(nArr, nRec)
}

// NewUserdataUV creates and pushes on the stack a new full userdata,
// with nUValue associated Lua values, called user values,
// plus an associated block of size bytes.
// The user values can be accessed or modified
// using [State.UserValue] and [State.SetUserValue] respectively.
// The block of bytes can be set and read
// using [State.SetUserdata] and [State.CopyUserdata] respectively.
func (l *State) NewUserdataUV(size, nUValue int) {
	l.state.NewUserdataUV(size, nUValue)
}

// SetUserdata copies the bytes from src to the userdata's block of bytes,
// starting at the given start byte position.
// SetUserdata panics if start+len(src) is greater than the size of the userdata's block of bytes.
func (l *State) SetUserdata(idx int, start int, src []byte) {
	l.state.SetUserdata(idx, 0, src)
}

// Metatable reports whether the value at the given index has a metatable
// and if so, pushes that metatable onto the stack.
func (l *State) Metatable(idx int) bool {
	return l.state.Metatable(idx)
}

// UserValue pushes onto the stack the n-th user value
// associated with the full userdata at the given index
// and returns the type of the pushed value.
// If the userdata does not have that value, pushes nil and returns [TypeNone].
// (As with other Lua APIs, the first user value is n=1.)
func (l *State) UserValue(idx int, n int) Type {
	return Type(l.state.UserValue(idx, n))
}

// SetGlobal pops a value from the stack
// and sets it as the new value of the global with the given name.
//
// As in Lua, this function may trigger a metamethod on the globals table
// for the "newindex" event.
// If there is any error, SetGlobal catches it,
// pushes a single value on the stack (the error object),
// and returns an error.
// SetGlobal always removes the value from the stack.
//
// If msgHandler is 0,
// then the error object returned on the stack is exactly the original error object.
// Otherwise, msgHandler is the stack index of a message handler.
// (This index cannot be a pseudo-index.)
// In case of runtime errors, this handler will be called with the error object
// and its return value will be the object returned on the stack by SetGlobal.
// Typically, the message handler is used to add more debug information to the error object,
// such as a stack traceback.
// Such information cannot be gathered after the return of SetGlobal,
// since by then the stack has unwound.
func (l *State) SetGlobal(name string, msgHandler int) error {
	return l.state.SetGlobal(name, msgHandler)
}

// SetTable does the equivalent to t[k] = v,
// where t is the value at the given index,
// v is the value on the top of the stack,
// and k is the value just below the top.
// This function pops both the key and the value from the stack.
//
// As in Lua, this function may trigger a metamethod for the "newindex" event.
// If there is any error, SetTable catches it,
// pushes a single value on the stack (the error object),
// and returns an error.
// SetTable always removes the key and value from the stack.
//
// If msgHandler is 0,
// then the error object returned on the stack is exactly the original error object.
// Otherwise, msgHandler is the stack index of a message handler.
// (This index cannot be a pseudo-index.)
// In case of runtime errors, this handler will be called with the error object
// and its return value will be the object returned on the stack by SetTable.
// Typically, the message handler is used to add more debug information to the error object,
// such as a stack traceback.
// Such information cannot be gathered after the return of SetTable,
// since by then the stack has unwound.
func (l *State) SetTable(idx, msgHandler int) error {
	return l.state.SetTable(idx, msgHandler)
}

// SetField does the equivalent to t[k] = v,
// where t is the value at the given index,
// v is the value on the top of the stack,
// and k is the given string.
// This function pops the value from the stack.
// See [State.SetTable] for more information.
func (l *State) SetField(idx int, k string, msgHandler int) error {
	return l.state.SetField(idx, k, msgHandler)
}

// RawSet does the equivalent to t[k] = v,
// where t is the value at the given index,
// v is the value on the top of the stack,
// and k is the value just below the top.
// This function pops both the key and the value from the stack.
func (l *State) RawSet(idx int) {
	l.state.RawSet(idx)
}

// RawSetIndex does the equivalent of t[n] = v,
// where t is the table at the given index
// and v is the value on the top of the stack.
// This function pops the value from the stack.
// The assignment is raw, that is, it does not use the __newindex metavalue.
func (l *State) RawSetIndex(idx int, n int64) {
	l.state.RawSetIndex(idx, n)
}

// RawSetField does the equivalent to t[k] = v,
// where t is the value at the given index
// and v is the value on the top of the stack.
// This function pops the value from the stack.
func (l *State) RawSetField(idx int, k string) {
	l.state.RawSetField(idx, k)
}

// SetMetatable pops a table or nil from the stack
// and sets that value as the new metatable for the value at the given index.
// (nil means no metatable.)
func (l *State) SetMetatable(objIndex int) {
	l.state.SetMetatable(objIndex)
}

// SetUserValue pops a value from the stack
// and sets it as the new n-th user value
// associated to the full userdata at the given index,
// reporting if the userdata has that value.
// (As with other Lua APIs, the first user value is n=1.)
func (l *State) SetUserValue(idx int, n int) bool {
	return l.state.SetUserValue(idx, n)
}

// Call calls a function (or callable object) in protected mode.
//
// To do a call you must use the following protocol:
// first, the function to be called is pushed onto the stack;
// then, the arguments to the call are pushed in direct order;
// that is, the first argument is pushed first.
// Finally you call Call;
// nArgs is the number of arguments that you pushed onto the stack.
// When the function returns,
// all arguments and the function value are popped
// and the call results are pushed onto the stack.
// The number of results is adjusted to nResults,
// unless nResults is [MultipleReturns].
// In this case, all results from the function are pushed;
// Lua takes care that the returned values fit into the stack space,
// but it does not ensure any extra space in the stack.
// The function results are pushed onto the stack in direct order
// (the first result is pushed first),
// so that after the call the last result is on the top of the stack.
//
// If there is any error, Call catches it,
// pushes a single value on the stack (the error object),
// and returns an error.
// Call always removes the function and its arguments from the stack.
//
// If msgHandler is 0,
// then the error object returned on the stack is exactly the original error object.
// Otherwise, msgHandler is the stack index of a message handler.
// (This index cannot be a pseudo-index.)
// In case of runtime errors, this handler will be called with the error object
// and its return value will be the object returned on the stack by Call.
// Typically, the message handler is used to add more debug information to the error object,
// such as a stack traceback.
// Such information cannot be gathered after the return of Call,
// since by then the stack has unwound.
func (l *State) Call(nArgs, nResults, msgHandler int) error {
	return l.state.Call(nArgs, nResults, msgHandler)
}

// Load loads a Lua chunk without running it.
// If there are no errors,
// Load pushes the compiled chunk as a Lua function on top of the stack.
// Otherwise, it pushes an error message.
//
// The chunkName argument gives a name to the chunk,
// which is used for error messages and in [debug information].
//
// The string mode controls whether the chunk can be text or binary
// (that is, a precompiled chunk).
// It may be the string "b" (only binary chunks),
// "t" (only text chunks),
// or "bt" (both binary and text).
//
// [debug information]: https://www.lua.org/manual/5.4/manual.html#4.7
func (l *State) Load(r io.Reader, chunkName string, mode string) error {
	return l.state.Load(r, chunkName, mode)
}

// LoadString loads a Lua chunk from a string without running it.
// It behaves the same as [State.Load],
// but takes in a string instead of an [io.Reader].
func (l *State) LoadString(s string, chunkName string, mode string) error {
	return l.state.LoadString(s, chunkName, mode)
}

// Dump dumps a function as a binary chunk to the given writer.
// Receives a Lua function on the top of the stack and produces a binary chunk that,
// if loaded again, results in a function equivalent to the one dumped.
// If strip is true, the binary representation may not include all debug information about the function, to save space.
// Dump does not pop the Lua function from the stack.
// Returns the number of bytes written and the first error that occurred.
func (l *State) Dump(w io.Writer, strip bool) (int64, error) {
	return l.state.Dump(w, strip)
}

// GC performs a full garbage-collection cycle.
//
// This function should not be called by a Lua finalizer.
func (l *State) GC() {
	l.state.GC()
}

// GCStop stops the garbage collector.
//
// This function should not be called by a Lua finalizer.
func (l *State) GCStop() {
	l.state.GCStop()
}

// GCRestart restarts the garbage collector.
//
// This function should not be called by a Lua finalizer.
func (l *State) GCRestart() {
	l.state.GCRestart()
}

// GCCount returns the current amount of memory (in bytes) in use by Lua.
//
// This function should not be called by a Lua finalizer.
func (l *State) GCCount() int64 {
	return l.state.GCCount()
}

// GCStep performs an incremental step of garbage collection,
// corresponding to the allocation of stepSize kibibytes.
//
// This function should not be called by a Lua finalizer.
func (l *State) GCStep(stepSize int) {
	l.state.GCStep(stepSize)
}

// IsGCRunning reports whether the garbage collector is running
// (i.e. not stopped).
//
// This function should not be called by a Lua finalizer.
func (l *State) IsGCRunning() bool {
	return l.state.IsGCRunning()
}

// GCIncremental changes the collector to [incremental mode] with the given parameters.
//
// This function should not be called by a Lua finalizer.
//
// [incremental mode]: https://www.lua.org/manual/5.4/manual.html#2.5.1
func (l *State) GCIncremental(pause, stepMul, stepSize int) {
	l.state.GCIncremental(pause, stepMul, stepSize)
}

// GCGenerational changes the collector to [generational mode] with the given parameters.
//
// This function should not be called by a Lua finalizer.
//
// [generational mode]: https://www.lua.org/manual/5.4/manual.html#2.5.2
func (l *State) GCGenerational(minorMul, majorMul int) {
	l.state.GCGenerational(minorMul, majorMul)
}

// Next pops a key from the stack,
// and pushes a key–value pair from the table at the given index,
// the "next" pair after the given key.
// If there are no more elements in the table,
// then Next returns false and pushes nothing.
//
// While traversing a table,
// avoid calling [State.ToString] directly on a key,
// unless you know that the key is actually a string.
// Recall that [State.ToString] may change the value at the given index;
// this confuses the next call to Next.
//
// This behavior of this function is undefined if the given key
// is neither nil nor present in the table.
// See function [next] for the caveats of modifying the table during its traversal.
//
// [next]: https://www.lua.org/manual/5.4/manual.html#pdf-next
func (l *State) Next(idx int) bool {
	return l.state.Next(idx)
}

// Concat concatenates the n values at the top of the stack, pops them,
// and leaves the result on the top.
// If n is 1, the result is the single value on the stack
// (that is, the function does nothing);
// if n is 0, the result is the empty string.
// Concatenation is performed following the usual semantics of Lua.
func (l *State) Concat(n, msgHandler int) error {
	return l.state.Concat(n, msgHandler)
}

// Len pushes the length of the value at the given index to the stack.
// It is equivalent to the ['#' operator in Lua]
// and may trigger a [metamethod] for the "length" event.
//
// If there is any error, Len catches it,
// pushes a single value on the stack (the error object),
// and returns an error.
//
// If msgHandler is 0,
// then the error object returned on the stack is exactly the original error object.
// Otherwise, msgHandler is the stack index of a message handler.
// (This index cannot be a pseudo-index.)
// In case of runtime errors, this handler will be called with the error object
// and its return value will be the object returned on the stack by Len.
// Typically, the message handler is used to add more debug information to the error object,
// such as a stack traceback.
// Such information cannot be gathered after the return of Len,
// since by then the stack has unwound.
//
// ['#' operator in Lua]: https://www.lua.org/manual/5.4/manual.html#3.4.7
// [metamethod]: https://www.lua.org/manual/5.4/manual.html#2.4
func (l *State) Len(idx, msgHandler int) error {
	return l.state.Len(idx, msgHandler)
}

// Stack returns an identifier of the activation record
// of the function executing at the given level.
// Level 0 is the current running function,
// whereas level n+1 is the function that has called level n
// (except for tail calls, which do not count in the stack).
// When called with a level greater than the stack depth, Stack returns nil.
func (l *State) Stack(level int) *ActivationRecord {
	ar := l.state.Stack(level)
	if ar == nil {
		return nil
	}
	return &ActivationRecord{ar}
}

// Info gets information about a specific function.
// Each character in the string what
// selects some fields of the [Debug] structure to be filled
// or a value to be pushed on the stack.
//
//   - 'f': pushes onto the stack the function that is running at the given level;
//   - 'l': fills in the field CurrentLine;
//   - 'n': fills in the fields Name and NameWhat;
//   - 'S': fills in the fields Source, ShortSource, LineDefined, LastLineDefined, and What;
//   - 't': fills in the field IsTailCall;
//   - 'u': fills in the fields NumUpvalues, NumParams, and IsVararg;
//   - 'L': pushes onto the stack a table
//     whose indices are the lines on the function with some associated code,
//     that is, the lines where you can put a break point.
//     (Lines with no code include empty lines and comments.)
//     If this option is given together with option 'f',
//     its table is pushed after the function.
func (l *State) Info(what string) *Debug {
	return (*Debug)(l.state.Info(what))
}

// Upvalue gets information about the n-th upvalue of the closure at funcIndex.
// Upvalue pushes the upvalue's value onto the stack,
// unless n is greater than the number of upvalues.
// Upvalue returns the name of the upvalue and whether the upvalue exists.
// The first upvalue is n=1.
func (l *State) Upvalue(funcIndex, n int) (name string, ok bool) {
	return l.state.Upvalue(funcIndex, n)
}

// SetUpvalue assigns the value on the top of the stack to the the closure's upvalue.
// SetUpvalue also pops the value from the stack,
// unless n is greater than the number of upvalues.
// SetUpvalue returns the name of the upvalue,
// and whether the assignment occurred.
// The first upvalue is n=1.
func (l *State) SetUpvalue(funcIndex, n int) (name string, ok bool) {
	return l.state.SetUpvalue(funcIndex, n)
}

// Debug holds information about a function or an activation record.
type Debug struct {
	// Name is a reasonable name for the given function.
	// Because functions in Lua are first-class values, they do not have a fixed name:
	// some functions can be the value of multiple global variables,
	// while others can be stored only in a table field.
	// The Info functions check how the function was called to find a suitable name.
	// If they cannot find a name, then Name is set to the empty string.
	Name string
	// NameWhat explains the Name field.
	// The value of NameWhat can be
	// "global", "local", "method", "field", "upvalue", or the empty string,
	// according to how the function was called.
	// (Lua uses the empty string when no other option seems to apply.)
	NameWhat string
	// What is the string "Lua" if the function is a Lua function,
	// "C" if it is a C or Go function,
	// "main" if it is the main part of a chunk.
	What string
	// Source is the source of the chunk that created the function.
	// If source starts with a '@',
	// it means that the function was defined in a file where the file name follows the '@'.
	// If source starts with a '=',
	// the remainder of its contents describes the source in a user-dependent manner.
	// Otherwise, the function was defined in a string where source is that string.
	Source string
	// ShortSource is a "printable" version of source, to be used in error messages.
	ShortSource string
	// CurrentLine is the current line where the given function is executing.
	// When no line information is available, CurrentLine is set to -1.
	CurrentLine int
	// LineDefined is the line number where the definition of the function starts.
	LineDefined int
	// LastLineDefined is the line number where the definition of the function ends.
	LastLineDefined int
	// NumUpvalues is the number of upvalues of the function.
	NumUpvalues uint8
	// NumParams is the number of parameters of the function
	// (always 0 for Go/C functions).
	NumParams uint8
	// IsVararg is true if the function is a variadic function
	// (always true for Go/C functions).
	IsVararg bool
	// IsTailCall is true if this function invocation was called by a tail call.
	// In this case, the caller of this level is not in the stack.
	IsTailCall bool
}

// An ActivationRecord is a reference to a function invocation's activation record.
type ActivationRecord struct {
	ar *lua54.ActivationRecord
}

// Info gets information about the function invocation.
// The what string is the same as for [State.Info].
// If Info is called on a nil ActivationRecord
// or the [State] it originated from has been closed,
// then Info returns nil.
func (ar *ActivationRecord) Info(what string) *Debug {
	if ar == nil {
		return (*Debug)((*lua54.ActivationRecord)(nil).Info(what))
	}
	return (*Debug)(ar.ar.Info(what))
}

// Standard library names.
const (
	GName = lua54.GName

	CoroutineLibraryName = lua54.CoroutineLibraryName
	TableLibraryName     = lua54.TableLibraryName
	IOLibraryName        = lua54.IOLibraryName
	OSLibraryName        = lua54.OSLibraryName
	StringLibraryName    = lua54.StringLibraryName
	UTF8LibraryName      = lua54.UTF8LibraryName
	MathLibraryName      = lua54.MathLibraryName
	DebugLibraryName     = lua54.DebugLibraryName
	PackageLibraryName   = lua54.PackageLibraryName
)

// IsOutOfMemory reports whether the error indicates a memory allocation error.
func IsOutOfMemory(err error) bool {
	code, ok := lua54.AsError(err)
	return ok && code == lua54.ErrMem
}

// IsHandlerError reports whether the error indicates an error while running the message handler.
func IsHandlerError(err error) bool {
	code, ok := lua54.AsError(err)
	return ok && code == lua54.ErrErr
}

// IsSyntax reports whether the error indicates a Lua syntax error.
func IsSyntax(err error) bool {
	code, ok := lua54.AsError(err)
	return ok && code == lua54.ErrSyntax
}

// IsYield reports whether the error indicates a coroutine yield.
func IsYield(err error) bool {
	code, ok := lua54.AsError(err)
	return ok && code == lua54.Yield
}
