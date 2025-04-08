# Getting Started

**This guide is not complete.**
Progress tracked in [#100](https://github.com/256lights/zb/issues/100).

This introductory guide will help you run your first build with zb
and explain the basic concepts you will need to use zb.
zb is a build system that manages your dependencies
so that you can be confident that a build that works on one machine works on any machine.

## Prerequisites

This guide assumes:

- zb is installed on your computer.
- Familiarity with the command-line interface for your operating system.
  (For example, Terminal.app on macOS, Command Prompt or PowerShell on Windows, etc.)
- Knowledge of at least one programming language.
  We will be building a C program in this tutorial,
  but you do not need to know C or have any specific developer tools installed.
  Installing tools automatically is a key feature of zb!
- zb uses the [Lua programming language](https://www.lua.org/) to configure builds.
  Learning Lua is helpful for using zb.
  zb uses the Lua 5.4 language,
  with some standard libraries omitted to limit the complexity of builds.
  As such, any learning resources for Lua will be applicable to zb.

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
that comes with tools for some common programming languages.
In your editor, enter the following into a new file `zb.lua`
in the same directory as `hello.c`:

```lua
-- zb.lua

-- Download the standard library.
local zb = fetchArchive {
  url = "https://github.com/256lights/zb/archive/"..commit..".tar.gz";
  hash = "TODO(someday)";
}

-- Import modules from the standard library.
local lib = import(zb.."/lib/lib.lua")
local packages = import(zb.."/lib/packages.lua")

-- TODO(soon): Per-architecture.
-- Create our build target.
hello = lib.stdenv.makeDerivation {
  name = "hello";
  src = path ".";

  buildPhase = "gcc -o hello hello.c";
  installPhase = '\z
    mkdir -p "$out/bin"\n\z
    mv hello "$out/bin/hello"\n';
}
```

Now we can build the program with `zb build`:

```shell
zb build --file zb.lua hello
```

`zb build` takes in a Lua file to run.
The subsequent arguments name variables to build (in this case, `hello`).
After downloading and running the necessary programs,
`zb build` will display the path to the directory it created,
something like `/zb/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-hello`.
As you might expect from the `installPhase` we used above,
it will be inside the `bin` directory we created inside the output directory.

```console
% /zb/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-hello/bin/hello
Hello, World!
```

In the next few sections, we'll explain the `zb.lua` script in more detail.

## Derivation Basics

**TODO(someday):** This mechanism exists today,
but the standard library is not stable.

The first section downloads the standard library from GitHub:

```lua
-- Download the standard library.
local zb = fetchArchive {
  url = "https://github.com/256lights/zb/archive/"..commit..".tar.gz";
  hash = "TODO(someday)";
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

## Modules and Imports

The next section loads Lua modules from the zb standard library:

```lua
-- Import modules from the standard library.
local lib = import(zb.."/lib/lib.lua")
local packages = import(zb.."/lib/packages.lua")
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

Together, these aspects allow imports to be reordered or done as needed
without fear of unintended side effects.

One other interesting property of the `import` function
is that if you use a path created from a derivation,
it will build the derivation.
So `zb.."/lib/packages.lua"` will build the `zb` derivation
and then import the `lib/packages.lua` file inside the output.

## Creating a Derivation

**TODO(soon):** Explain this.

```lua
-- Create our build target.
hello = lib.stdenv.makeDerivation {
  name = "hello";
  src = path ".";

  buildPhase = "gcc -o hello hello.c";
  installPhase = '\z
    mkdir -p "$out/bin"\n\z
    mv hello "$out/bin/hello"\n';
}
```
