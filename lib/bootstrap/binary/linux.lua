-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local binutils <const> = import "../binutils.lua"
local busybox <const> = import "../busybox.lua"
local gcc <const> = import "../gcc.lua"
local gmp <const> = import "../gmp.lua"
local linux_headers <const> = import "../linux_headers.lua"
local mpc <const> = import "../mpc.lua"
local mpfr <const> = import "../mpfr.lua"
local musl <const> = import "../musl.lua"

local builderScript <const> = path "builder.sh"

---@param t table
---@param k any
---@param v any
local function addDefault(t, k, v)
  local x = t[k]
  if x == nil then
    if type(v) == "function" then
      t[k] = v()
    else
      t[k] = v
    end
  elseif x == false then
    t[k] = nil
  end
end

local muslCrossMakeCommit <const> = "6f3701d08137496d5aac479e3a3977b5ae993c1f"
local muslCrossMakeTarball <const> = fetchurl {
  name = "musl-cross-make-"..muslCrossMakeCommit..".tar.gz";
  url = "https://github.com/richfelker/musl-cross-make/archive/"..muslCrossMakeCommit..".tar.gz";
  hash = "sha256:b6ad075187d8ac737e38f5f97545bebab6272aac07ffed321d0d90f71ef4c468";
}

local function forArchitecture(arch)
  local userPath <const> = os.getenv("PATH") or "/usr/local/bin:/usr/bin:/bin"
  local userCIncludePath <const> = os.getenv("C_INCLUDE_PATH")
  local userLibraryPath <const> = os.getenv("LIBRARY_PATH")
  local system <const> = arch.."-linux"
  local target <const> = arch.."-unknown-linux-musl"

  local gnuConfigCommit <const> = "3d5db9ebe8607382d17d60faf8853c944fc5f353"
  local configGuess <const> = fetchurl {
    name = "config.guess";
    url = "https://git.savannah.gnu.org/gitweb/?p=config.git;a=blob_plain;f=config.guess;hb="..gnuConfigCommit;
    hash = "sha256:facdf496e646084c42ef81909af0815f8710224599517e1e03bfb90d44e5c936";
  }
  local configSub <const> = fetchurl {
    name = "config.sub";
    url = "https://git.savannah.gnu.org/gitweb/?p=config.git;a=blob_plain;f=config.sub;hb="..gnuConfigCommit;
    hash = "sha256:75d5d255a2a273b6e651f82eecfabf6cbcd8eaeae70e86b417384c8f4a58d8d3";
  }

  local function makeDerivation(args)
    addDefault(args, "name", function() return args.pname.."-"..args.version end)
    addDefault(args, "system", system)
    addDefault(args, "builder", "/usr/bin/bash")
    addDefault(args, "PATH", userPath)
    addDefault(args, "SOURCE_DATE_EPOCH", 0)
    addDefault(args, "KBUILD_BUILD_TIMESTAMP", "@0")
    args.args = { builderScript }
    return derivation(args)
  end

  -- Build GCC.
  -- This gives us a mostly deterministic base for compilation.
  local binutilsVersion <const> = "2.27"
  local gccVersion <const> = "4.2.1"
  local muslVersion <const> = "1.2.4"

  ---@param args {
  ---BUILD: string|nil,
  ---PATH: string,
  ---C_INCLUDE_PATH: string|nil,
  ---LIBRARY_PATH: string|nil,
  ---forceStatic: boolean|nil,
  ---postInstall: string|nil,
  ---}
  ---@return derivation
  local function makeGCC(args)
    local drvArgs = {
      pname = "gcc";
      version = gccVersion;

      TARGET = target;
      PATH = args.PATH;
      C_INCLUDE_PATH = args.C_INCLUDE_PATH;
      CPLUS_INCLUDE_PATH = args.C_INCLUDE_PATH;
      LIBRARY_PATH = args.LIBRARY_PATH;

      src = muslCrossMakeTarball;

      sourceFiles = {
        binutils.tarballs[binutilsVersion],
        gcc.tarballs[gccVersion],
        musl.tarballs[muslVersion],
        gmp.tarball,
        mpc.tarball,
        mpfr.tarball,
        configSub,
        configGuess,
      };

      postUnpack = [[
for i in $sourceFiles; do
  cp "$i" "../$(stripHash "$i")"
done
]];

      patches = {
        path "patches/musl-cross-make/01-config.diff",
        path "patches/musl-cross-make/02-binutils-tools-as-env.diff",
        path "patches/musl-cross-make/03-libgcc-path.diff",
      };

      extraGCCPatches = path "patches/gcc-4.2.1";
      postPatch = [[
for i in "$extraGCCPatches"/*; do
  cp "$i" "patches/gcc-$version/"
done
]];

      configurePhase = [[
cp "$configFile" config.mak
chmod +w config.mak
echo "SOURCES = $(dirname "$(pwd)")" >> config.mak
echo "OUTPUT = $out" >> config.mak
]];

      postInstall = args.postInstall;
    }

    local config = "\z
      TARGET = "..target.."\n\z
      BINUTILS_VER = "..binutilsVersion.."\n\z
      GCC_VER = "..gccVersion.."\n\z
      MUSL_VER = "..muslVersion.."\n\z
      GMP_VER = "..gmp.version.."\n\z
      MPC_VER = "..mpc.version.."\n\z
      MPFR_VER = "..mpfr.version.."\n\z
      COMMON_CONFIG = --disable-shared\n"
    if args.BUILD then
      assert(args.BUILD == target, string.format("unknown config %q", args.BUILD))
      config = config.."BUILD = "..args.BUILD.."\n\z
        HOST = "..args.BUILD.."\n"
    end
    -- The .gch files contain references to previous GCCs, so don't build them.
    local gccConfig <const> = "--disable-libstdcxx-pch"
    if args.forceStatic then
      -- The double dash in `--static` is sadly intentional and necessary.
      -- This terrifying hack courtesy of https://stackoverflow.com/a/29055118.
      -- binutils uses libtool for linking the programs.
      -- libtool takes in a GCC command-line as input,
      -- so an example invocation is `libtool --mode=link gcc $(LDFLAGS) foo.o -o foo`.
      -- Unfortunately, as per https://www.gnu.org/software/libtool/manual/html_node/Link-mode.html,
      -- `-static` has a slightly different meaning:
      -- it avoids libtool shared libraries, but does not pass the flag along to gcc.
      -- This then picks up the shared object library of musl because of the implicit -lc.
      -- The `-all-static` flag is what we want,
      -- but that's not a valid gcc flag,
      -- and it must be passed as a positional argument to libtool.
      -- The binutils Makefiles don't allow you to pass libtool-specific LDFLAGS,
      -- and passing `-all-static` to gcc will kill configure.
      -- `--static` is accepted by GCC but critically, not recognized by libtool,
      -- so it is passed through verbatim without special processing.
      config = config.."BINUTILS_CONFIG = LDFLAGS='--static'\n"

      config = config.."GCC_CONFIG = "..gccConfig.." \z
        LDFLAGS='-static' \z
        LDFLAGS_FOR_BUILD='-static' \z
        LDFLAGS_FOR_TARGET='-static' \z
        BOOT_LDFLAGS='-static'\n"
    else
      config = config.."GCC_CONFIG = "..gccConfig.."\n"
    end
    drvArgs.configFile = toFile("config.mak", config)

    return makeDerivation(drvArgs)
  end

  local gcc1 = makeGCC {
    PATH = userPath;
    C_INCLUDE_PATH = userCIncludePath;
    LIBRARY_PATH = userLibraryPath;

    postInstall = [[
for i in gcc g++; do
  ln -s "$out/bin/${TARGET}-${i}" "$out/bin/$i"
done
]];
  }
  local gcc2 = makeGCC {
    BUILD = target;
    PATH = table.concat({
      gcc1.."/bin",
      userPath,
    }, ":");
    C_INCLUDE_PATH = gcc1.."/"..target.."/include";
    LIBRARY_PATH = table.concat({
      gcc1.."/lib/gcc/"..target.."/"..gccVersion,
      gcc1.."/"..target.."/lib",
    }, ":");
    forceStatic = true;

    postInstall = [[
# Strip debug symbols from executables.
find \
  "$out/bin" \
  "$out/$TARGET/bin" \
  "$out/libexec/gcc/$TARGET/$version" \
  -type f \
  -a '!' -name '*gccbug' \
  -a '!' -name '*.sh' \
  -a '!' -name 'mkheaders' \
  -exec "${TARGET}-strip" -S -p -- '{}' +

# Strip debug symbols from static libraries
# (then use ranlib to rebuild the indices).
find \
  "$out/lib" \
  -type f \
  -name '*.a' \
  -exec "${TARGET}-strip" -S -p -- '{}' + \
  -exec "${TARGET}-ranlib" '{}' \;

# Clean up dependency_libs fields from libtool files.
find \
  "$out" \
  -type f \
  -name '*.la' \
  -exec grep -q '^# Generated by .*libtool' '{}' \; \
  -exec sed -i '{}' \
    -e "/^dependency_libs='[^']/s:-L/build/[^ ']*::g" \
    -e "/^dependency_libs='[^']/s:-L]]..gcc1..[[\([^ ']*\):-L${out}\1:g" \
    \;
]];
  }

  local linux_headers <const> = linux_headers {
    system = system;

    PATH = table.concat({
      gcc2.."/bin",
      userPath,
    }, ":");

    LDFLAGS = "-static";

    C_INCLUDE_PATH = gcc2.."/include";
    LIBRARY_PATH = table.concat({
      gcc2.."/lib/gcc/"..target.."/"..gccVersion,
      gcc2.."/"..target.."/lib",
    }, ":");
  }

  local busybox = makeDerivation {
    pname = "busybox";
    version = busybox.version;

    src = busybox.tarball;
    patches = {
      path "patches/busybox-1.36.1/01-ldflags.diff",
    };

    PATH = table.concat({
      gcc2.."/bin",
      userPath,
    }, ":");

    C_INCLUDE_PATH = table.concat({
      linux_headers.."/include",
      gcc2.."/include",
    }, ":");
    LIBRARY_PATH = table.concat({
      gcc2.."/lib/gcc/"..target.."/"..gccVersion,
      gcc2.."/"..target.."/lib",
    }, ":");

    CONFIG_INSTALL_NO_USR = "y";

    configFile = path "busybox-config";

    makeFlags = "LDFLAGS=-static HOSTLDFLAGS=-static";
    configurePhase = "cp $configFile .config\n";
    installPhase = "make CONFIG_PREFIX=\"$out\" ${makeFlags:-} ${installFlags:-} install";
  }

  return {
    gcc = gcc2;
    busybox = busybox;
  }
end

return setmetatable({}, {
  __index = function(_, k)
    if k == "x86_64" or k == "aarch64" then
      return forArchitecture(k)
    end
    return nil
  end;
})
