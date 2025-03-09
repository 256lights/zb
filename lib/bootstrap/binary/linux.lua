-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local gcc <const> = import "../gcc.lua"
local gmp <const> = import "../gmp.lua"
local m4 <const> = import "../m4.lua"
local mpc <const> = import "../mpc.lua"
local mpfr <const> = import "../mpfr.lua"
local musl <const> = import "../musl.lua"
local strings <const> = import "../../strings.lua"
local tables <const> = import "../../tables.lua"

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

---@param paths (string|derivation)[]
---@return string
local function makeRPathFlags(paths)
  local parts = { "-Wl" }
  for _, p in ipairs(paths) do
    parts[#parts + 1] = '-rpath'
    if type(p) == "string" then
      parts[#parts + 1] = p
    else
      parts[#parts + 1] = p.."/lib"
    end
  end
  return table.concat(parts, ',')
end

local function forArchitecture(arch)
  local userPath <const> = os.getenv("PATH") or "/usr/local/bin:/usr/bin:/bin"
  local userCIncludePath <const> = os.getenv("C_INCLUDE_PATH")
  local userCPlusIncludePath <const> = os.getenv("CPLUS_INCLUDE_PATH")
  local userLibraryPath <const> = os.getenv("LIBRARY_PATH")
  local system <const> = arch.."-linux"
  local builderScript <const> = toFile(
    "builder.sh",
    '\z
      #!/usr/bin/env bash\n\z
      set -e\n\z
      mkdir "${TEMPDIR}/src"\n\z
      cd "${TEMPDIR}/src"\n\z
      case "$src" in\n\z
        *.tar.bz2)\n\z
          tar -xf "$src" --bzip2\n\z
          ;;\n\z
        *.tar.gz)\n\z
          tar -xf "$src" --gzip\n\z
          ;;\n\z
        *.tar.xz)\n\z
          tar -xf "$src" --xz\n\z
          ;;\n\z
        *)\n\z
          echo "unhandled source $src"\n\z
          exit 1\n\z
          ;;\n\z
      esac\n\z
      if [[ -n "$sourceRoot" ]]; then\n\z
        cd "$sourceRoot"\n\z
      else\n\z
        cd *\n\z
      fi\n\z
      for i in ${patches:-}; do\n\z
        patch ${patchFlags:--p1} < "$i"\n\z
      done\n\z
      ./configure --prefix=$out $configureFlags\n\z
      make $makeFlags $buildFlags\n\z
      make install $makeFlags $installFlags\n'
  )

  local function mkDerivation(args)
    addDefault(args, "name", function() return args.pname.."-"..args.version end)
    addDefault(args, "system", system)
    addDefault(args, "builder", "/usr/bin/bash")
    addDefault(args, "PATH", userPath)
    addDefault(args, "C_INCLUDE_PATH", userCIncludePath)
    addDefault(args, "CPLUS_INCLUDE_PATH", userCPlusIncludePath)
    addDefault(args, "LIBRARY_PATH", userLibraryPath)
    addDefault(args, "SOURCE_DATE_EPOCH", 0)
    addDefault(args, "KBUILD_BUILD_TIMESTAMP", "@0")
    args.args = { builderScript }
    return derivation(args)
  end

  local m4 = mkDerivation {
    pname = "m4";
    version = m4.version;
    src = m4.tarball;
  }

  local function mkGCCDeps(path)
    path = path or userPath
    local result = {}

    result.gmp = mkDerivation {
      pname = "gmp";
      version = gmp.version;
      src = gmp.tarball;

      PATH = strings.mkBinPath {
        m4,
      }..":"..path;
    }

    result.mpfr = mkDerivation {
      pname = "mpfr";
      version = mpfr.version;
      src = mpfr.tarball;

      PATH = path;

      configureFlags = {
        "--with-gmp="..result.gmp,
      };
    }

    result.mpc = mkDerivation {
      pname = "mpc";
      version = mpc.version;
      src = mpc.tarball;

      PATH = path;

      configureFlags = {
        "--with-gmp="..result.gmp,
        "--with-mpfr="..result.mpfr,
      };
    }
    return result
  end

  --[[
  GCC's build process is *very* particular about include paths.
  During the build of libstdc++,
  it passes --nostdinc++ for libstdc++-v3/src/c++17.
  (See https://gcc.gnu.org/bugzilla/show_bug.cgi?id=100017 for background.)
  Nix's gcc doesn't use the standard include paths
  and depends on the system includes being passed in as flags or environment variables.
  Thus, --nostdinc++ has no effect.
  To emulate this, we:
    - don't set CPLUS_INCLUDE_PATH (because that would get used everywhere)
    - set CXXFLAGS to the set of paths that are in both CPLUS_INCLUDE_PATH and C_INCLUDE_PATH
    - set CXXFLAGS_FOR_BUILD to the set of paths that are in CPLUS_INCLUDE_PATH
      but not C_INCLUDE_PATH.
      (Recall that in cross-compilation, the *build* platform is the machine currently in use,
      the *host* platform is the machine the compiler will run on,
      and the *target* platform is the machine the compiler will generate code for.)
  --]]
  local userCIncludePathList = strings.splitString(":", userCIncludePath)
  local userCPlusIncludePathList = strings.splitString(":", userCPlusIncludePath)
  local cxxArgs = {}
  local cxxForBuildArgs = {}
  for _, path in ipairs(userCPlusIncludePathList) do
    if tables.elem(path, userCIncludePathList) then
      cxxArgs[#cxxArgs + 1] = "-idirafter"
      cxxArgs[#cxxArgs + 1] = path
    else
      cxxForBuildArgs[#cxxForBuildArgs + 1] = "-isystem"
      cxxForBuildArgs[#cxxForBuildArgs + 1] = path
    end
  end

  -- Build first GCC.
  -- This gives us a mostly deterministic base for compilation.
  local gccVersion <const> = "13.1.0"
  local gccDeps = mkGCCDeps()
  local gcc1 <const> = mkDerivation {
    pname = "gcc";
    version = gccVersion;
    src = gcc.tarballs[gccVersion];

    PATH = userPath;
    LDFLAGS = makeRPathFlags { gccDeps.gmp, gccDeps.mpfr, gccDeps.mpc };

    C_INCLUDE_PATH = userCIncludePath;
    CPLUS_INCLUDE_PATH = false;
    CXXFLAGS = cxxArgs;
    CXXFLAGS_FOR_BUILD = cxxForBuildArgs;

    configureFlags = {
      "--with-gmp="..gccDeps.gmp,
      "--with-mpfr="..gccDeps.mpfr,
      "--with-mpc="..gccDeps.mpc,
      "--disable-plugins",
      "--disable-libssp",
      "--disable-libsanitizer",
      "--disable-multilib",
      "--disable-bootstrap",
      "--enable-threads=posix",
      "--enable-languages=c,c++",
    };
  }

  local muslVersion <const> = "1.2.4"
  local musl = mkDerivation {
    pname = "musl";
    version = muslVersion;
    src = musl.tarballs[muslVersion];

    PATH = strings.mkBinPath {
      gcc1,
    }..":"..userPath;

    configureFlags = {
      "--disable-shared",
    };
  }

  return {
    gcc = gcc1;
    musl = musl;
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
