# Lua as used by zb

zb uses [Lua 5.4][Lua 5.4 manual] to configure builds.
Where possible, zb tries to maintain compatibility with Lua 5.4
so that resources or tooling for Lua can be used with zb.
However, to facilitate the goal of fast and reproducible builds,
zb does make some minor alterations to the standard libraries available.
This document describes the departures from standard Lua
as well as the new built-in globals that control zb's behavior.
This document serves as a specification of zb's Lua:
deviations from this document should be [reported][zb new issue].

[Lua 5.4 manual]: https://www.lua.org/manual/5.4/
[zb new issue]: https://github.com/256lights/zb/issues/new

## Language Differences

zb's Lua language semantics differ from Lua 5.4 in two key ways:

- [Weak tables][] (i.e. the `__mode` metafield) are not supported.
- The [`__gc` (finalizer) metamethod][Garbage-Collection Metamethods]
  is never called by zb's runtime.
  Finalizers are not guaranteed to run in Lua,
  so this is technically within specification,
  but this document calls it out so readers are aware.

Aside from this, all other aspects of the language should be the same.
Refer to the [Lua 5.4 manual][] for details.

[Garbage-Collection Metamethods]: https://www.lua.org/manual/5.4/manual.html#2.5.3
[Weak tables]: https://www.lua.org/manual/5.4/manual.html#2.5.4

## Standard Libraries

zb provides the following standard libraries from Lua:

- The [basic functions][] (globals and as `_G`)
- The [math library][] (`math`)
- The [string manipulation library][] (`string`)
- The [table manipulation library][] (`table`)
- The [UTF-8 library][] (`utf8`)
- The [operating system library][] (`os`), albeit in a very limited capacity

Each of these libraries are available as globals and do not require importing.
Unless otherwise noted, the behavior of every symbol in this section
is as documented in the [Lua 5.4 manual][].

[basic functions]: https://www.lua.org/manual/5.4/manual.html#6.1
[math library]: https://www.lua.org/manual/5.4/manual.html#6.7
[operating system library]: https://www.lua.org/manual/5.4/manual.html#6.9
[string manipulation library]: https://www.lua.org/manual/5.4/manual.html#6.4
[table manipulation library]: https://www.lua.org/manual/5.4/manual.html#6.6
[UTF-8 library]: https://www.lua.org/manual/5.4/manual.html#6.5

### Basics (`_G`)

The following [basic functions][] are available as globals:

- [`assert`](https://www.lua.org/manual/5.4/manual.html#pdf-assert)
- [`error`](https://www.lua.org/manual/5.4/manual.html#pdf-error)
- [`getmetatable`](https://www.lua.org/manual/5.4/manual.html#pdf-getmetatable)
- [`ipairs`](https://www.lua.org/manual/5.4/manual.html#pdf-ipairs)
- [`load`](https://www.lua.org/manual/5.4/manual.html#pdf-load)
- [`next`](https://www.lua.org/manual/5.4/manual.html#pdf-next)
- [`pairs`](https://www.lua.org/manual/5.4/manual.html#pdf-pairs)
- [`pcall`](https://www.lua.org/manual/5.4/manual.html#pdf-pcall)
- [`rawequal`](https://www.lua.org/manual/5.4/manual.html#pdf-rawequal)
- [`rawget`](https://www.lua.org/manual/5.4/manual.html#pdf-rawget)
- [`rawlen`](https://www.lua.org/manual/5.4/manual.html#pdf-rawlen)
- [`rawset`](https://www.lua.org/manual/5.4/manual.html#pdf-rawset)
- [`select`](https://www.lua.org/manual/5.4/manual.html#pdf-select)
- [`setmetatable`](https://www.lua.org/manual/5.4/manual.html#pdf-setmetatable)
- [`tonumber`](https://www.lua.org/manual/5.4/manual.html#pdf-tonumber)
- [`tostring`](https://www.lua.org/manual/5.4/manual.html#pdf-tostring)
- [`type`](https://www.lua.org/manual/5.4/manual.html#pdf-type)
- [`warn`](https://www.lua.org/manual/5.4/manual.html#pdf-warn)
- [`xpcall`](https://www.lua.org/manual/5.4/manual.html#pdf-xpcall)

The following variables are available as globals:

- [`_G`](https://www.lua.org/manual/5.4/manual.html#pdf-_G)
- [`_VERSION`](https://www.lua.org/manual/5.4/manual.html#pdf-_VERSION)

Intentionally absent are:

- [`collectgarbage`](https://www.lua.org/manual/5.4/manual.html#pdf-collectgarbage)
- [`dofile`](https://www.lua.org/manual/5.4/manual.html#pdf-dofile)
- [`loadfile`](https://www.lua.org/manual/5.4/manual.html#pdf-loadfile)

[`print`](https://www.lua.org/manual/5.4/manual.html#pdf-print)
is currently missing, but [planned](https://github.com/256lights/zb/issues/40).

### Mathematics

The following symbols are available in the [`math` library][math library]:

- [`abs`](https://www.lua.org/manual/5.4/manual.html#pdf-math.abs)
- [`acos`](https://www.lua.org/manual/5.4/manual.html#pdf-math.acos)
- [`asin`](https://www.lua.org/manual/5.4/manual.html#pdf-math.asin)
- [`atan`](https://www.lua.org/manual/5.4/manual.html#pdf-math.atan)
- [`ceil`](https://www.lua.org/manual/5.4/manual.html#pdf-math.ceil)
- [`cos`](https://www.lua.org/manual/5.4/manual.html#pdf-math.cos)
- [`deg`](https://www.lua.org/manual/5.4/manual.html#pdf-math.deg)
- [`exp`](https://www.lua.org/manual/5.4/manual.html#pdf-math.exp)
- [`tointeger`](https://www.lua.org/manual/5.4/manual.html#pdf-math.tointeger)
- [`floor`](https://www.lua.org/manual/5.4/manual.html#pdf-math.floor)
- [`fmod`](https://www.lua.org/manual/5.4/manual.html#pdf-math.fmod)
- [`ult`](https://www.lua.org/manual/5.4/manual.html#pdf-math.ult)
- [`log`](https://www.lua.org/manual/5.4/manual.html#pdf-math.log)
- [`max`](https://www.lua.org/manual/5.4/manual.html#pdf-math.max)
- [`min`](https://www.lua.org/manual/5.4/manual.html#pdf-math.min)
- [`modf`](https://www.lua.org/manual/5.4/manual.html#pdf-math.modf)
- [`rad`](https://www.lua.org/manual/5.4/manual.html#pdf-math.rad)
- [`sin`](https://www.lua.org/manual/5.4/manual.html#pdf-math.sin)
- [`sqrt`](https://www.lua.org/manual/5.4/manual.html#pdf-math.sqrt)
- [`tan`](https://www.lua.org/manual/5.4/manual.html#pdf-math.tan)
- [`type`](https://www.lua.org/manual/5.4/manual.html#pdf-math.type)
- [`pi`](https://www.lua.org/manual/5.4/manual.html#pdf-math.pi)
- [`huge`](https://www.lua.org/manual/5.4/manual.html#pdf-math.huge)
- [`maxinteger`](https://www.lua.org/manual/5.4/manual.html#pdf-math.maxinteger)
- [`mininteger`](https://www.lua.org/manual/5.4/manual.html#pdf-math.mininteger)

Intentionally absent are:

- [`random`](https://www.lua.org/manual/5.4/manual.html#pdf-math.random)
- [`randomseed`](https://www.lua.org/manual/5.4/manual.html#pdf-math.randomseed)

### String Manipulation

The following symbols are available in the [`string` library][string manipulation library]:

- [`byte`](https://www.lua.org/manual/5.4/manual.html#pdf-string.byte)
- [`char`](https://www.lua.org/manual/5.4/manual.html#pdf-string.char)
- [`find`](https://www.lua.org/manual/5.4/manual.html#pdf-string.find)
- [`format`](https://www.lua.org/manual/5.4/manual.html#pdf-string.format)
- [`gmatch`](https://www.lua.org/manual/5.4/manual.html#pdf-string.gmatch)
- [`gsub`](https://www.lua.org/manual/5.4/manual.html#pdf-string.gsub)
- [`len`](https://www.lua.org/manual/5.4/manual.html#pdf-string.len)
- [`lower`](https://www.lua.org/manual/5.4/manual.html#pdf-string.lower)
- [`match`](https://www.lua.org/manual/5.4/manual.html#pdf-string.match)
- [`pack`](https://www.lua.org/manual/5.4/manual.html#pdf-string.pack)
- [`packsize`](https://www.lua.org/manual/5.4/manual.html#pdf-string.packsize)
- [`rep`](https://www.lua.org/manual/5.4/manual.html#pdf-string.rep)
- [`reverse`](https://www.lua.org/manual/5.4/manual.html#pdf-string.reverse)
- [`sub`](https://www.lua.org/manual/5.4/manual.html#pdf-string.sub)
- [`upper`](https://www.lua.org/manual/5.4/manual.html#pdf-string.upper)

Intentionally absent is:

- [`dump`](https://www.lua.org/manual/5.4/manual.html#pdf-string.dump)

[`string.unpack`](https://www.lua.org/manual/5.4/manual.html#pdf-string.unpack)
is currently missing but [planned](https://github.com/256lights/zb/issues/79).

zb also sets a metatable for strings where the `__index` field points to the `string` table.

[Patterns][] behave a little differently in zb's implementation
in order to avoid pathological runtime performance and clean up some confusing behaviors.
Patterns do not support backreferences (i.e. `%0` - `%9`) or balances (i.e. `%b`).
Attempting to use either of these pattern items will raise an error.
In patterns, character sets with classes in ranges (e.g. `[%a-z]`)
raise an error instead of silently exhibiting undefined behavior.
However, ranges using escapes (e.g. ``[%]-`]``) are well-defined in this implementation.

[Patterns]: https://www.lua.org/manual/5.4/manual.html#6.4.1

### Table Manipulation

All of the symbols in the [`table` library][table manipulation library] are available:

- [`concat`](https://www.lua.org/manual/5.4/manual.html#pdf-table.concat)
- [`insert`](https://www.lua.org/manual/5.4/manual.html#pdf-table.insert)
- [`move`](https://www.lua.org/manual/5.4/manual.html#pdf-table.move)
- [`pack`](https://www.lua.org/manual/5.4/manual.html#pdf-table.pack)
- [`remove`](https://www.lua.org/manual/5.4/manual.html#pdf-table.remove)
- [`sort`](https://www.lua.org/manual/5.4/manual.html#pdf-table.sort)
- [`unpack`](https://www.lua.org/manual/5.4/manual.html#pdf-table.unpack)

### UTF-8

All of the symbols in the [`utf8` library][UTF-8 library] are available:

- [`char`](https://www.lua.org/manual/5.4/manual.html#pdf-utf8.char)
- [`charpattern`](https://www.lua.org/manual/5.4/manual.html#pdf-utf8.charpattern)
- [`codepoint`](https://www.lua.org/manual/5.4/manual.html#pdf-utf8.codepoint)
- [`codes`](https://www.lua.org/manual/5.4/manual.html#pdf-utf8.codes)
- [`len`](https://www.lua.org/manual/5.4/manual.html#pdf-utf8.len)
- [`offset`](https://www.lua.org/manual/5.4/manual.html#pdf-utf8.offset)

### Operating System

The only symbol available in the [`os` library][operating system library]
is [`os.getenv`](https://www.lua.org/manual/5.4/manual.html#pdf-os.getenv).
`os.getenv` only operates on an allow-list of variables permitted by the user
and returns `fail` for all other variables.

## Dependency Information in Strings

Lua strings in zb that represent a store path carry extra dependency information
that is used when creating store objects derived from those strings.
For example, passing a string returned from the `path` function
into the `derivation` function will add the store path as an input to the derivation.
Similarly, passing the `out` field of a derivation object to another `derivation` function call
will add the derivation object as a build dependency of the new derivation.

String dependency information is not directly accessible in the Lua environment,
but its effects are observable.
The standard Lua functions and operators that manipulate strings are aware of such dependency information
and will preserve them where possible.
Examples of this include:

- Using the `..` operator to concatenate two strings
  will include the dependency information of both operands.
- The string returned by `string.format` will include dependency information
  from its arguments.
- Substrings from `string.match` or `string.sub` will include dependency information
  from the input string, if applicable.

The notable exception is if a string is serialized and deserialized in some way
(e.g. with `string.byte`), the dependency information will be stripped.
This is not a common thing to do in most build configurations,
but doing so can cause the dependency graph to be incorrect.

## Hash Strings

Certain zb functions take a *hash string* argument.
Hash strings are the result of a [cryptographic hash function][]
and are used to ensure integrity of an output.
Hash strings are either in the format `<type>:<base16|base32|base64>`
or the [Subresource Integrity hash expression][] format `<type>-<base64>`,
where `<type>` is one of `md5`, `sha1`, `sha256`, or `sha512`.

[cryptographic hash function]: https://en.wikipedia.org/wiki/Cryptographic_hash_function
[Subresource Integrity hash expression]: https://www.w3.org/TR/SRI/#the-integrity-attribute

## zb-specific extensions

This section documents the functions that zb adds to its Lua environment.
All the functions in this section are available as globals.

### `path`

`path(p)` copies the file, directory, or symbolic link at `p` into the store.
This is a common way of loading a program's source code into zb.
`p` can be either an absolute path or a path relative to the Lua file that called `path`.
Slash-separated relative paths are accepted on all platforms.
`path` returns a string containing the absolute path to the imported store object.

`path` can alternatively take in a table as its sole argument
with the following fields:

- `path` (required string): The meaning is the same as the string argument form of `path`.
- `name` (optional string): The name to use for the store object (excluding the digest).
  If omitted, then the last path component of `path` is used as the name.
- `filter` (optional function): If `filter` is given and `path` names a directory,
  then `path` calls `filter` for each file, directory, or symlink inside the directory.
  The first argument to the `filter` function is a slash-separated path
  relative to the top of the directory of the file currently being filtered.
  The second argument to the `filter` function
  is one of `"regular"`, `"directory"`, or `"symlink"`
  to indicate the type of the file.
  If the filter function returns `nil` or `false`,
  then the file will be excluded from import into the store.
  The default behavior of `path` is equivalent to passing `filter = function() return true end`.

### `derivation`

`derivation` adds a `.drv` file to the store specifying a derivation that can be built.
`derivation` takes a table as its sole argument.
All fields in the table are passed to the `builder` program as environment variables.
The values can be strings, numbers, booleans, or lists of any of the previous types.
A string is used as-is.
Numbers are converted to strings.
`false` is converted to the empty string; `true` is converted to the string `1`.
Each item in a list is converted to a string and then joined with a single space.

The following fields in the table passed to `derivation` are treated specially:

- `name` (required string): The name to use for the derivation
  and the resulting store object (excluding the digest and the `.drv` extension).
- `system` (required string): The triple that the derivation can run on.
- `builder` (required string): The path to the program to run.
- `args` (optional list of strings): The arguments to pass to the `builder` program.
- `outputHash` (optional string): If given, the derivation is a fixed-output derivation.
  This argument is a hash string for the derivation's output,
  with its exact meaning determined by `outputHashMode` (see below).
  Derivations with the same `name`, `outputHash`, and `outputHashMode` are considered interchangable,
  regardless of the other fields in the derivation.
- `outputHashMode` (optional string): This field must not be set unless `outputHash` is also set.
  The value of the field must be one of `flat` (the default) or `recursive`.
  If the value of the field is `flat` or not set,
  then the builder must produce a single file and its contents,
  when hashed with the algorithm given by `outputHash`,
  must produce the same hash as `outputHash`.
  If the value of the field is `recursive`,
  then the builder's output,
  when serialized as a NAR file and hashed with the algorithm given by `outputHash`,
  must produce the same hash as `outputHash`.
- `__network` (optional boolean): If `true`, then the derivation is given network access.
  It is assumed that the derivation is still deterministic.

The `derivation` function returns a derivation object.
This object will have a copy of all the fields of the table passed into the `derivation` function that produced it,
plus a few extra fields:

- `drvPath`: a string containing the absolute path to the resulting `.drv` file in the store.
- `out`: a placeholder string that represents the absolute path to the derivation's output.
  Passing this string (or strings formed from it) into other calls to `derivation`
  will implicitly add a build dependency between the derivations.
  (See the prior section on dependency information for details.)

For convenience, using a derivation object in places that expect a string
(e.g. concatenation or a call to `tostring`)
will be treated the same as accessing the `out` field.

### `fetchurl`

`fetchurl` returns a derivation that downloads a URL.
`fetchurl` takes a table as its sole argument
with the following fields:

- `url` (required string): The URL to download.
- `hash` (required string): A hash string of the file's content.
- `name` (optional string): The name to use for the store object (excluding the digest).
  If omitted, then the last path component of the `url` is used as the name.
- `executable` (optional boolean): Whether the file should be marked as executable.
  If true, then the NAR serialization is used to compute the `hash` instead of the file content.

### `extract`

`extract` returns a derivation that extracts an archive file.
`extract` takes a table as its sole argument
with the following fields:

- `src` (required string): Path to the file.
- `name` (optional string): The name to use for the store object (excluding the digest).
  If omitted, then the last path component of the `src` without the file extension is used as the name.
- `stripFirstComponent` (optional boolean): If true or omitted,
  then the root directory is stripped during extraction.

The source must be in one of the following formats:

- .tar
- .tar.gz
- .tar.bz2
- .zip

The algorithm used to extract the archive is selected based on the first few bytes of the file.

### `fetchArchive`

`fetchArchive` returns a derivation that extracts an archive from a URL.
This is a convenience wrapper around `fetchurl` and `extract`.
`fetchArchive` takes a table as its sole argument
with the following fields:

- `url` (required string): The URL to download.
- `hash` (required string): The hash string of the archive's content
  (not the extracted store object).
- `name` (optional string): The name to use for the store object (excluding the digest).
  If omitted, then the last path component of the `url` is used as the name.
- `stripFirstComponent` (optional boolean): If true or omitted,
  then the root directory is stripped during extraction.

### `import`

`import(path)` reads the Lua file at the given path and executes it asynchronously.
Every Lua file that zb encounters is treated as a separate module.
This is similar to the `dofile` and `require` functions in standalone Lua
(which are not supported in zb),
but `import` is special in a few ways:

- `import` will load the module for any given path at most once during a run of `zb`.
- `import` does not execute the module right away.
  Instead, `import` returns a placeholder object that acts like the module.
  When you do anything with the placeholder object other than pass it around,
  it will then wait for the module to finish initialization.
  `await` can be used to access the value.
- Globals are not shared among modules.
  Setting a "global" variable in a zb module will place it in a table
  which is implicitly returned by `import`
  if the module does not return any values.
- Everything in a module will be "frozen" when the end of the file is reached.
  This means that any changes to variables or tables (even locals)
  will raise an error.
- If `path` is a path constructed from a derivation,
  then zb will build the derivation before attempting to read it.

### `await`

`await(x)` forces a module (as returned by `import`) to load
and returns its value.
If `x` is not a module, then `x` is returned as-is.

### `toFile`

`toFile(name, s)` creates a non-executable file in the store
with the given file name (excluding the digest)
and the content given by the string.
`toFile` returns a string containing the absolute path to the store file.

### `storePath`

`storePath(path)` adds a dependency on an existing store path.
`path` must be an absolute path.
If the store object named by `path` does not exist in the store,
`storePath` raises an error.
`storePath` returns a string that is equivalent to its argument
but includes the dependency information necessary
for it to be correctly interpreted as a store path.
(See the prior section on dependency information for details.)

`storePath` is used to reference store objects that are created outside the zb build
and imported into the store.
Most users should avoid this function, as it is mostly intended for bootstrapping.

### `storeDir`

`storeDir` is a string constant with the running evaluator's store directory
(e.g. `/zb/store` or `C:\zb\store`).
