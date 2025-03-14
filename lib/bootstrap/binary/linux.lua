-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local binutils <const> = import "../binutils.lua"
local gcc <const> = import "../gcc.lua"
local gmp <const> = import "../gmp.lua"
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

local function forArchitecture(arch)
  local userPath <const> = os.getenv("PATH") or "/usr/local/bin:/usr/bin:/bin"
  local userCIncludePath <const> = os.getenv("C_INCLUDE_PATH")
  local userLibraryPath <const> = os.getenv("LIBRARY_PATH")
  local system <const> = arch.."-linux"

  local muslCrossMakeCommit <const> = "6f3701d08137496d5aac479e3a3977b5ae993c1f"
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

  local function mkDerivation(args)
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
  local gcc <const> = mkDerivation {
    pname = "gcc";
    version = gccVersion;

    PATH = userPath;
    C_INCLUDE_PATH = userCIncludePath;
    CPLUS_INCLUDE_PATH = userCIncludePath;
    LIBRARY_PATH = userLibraryPath;

    src = fetchurl {
      name = "musl-cross-make-"..muslCrossMakeCommit..".tar.gz";
      url = "https://github.com/richfelker/musl-cross-make/archive/"..muslCrossMakeCommit..".tar.gz";
      hash = "sha256:b6ad075187d8ac737e38f5f97545bebab6272aac07ffed321d0d90f71ef4c468";
    };

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

    configFile = toFile("config.mak", "\z
      TARGET = "..arch.."-unknown-linux-musl\n\z
      BINUTILS_VER = "..binutilsVersion.."\n\z
      GCC_VER = "..gccVersion.."\n\z
      MUSL_VER = "..muslVersion.."\n\z
      GMP_VER = "..gmp.version.."\n\z
      MPC_VER = "..mpc.version.."\n\z
      MPFR_VER = "..mpfr.version.."\n\z
      GCC_CONFIG = --disable-shared\n");
    configurePhase = [[
cp "$configFile" config.mak
chmod +w config.mak
echo "SOURCES = $(dirname "$(pwd)")" >> config.mak
echo "OUTPUT = $out" >> config.mak
]];
  }


  return {
    gcc = gcc;
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
