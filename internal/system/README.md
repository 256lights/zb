# `system` values specification

(The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this
document are to be interpreted as described in [RFC 2119][].
Syntax is defined in Augmented Backus-Naur Form as described in [RFC 5234][].)

The `system` value of a derivation is used to specify a class of machines that can execute the derivation's builder.
The format is intentionally compatible with [LLVM target triples][]
(which are, in turn, similar to GCC target triples).
`system` values are a hyphen-separated collection of architecture, vendor, operating system, and environment components.
The syntax of a `system` value is:

```abnf
system =  arch [subarch] ["-" vendor] "-" os [os_version] [ "-" env [env_version] ]
system =/ arch [subarch] ["-" vendor] "-" %63.79.67.77.69.6E ; "cygwin", case sensitive.
                     ; Translates to os=windows, env=cygnus
system =/ arch [subarch] ["-" vendor] "-" %6D.69.6E.67.77    ; "mingw", case sensitive.
                     ; Translates to os=windows, env=gnu

arch        = word
subarch     = word   ; Reserved for future use, particularly for ARM.
vendor      = word
os          = word
os_version  = word   ; Semantics are specific to os.
env         = word
env_version = word   ; Semantics are specific to os and env.

word = *(ALPHA / DIGIT / "_")
```

`system` value parsers **MAY** permit constants beyond the ones in the tables below,
but parsers **MUST** be able to recognize the constants below.
Additional constants **MUST** follow the `word` pattern.

`system` value parsers **MUST NOT** permit reordering of the components.
If exactly three hyphen-separated components are present and the second component starts with an `os` known to the parser,
then the second component **MUST** be interpreted as the operating system
and the third component **MUST** be interpreted as the environment.
Otherwise, the second component **MUST** be interpreted as the vendor
and the third component **MUST** be interpreted as the operating system.

[RFC 2119]: https://datatracker.ietf.org/doc/html/rfc2119
[RFC 5234]: https://datatracker.ietf.org/doc/html/rfc5234
[LLVM target triples]: https://clang.llvm.org/docs/CrossCompilation.html#target-triple

## Operating Systems

The `os` **SHOULD** be one of the constants (case-sensitive) from the "zb name" column in the following table:

| zb name   | LLVM name | Go name   | Description                          |
| :-------- | :-------- | :-------- | :----------------------------------- |
| `linux`   | `linux`   | `linux`   | Linux                                |
| `macos`   | `macos`   | `darwin`  | macOS (preferred new spelling)       |
| `darwin`  | `darwin`  | `darwin`  | macOS (compatibility spelling)       |
| `ios`     | `ios`     | `ios`     | iOS                                  |
| `windows` | `windows` | `windows` | Windows                              |

## Vendors

The `vendor` **SHOULD** be one of the constants (case-sensitive) from the "zb name" column in the following table:

| zb name   | Description                 |
| :-------- | :-------------------------- |
| `pc`      | Generic "personal computer" |
| `apple`   | Apple Inc.                  |
| `unknown` | Placeholder                 |

If a vendor is omitted, then the parser **MUST** infer the vendor based on the operating system.

- If the operating system is Darwin-like (e.g. `darwin`, `macos`, or `ios`),
  then the inferred vendor **MUST** be `apple`.
- If the operating system is Windows, then the inferred vendor **MUST** be `pc`.
- If the operating system is `linux`, then the inferred vendor **MUST** be `unknown`.
  (This is consistent with how LLVM parses target triples,
  but is a departure from GNU conventions,
  which use `pc` if the architecture is x86-based.)
- Otherwise, the inferred vendor **SHOULD** be `unknown`.
  Newer parsers may have knowledge of operating systems not documented here,
  so a particular vendor string may be appropriate.
  However, `unknown` is preferred
  for compatibility with parsers that don't have knowledge of those operating systems.

## Architectures

The `arch` rule **SHOULD** be one of the constants (case-sensitive) from the "zb name" column in the following table:

| zb name                     | LLVM name                   | Go name   | Description              |
| :-------------------------- | :-------------------------- | :-------- | :----------------------- |
| `i386`/`i486`/`i586`/`i686` | `i386`/`i486`/`i586`/`i686` | `386`     | Intel 32-bit             |
| `x86_64`                    | `x86_64`                    | `amd64`   | Intel 64-bit             |
| `arm`                       | `arm`                       | `arm`     | ARM 32-bit Little Endian |
| `aarch64`                   | `aarch64`                   | `arm64`   | ARM 64-bit Little Endian |
| `riscv32`                   | `riscv32`                   | `riscv`   | RISC-V 32-bit            |
| `riscv64`                   | `riscv64`                   | `riscv64` | RISC-V 64-bit            |

## Environments

The `env` rule **SHOULD** be one of the constants (case-sensitive) from the "zb name" column in the following table:

| zb name       | LLVM name     | Description                  |
| :------------ | :------------ | :--------------------------- |
| `android`     | `android`     | Android                      |
| `androideabi` | `androideabi` | Android ARM 32-bit           |
| `gnu`         | `gnu`         | GNU C Library                |
| `musl`        | `musl`        | musl                         |
| `msvc`        | `msvc`        | Microsoft Visual C++ Runtime |
| `cygnus`      | `cygnus`      | Cygwin                       |
| `unknown`     | `unknown`     | Placeholder                  |

If a vendor is omitted, then the parser **MUST** infer the vendor based on the operating system.
If the operating system is Windows, then the inferred environment **MUST** be `msvc`.
Otherwise, the inferred environment **SHOULD** be `unknown`.

When formatting a `system` value with a non-`unknown` environment and an `unknown` vendor,
the vendor **SHOULD NOT** be omitted.
This avoids any ambiguity when parsing the `system` value.

## Appendix: Test Suite

There is a parsing test suite available as a [JSON file](testdata/known_triples.jwcc)
and a [list of specifically invalid values](testdata/bad_triples.jwcc).
Parsers **MUST** produce results that match the data files.
