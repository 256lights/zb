# zombiezen build (zb) tool

zb is an experiment in hermetic, reproducible build systems.
It is a prototype and should not be used for production purposes.

zb is based on the ideas in [The purely functional software deployment model by Eelco Dolstra][dolstra_purely_2006]
and [Build systems à la carte][mokhov_build_2018],
as well as the author's experience in working with build systems.
The build model is the same as in [Nix](https://nixos.org/),
but build targets are configured in [Lua](https://www.lua.org/)
instead of a domain-specific language.

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

1. [Install Nix](https://nixos.org/download/) (used as a temporary build backend, see Caveats below)
2. `go build ./cmd/zb`
3. `./zb build --file demo/hello.lua`

You can use `./zb --help` to get more information on commands.

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
  by following the [live-bootstrap](https://github.com/fosslinux/live-bootstrap/) steps.)
- Permit optional interoperability with the nixpkgs ecosystem.
  (Not implemented yet: [#2](https://github.com/zombiezen/zb/issues/2))

## Caveats

The following is a list of shortcuts taken for the zb prototype.

- zb requires Nix to be installed.
  zb acts as a frontend over `nix-store --realise` to actually run the builds,
  but this dependency could be removed in the future.
- zb was written in Go for speed of development.
  This makes self-hosting more complex,
  but there's nothing preventing a more production-ready implementation in C/C++.
- Files and derivations are imported into the Nix store immediately instead of as-needed.
  This makes evaluation slow.
  A better implementation would check whether the files' `stat` information
  has changed since the last import attempt
  (like in [Avery Pennarun's redo][pennarun_mtime_2018]).
  Another improvement would be to implement paths as a lazy type
  that only causes a store import when the `__tostring` metamethod is called.
- The Lua `next` and `pairs` functions should sort keys to be deterministic.
- Need to stabilize the Lua standard library that's available.
  `string.format` specifically would be good,
  but would require some fancy work to support the "context" dependency feature.
- The [stage0 demo](demo/stage0-posix/x86_64-linux.lua) is not entirely hermetic,
  since it uses the host's `/bin/sh`.
  Hypothetically, the demo could use the included kaem shell
  if kaem could support `$out` expansion.
- The `derivation` built-in does not support the `outputs` parameter
  to declare multiple outputs.
- In the `demo` directory, most all derivations are in a single file.
  A more full standard library would [split up files](https://github.com/zombiezen/zb/issues/4).

[pennarun_mtime_2018]: https://apenwarr.ca/log/20181113

## License

[MIT](LICENSE)
