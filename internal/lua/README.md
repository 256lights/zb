# `zombiezen.com/go/lua`

(Copied from commit `290725fb91409ef842c6338495cbb0460b9b0c78` of `zombiezen.com/go/lua`.)

This is [Lua 5.4.6](https://www.lua.org/versions.html#5.4), wrapped as a Go package.

It's experimental and suited to fit my needs.

## Install

```shell
go get zombiezen.com/go/lua
```

## Getting Started

```go
import "zombiezen.com/go/lua"

// Create an execution environment
// and make the standard libraries available.
state := new(lua.State)
defer state.Close()
if err := lua.OpenLibraries(state); err != nil {
  return err
}

// Load Lua code as a chunk/function.
// Calling this function then executes it.
const luaSource = `print("Hello, World!")`
if err := state.LoadString(luaSource, luaSource, "t"); err != nil {
  return err
}
if err := state.Call(0, 0, 0); err != nil {
  return err
}
```

## License

[MIT](LICENSE) for compatibility with Lua itself.
