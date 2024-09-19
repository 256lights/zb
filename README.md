# zb

zb is an experiment in hermetic, reproducible build systems.
It has not stabilized and should not be used for production purposes.

zb is based on the ideas in [The purely functional software deployment model by Eelco Dolstra][dolstra_purely_2006]
and [Build systems à la carte][mokhov_build_2018],
as well as the author's experience in working with build systems.
The build model is mostly the same as in [Nix](https://nixos.org/),
but build targets are configured in [Lua](https://www.lua.org/)
instead of a domain-specific language.

For more motivation on the development of zb,
see the early blog posts:

- [zb: A Build System Prototype](https://www.zombiezen.com/blog/2024/06/zb-build-system-prototype/)
- [zb: An Early-Stage Build System](https://www.zombiezen.com/blog/2024/09/zb-early-stage-build-system/)

[dolstra_purely_2006]: https://edolstra.github.io/pubs/phd-thesis.pdf
[mokhov_build_2018]: https://doi.org/10.1145/3236774

## Examples

The [hello world](demo/hello.lua) example:

```lua
return derivation {
  name    = "hello";
  infile  = path "hello.txt";
  builder = "/bin/sh";
  system  = "x86_64-linux";
  args    = {"-c", "while read line; do echo \"$line\"; done < $infile > $out"};
}
```

Other examples:

- [Multi-step builds](demo/multistep.lua)
- [`stage0-posix/x86_64-linux.lua`](demo/stage0-posix/x86_64-linux.lua),
  which uses the [stage0-posix project](https://github.com/oriansj/stage0-posix)
  to build a minimal userspace (including a rudimentary C compiler).
- [`bootstrap.lua`](demo/bootstrap.lua),
  which follows the [live-bootstrap project](https://github.com/fosslinux/live-bootstrap/) steps
  to build a more complete userspace.

## Getting Started

Prerequisites:

- Knowledge of using the command-line for your OS (e.g. Terminal.app, Command Prompt, etc.)
- [Go](https://go.dev/dl/) 1.23 or later.

### Linux or macOS

1. `sudo mkdir /zb && sudo chown $(id -u):$(id -g) /zb`
2. Clone this repository to your computer and `cd` into it.
3. `go build ./cmd/zb`
4. Start the build server (only on startup): `./zb serve &`
5. Run a build: `./zb build --file demo/hello.lua`

You can use `./zb --help` to get more information on commands.

### Windows

Must be running Windows 10 or later,
since zb depends on Windows support for Unix sockets.

1. Install [MinGW-w64](https://www.mingw-w64.org/).
   If you're using the [Chocolatey package manager](https://community.chocolatey.org/),
   you can run `choco install mingw`.
2. Create a `C:\zb` directory.
3. Clone this repository to your computer and `cd` into it.
4. `go build .\cmd\zb`
5. Start the build server in one terminal: `.\zb.exe serve`
6. Run a build in another terminal: `.\zb.exe build --file demo/hello_windows.lua`

### Next Steps

zb uses a slightly modified version of Lua 5.4.
The primary difference is that strings
(like those returned from the `path` function
or the `.out` field of a derivation)
can carry dependency information,
like in the Nix expression language.
This is largely hidden from the user.
From there, the following libraries are available:

- [Basic functions](https://www.lua.org/manual/5.4/manual.html#6.1)
- The [`table` module](https://www.lua.org/manual/5.4/manual.html#6.6)
- [Additional functions](zb_defs.lua), such as `path` and `derivation`.
  These are intentionally similar to the [Nix built-in functions](https://nixos.org/manual/nix/stable/language/builtins.html).

## Objectives

- Prove that Lua is a viable alternative to a domain-specific build language. (Done!)
- Exclusively use content-addressed outputs. (Done!)
  This enables shallow builds, as described in [Build systems à la carte][mokhov_build_2018].
- Establish a [source bootstrap](https://bootstrappable.org/benefits.html)
  that is equivalent to the [nixpkgs standard environment](https://nixos.org/manual/nixpkgs/unstable/#chap-stdenv).
  ([Partially implemented](demo/bootstrap.lua)
  by following the [live-bootstrap](https://github.com/fosslinux/live-bootstrap/) steps.
  See [#30](https://github.com/256lights/zb/issues/30) for ongoing work.)

## License

[MIT](LICENSE)
