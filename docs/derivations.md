# Derivation Specification

A zb build is composed of one or more *derivations* which run *builder programs*.
A derivation's canonical form is its `.drv` file,
which shares the same syntax as [Nix's derivation file format][].
This document describes the `.drv` file format
and the environment in which a derivation's builder program executes.

Users of zb typically will not directly create `.drv` files themselves.
Instead, they use the [`derivation` Lua function][],
which produces a `.drv` file under the hood.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this
document are to be interpreted as described in [RFC 2119][].
Syntax is defined in Augmented Backus-Naur Form as described in [RFC 5234][] and [RFC 7405][].

[RFC 2119]: https://datatracker.ietf.org/doc/html/rfc2119
[RFC 5234]: https://datatracker.ietf.org/doc/html/rfc5234
[RFC 7405]: https://datatracker.ietf.org/doc/html/rfc7405
[Nix's derivation file format]: https://nix.dev/manual/nix/2.24/protocols/derivation-aterm
[`derivation` Lua function]: lua.md

## `.drv` format

The `.drv` format uses a subset of the Textual [ATerm][] Format.
The full syntax is as follows:

```abnf
drv-file = %s"Derive" "(" 
           outputs ","
           input-derivations ","
           input-sources ","
           system ","
           builder ","
           args ","
           env ")"

outputs = "[" output *("," output) "]"
          ; output names MUST be in lexicographically ascending order
          ; and MUST NOT contain duplicates
output = floating-output / fixed-output
fixed-output = "(" output-name "," output-path "," output-algo "," output-hash ")"
floating-output = "(" output-name "," 2DQUOTE "," output-algo "," 2DQUOTE ")"
output-name = non-empty-string
output-path = non-empty-string  ; a full store path
output-algo = DQUOTE ca-method hash-type DQUOTE
output-hash = DQUOTE *( 2hex-digit ) DQUOTE
hex-digit   = DIGIT / %s"a" / %s"b" / %s"c" / %s"d" / %s"e" / %s"f"

ca-method =             ; flat file, hash by file contents
ca-method =/ %s"r:"     ; recursive file, hashed by NAR serialization
ca-method =/ %s"text:"  ; a text file, hashed by file contents

hash-type = %s"md5" / %s"sha1" / %s"sha256" / %s"sha512"

input-derivations = "[" [ input-derivation *("," input-derivation) ] "]"
                    ; input derivation paths MUST be in
                    ; lexicographically ascending order
                    ; and MUST NOT contain duplicates
input-derivation = "(" input-derivation-path "," "[" output-name *( "," output-name ) "]" ")"
                   ; output names MUST be in lexicographically ascending order
                   ; and MUST NOT contain duplicates
input-derivation-path = non-empty-string  ; a full store path

input-sources = "[" [ input-source *("," input-source) ] "]"
                ; input sources MUST be in lexicographically ascending order
                ; and MUST NOT contain duplicates
input-source = non-empty-string  ; a full store path

system = non-empty-string
builder = non-empty-string
args = "[" [ string *("," string) ] "]"

env = "[" [ env-var *("," env-var) ] "]"
      ; environment variable names MUST be in lexicographically ascending order
      ; and MUST NOT contain duplicates
env-var = "(" env-var-name "," env-var-value ")"
env-var-name = non-empty-string
env-var-value = string

string = DQUOTE *(string-char / string-escape) DQUOTE
non-empty-string = DQUOTE 1*(string-char / string-escape) DQUOTE

string-char = %x00-08 / %x0B-0C / %x0E-21 / %x23-5B / %x5D-FF
              ; OCTET except "\", DQUOTE, LF, CR, or HTAB

string-escape = "\\" / "\" DQUOTE / %s"\n" / %s"\r" / %s"\t"
```

In summary, a `.drv` file consists of:

- One or more *outputs*.
  Each output has a name.
  Each output can be either a floating output or a fixed output.
  Fixed outputs have a predetermined hash;
  builders **MUST** produce a file that matches the fixed output hash.
- Zero or more *input derivations*.
  A derivation's input derivations **MUST** be built before running the derivation's builder.
- Zero or more *input sources*.
- A [*system* triple][].
- A *builder* program path.
  zb **MUST** use this string as `argv[0]` on Unix systems.
- Zero or more *builder arguments*.
- Zero or more *environment variables*.
  These are string key/value pairs.
  See the "Environment Variables" section below for the semantics.

[ATerm]: https://doi.org/10.1002/(SICI)1097-024X(200003)30:3<259::AID-SPE298>3.0.CO;2-Y
[*system* triple]: ../internal/system/README.md

## Placeholders

Environment variable values, the builder string, and builder arguments
within the `.drv` file **MAY** contain placeholders
for their own outputs or their input derivations' outputs.
(This mechanism is used in the [`derivation` Lua function][]
to provide the `out` environment variable, for example.)
zb **SHALL** treat each placeholder string present in these fields
as if it was replaced with the absolute path to the corresponding store object.

Placeholders are a [Nix-Base-32-encoded][] hash preceded by a slash (`/`).
Output placeholders use the SHA-256 hash of `nix-output:` followed by the output name.
For example, the placeholder for the output named `out`
is `/1rz4g4znpzjwh1xymhjpm42vipw92pr73vdgl6xs1hycac8kf2n9`.
Input derivation output placeholders use the SHA-256 hash
of `nix-upstream-output:`,
followed by the input derivation's digest,
followed by a colon (`:`),
followed by the input derivation's name with the trailing `.drv` removed,
and if the output name is not `out`,
then followed by a hyphen (`-`) and followed by the output name.

[Nix-Base-32-encoded]: https://edolstra.github.io/pubs/phd-thesis.pdf#page=97

## Environment Variables

zb **MUST** pass every environment variable that was specified in the `.drv` file
to builder programs after applying the placeholder substitution process described above.
zb **MAY** pass environment variables to the builder
in addition to those specified in the `.drv` file.
However, zb **MUST NOT** override the environment variables specified in the `.drv` files.
zb **SHOULD** pass the following environment variables to the builder on all systems:

| Variable         | Description                                         | Example           |
| :--------------- | :-------------------------------------------------- | :---------------- |
| `ZB_BUILD_CORES` | A hint as to the number of CPU cores to use.        | `4`               |
| `ZB_BUILD_TOP`   | The absolute path to the temporary build directory. | `/build`          |
| `ZB_STORE`       | The absolute path to the store directory            | `/opt/zb/store`   |

On Unix-like systems, zb **SHOULD** pass the additional following environment variables to the builder:

| Variable         | Description                                         | Example           |
| :--------------- | :-------------------------------------------------- | :---------------- |
| `HOME`           | The fixed value `/home-not-set`.                    | `/home-not-set`   |
| `PATH`           | The fixed value `/path-not-set`.                    | `/path-not-set`   |
| `TEMP`           | The absolute path to the temporary build directory. | `/build`          |
| `TEMPDIR`        | The absolute path to the temporary build directory. | `/build`          |
| `TMP`            | The absolute path to the temporary build directory. | `/build`          |
| `TMPDIR`         | The absolute path to the temporary build directory. | `/build`          |

On Windows systems, zb **SHOULD** pass the additional following environment variables to the builder:

| Variable         | Description                                         | Example           |
| :--------------- | :-------------------------------------------------- | :---------------- |
| `HOME`           | The fixed value `C:\home-not-set`.                  | `C:\home-not-set` |
| `PATH`           | The fixed value `C:\path-not-set`.                  | `C:\path-not-set` |
| `TEMP`           | The absolute path to the temporary build directory. | `C:\Users\foo\AppData\Local\Temp\build123` |
| `TMP`            | The absolute path to the temporary build directory. | `C:\Users\foo\AppData\Local\Temp\build123` |

zb **MAY** use environment variables that start with two underscores (`__`)
to control aspects of execution or signal special things about the target.
Users **SHOULD NOT** use environment variables that start with two underscores
for other purposes.

## Builder Success

For zb to consider a builder's run successful,
the builder **MUST** exit with a successful code (typically 0)
and the builder **MUST** create a file, directory, or symlink
for each of its outputs.
Otherwise, zb **SHALL** report the builder run as a failure.

## Filesystem

zb **MAY** restrict the filesystem available to a builder to a limited subset
in order to promote reproducibility of builds across machines.
zb **MUST** make the contents of each input derivation's output
and each input source named in the `.drv` file
available during the execution of the builder
at their canonical store paths.

zb **MUST** create an empty directory that is unique to the builder's run
before starting the builder.
That directory will be the builder's initial working directory
and its absolute path will be used as the default value of `ZB_BUILD_TOP` as described above.

A `.drv` file **MAY** include an environment variable called `__buildSystemDeps`.
zb **SHALL** interpret such an environment variable as a space-separted list of files or directories
that **SHALL** be available to the builder when it runs.
If any files or directories named in `__buildSystemDeps` do not exist
or zb's security policy does not permit access to the requested file,
zb **MUST NOT** run the builder and **MUST** report the builder run as a failure.

## Network Access

If the derivation produces a fixed output
or the `__network` environment variable is set to `1` in the `.drv` file,
then the builder program **SHOULD** have access to the builder machine's network interfaces.
Otherwise, the builder program **SHOULD NOT** have access to network interfaces
other than the loopback interface.
The loopback network interface **MUST** always be available during a builder's run.
