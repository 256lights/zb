# Getting Started

This introductory guide will help you run your first build with zb
and explain the basic concepts you will need to use zb.
zb is a build system that manages your dependencies
so that you can be confident that a build that works on one machine
works on any machine with the same operating system and CPU architecture.

## Prerequisites

This guide assumes:

- Familiarity with the command-line interface for your operating system.
  (For example, Terminal.app on macOS, Command Prompt or PowerShell on Windows, etc.)
- Knowledge of at least one programming language.
  We will be building a C program in this tutorial,
  but you do not need to know C or have any specific version of developer tools installed.
  Installing tools automatically is a key feature of zb!
- zb uses the [Lua programming language](https://www.lua.org/) to configure builds.
  Learning Lua is helpful for using zb.
  zb uses the Lua 5.4 language,
  with some standard libraries omitted to limit the complexity of builds.
  As such, any learning resources for Lua will be applicable to zb.

The standard library currently supports:

- `x86_64-unknown-linux`
- `aarch64-apple-macos`

## Installation

To install zb:

1. Go to the [latest zb release](https://github.com/256lights/zb/releases/latest)
   in your web browser.
2. Download the binary archive asset for your platform.
3. Extract the binary archive.
4. On Unix-like systems, run the `install` script.
   There is no installer for Windows yet.
   ([#82](https://github.com/256lights/zb/issues/82) tracks adding an installer.)
5. Once the installer finishes, you can delete the binary archive and the extracted directory.

On `aarch64-apple-macos`, the zb standard library requires additional setup.
See the [standard library README](https://github.com/256lights/zb-stdlib/blob/main/README.md) for details.

## Tutorial

Let's start with a C "Hello, World" program.
Open your editor of choice and enter the following into a new file `hello.c`:

```c
/* hello.c */
#include <stdio.h>

int main() {
  printf("Hello, World!\n");
  return 0;
}
```

Now let's learn how to build `hello.c` into an executable with zb.

We will write a small Lua script that describes the build,
and then use the zb command-line interface (CLI) to run the script.

Out of the box, zb only knows how to run programs and download source.
However, zb has a standard library that can be fetched
to provide tools for some common programming languages.
In your editor, enter the following into a new file `zb.lua`
in the same directory as `hello.c`:

```lua
-- zb.lua

-- Download the standard library.
local zb <const> = fetchArchive {
  url = "https://github.com/256lights/zb-stdlib/releases/download/v0.1.0/zb-stdlib-v0.1.0.tar.gz";
  hash = "sha256:dd040fe8baad8255e4ca44b7249cddfc24b5980f707a29c3b3e2b47f5193ea48";
}

-- Import modules from the standard library.
local stdenv = import(zb.."/stdenv/stdenv.lua")

-- Copy the source to the store.
local src = path {
  path = ".";
  name = "hello-source";
  filter = function(name)
    return name == "hello.c"
  end;
}

-- Create our build target.
for _, system in ipairs(stdenv.systems) do
  _G[system] = {
    hello = stdenv.makeDerivation {
      pname = "hello";
      src = src;
      buildSystem = system;

      buildPhase = "gcc -o hello hello.c";
      installPhase = '\z
        mkdir -p "$out/bin"\n\z
        mv hello "$out/bin/hello"\n';
    };
  }
end
```

Now we can build the program with `zb build`.
Note that the first time you run `zb build`,
will take a while,
since it is building the standard library tools from source.
(Because zb build artifacts can be safely shared among machines,
there are plans to speed this up.
[#43](https://github.com/256lights/zb/issues/43) tracks this work.)

```shell
zb build 'zb.lua#hello'
```

`zb build` takes in a URL of Lua file to run.
The fragment (i.e. everything after the `#`)
names a variable to build.
In this case, we're building `hello`.
`zb build` will automatically look for a global called `hello` defined inside `zb.lua`.
When it finds `nil`, then it looks for `hello`
inside a table with the same name as the currently running platform (e.g. `x86-unknown-linux`).

At the end, `zb build` will print the path to the directory it created,
something like `/opt/zb/store/2lvf1cavwkainjz32xzja04hfl5cimx6-hello`.
As you might expect from the `installPhase` we used above,
it will be inside the `bin` directory we created inside the output directory.

```console
% /opt/zb/store/2lvf1cavwkainjz32xzja04hfl5cimx6-hello/bin/hello
Hello, World!
```

In the next few sections, we'll explain the `zb.lua` script in more detail.

## Derivation Basics

The first section downloads [the standard library][] from GitHub:

```lua
local zb <const> = fetchArchive {
  url = "https://github.com/256lights/zb-stdlib/releases/download/v0.1.0/zb-stdlib-v0.1.0.tar.gz";
  hash = "sha256:dd040fe8baad8255e4ca44b7249cddfc24b5980f707a29c3b3e2b47f5193ea48";
}
```

`fetchArchive` is a built-in global function that returns a **derivation**
that extracts the tarball or zip file downloaded from a URL.

A **derivation** in zb is a build step:
a description of a program — called a *builder* — to run to produce files,
along with the builder's dependencies.
Creating a derivation does not run its builder;
derivations only record how to invoke a builder.
We use `zb build` to run builders,
or as we'll see in a moment,
the `import` function will implicitly run the builder.

Finally, derivations can be used like strings.
For example, derivations can be concatenated or passed as an argument to `tostring`.
Such a string is a placeholder for the derivation's output file or directory,
and when used for other derivations,
it implicitly adds a dependency on the derivation.

[the standard library]: https://github.com/256lights/zb-stdlib

## Modules and Imports

The next section loads a Lua module from the zb standard library:

```lua
local stdenv = import(zb.."/stdenv/stdenv.lua")
```

Every Lua file that zb encounters is treated as a separate module.
The `import` built-in global function returns the module at the path given as an argument.
This is similar to the `dofile` and `require` functions in standalone Lua
(which are not supported in zb),
but `import` is special in a few ways:

- `import` will load the module for any given path at most once during a run of `zb`.
- `import` does not execute the module right away.
  Instead, `import` returns a placeholder object that acts like the module.
  When you do anything with the placeholder object other than pass it around,
  it will then wait for the module to finish initialization.
- Globals are not shared among modules.
  Setting a "global" variable in a zb module will place it in a table
  which is implicitly returned by `import`
  if the module does not return any values.
- Everything in a module will be "frozen" when the end of the file is reached.
  This means that any changes to variables or tables (even locals)
  will raise an error.

Together, these aspects allow imports to be reordered or run lazily
without fear of unintended side effects.

One other interesting property of the `import` function
is that if you use a path created from a derivation,
it will build the derivation.
So `zb.."/stdenv/stdenv.lua"` will build the `zb` derivation
and then import the `stdenv/stdenv.lua` file inside the output.

## Importing the Source

The `path` built-in function imports files for use in a derivation:

```lua
local src = path {
  path = ".";
  name = "hello-source";
  filter = function(name)
    return name == "hello.c"
  end;
}
```

The `filter` function allows us to create an allow-list of files in the folder to use.
Changing any file inside a source causes the derivation to be rebuilt on the next `zb build`,
so minimizing the number of files is important for faster incremental builds.

## Creating a Derivation

Finally, for each system that the standard library supports,
we create a table with the `hello` derivation:

```lua
for _, system in ipairs(stdenv.systems) do
  _G[system] = {
    hello = stdenv.makeDerivation {
      pname = "hello";
      src = src;
      buildSystem = system;

      buildPhase = "gcc -o hello hello.c";
      installPhase = '\z
        mkdir -p "$out/bin"\n\z
        mv hello "$out/bin/hello"\n';
    };
  }
end
```

`stdenv.systems` is a table containing the platforms that the standard library supports
(e.g. `"x86_64-unknown-linux"`).

`stdenv.makeDerivation` is a function that returns a derivation.
It provides GCC and a minimal set of standard Unix tools.
If the source contains a Makefile, then it uses that to build.
However, for our simple single-file program, we provide a `buildPhase` directly.
`buildPhase` specifies a snippet of Bash script that builds the program in the source directory.
The `installPhase` specifies a snippet of Bash script
to copy the program to `$out`,
the path to where the derivation's output must be placed.

## Wrapping Up

In this guide, we wrote a simple build configuration for a single-file C program.
The [language reference](lua.md) describes the flavor of Lua that zb understands,
as well as its built-in functions.
The [standard library repository](https://github.com/256lights/zb-stdlib)
includes other packages and utility functions that can be useful.

zb is still in early development.
If you have questions or feedback, [open a discussion on GitHub](https://github.com/256lights/zb/discussions).
If you're interested in getting involved,
see the [zb contribution guide](https://github.com/256lights/zb/blob/main/CONTRIBUTING.md)
and/or the [standard library contribution guide](https://github.com/256lights/zb-stdlib/blob/main/CONTRIBUTING.md).
