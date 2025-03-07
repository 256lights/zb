-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local gcc <const> = import "../gcc.lua"
local gmp <const> = import "../gmp.lua"
local linuxHeaders <const> = import "../linux_headers.lua"
local m4 <const> = import "../m4.lua"
local mpc <const> = import "../mpc.lua"
local mpfr <const> = import "../mpfr.lua"
local musl <const> = import "../musl.lua"
local strings <const> = import "../../strings.lua"

local function forArchitecture(arch)
  local userPath <const> = os.getenv("PATH")
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
      ./configure --prefix=$out $configureFlags\n\z
      make $makeFlags $buildFlags\n\z
      make install $makeFlags $installFlags\n'
  )

  local function mkDerivation(args)
    args.name = args.name or args.pname.."-"..args.version
    args.system = args.system or system
    args.builder = args.builder or "/usr/bin/bash"
    args.PATH = args.PATH or userPath
    args.SOURCE_DATE_EPOCH = args.SOURCE_DATE_EPOCH or 0
    args.KBUILD_BUILD_TIMESTAMP = args.KBUILD_BUILD_TIMESTAMP or "@0"
    args.args = { builderScript }
    return derivation(args)
  end

  local m4 = mkDerivation {
    pname = "m4";
    version = m4.version;
    src = m4.tarball;
  }

  local gmp = mkDerivation {
    pname = "gmp";
    version = gmp.version;
    src = gmp.tarball;

    PATH = strings.mkBinPath {
      m4,
    }..":"..userPath;
  }

  local mpfr = mkDerivation {
    pname = "mpfr";
    version = mpfr.version;
    src = mpfr.tarball;

    PATH = userPath;

    configureFlags = {
      "--with-gmp="..gmp,
    };
  }

  local mpc = mkDerivation {
    pname = "mpc";
    version = mpc.version;
    src = mpc.tarball;

    PATH = userPath;

    configureFlags = {
      "--with-gmp="..gmp,
      "--with-mpfr="..mpfr,
    };
  }

  local muslVersion <const> = "1.2.4"
  local musl = mkDerivation {
    pname = "musl";
    version = muslVersion;
    src = musl.tarballs[muslVersion];

    PATH = userPath;
  }

  local linuxHeaders = linuxHeaders {
    system = system;
    builder = "/usr/bin/bash";

    PATH = userPath;
    C_INCLUDE_PATH = strings.mkIncludePath { musl };
    LIBRARY_PATH = strings.mkIncludePath { musl };
  }

  local gccVersion <const> = "13.1.0"
  local gcc = mkDerivation {
    pname = "gcc";
    version = gccVersion;
    src = gcc.tarballs[gccVersion];

    PATH = userPath;

    configureFlags = {
      "--target="..arch.."-unknown-linux-musl",
      "--program-transform-name=",
      "--with-mpc="..mpc,
      "--with-mpfr="..mpfr,
      "--with-gmp="..gmp,
      "--disable-plugins",
      "--disable-libssp",
      "--disable-libsanitizer",
      "--disable-multilib",
      "--disable-bootstrap",
      "--enable-threads=posix",
      "--enable-languages=c,c++",
      "--enable-static",
    };

    buildFlags = {
      "BOOT_LDFLAGS=-static",
    };

    C_INCLUDE_PATH = strings.mkIncludePath {
      musl,
      linuxHeaders,
    };
    LIBRARY_PATH = strings.mkLibraryPath {
      musl,
    };
  }

  return {
    gcc = gcc;
    linuxHeaders = linuxHeaders;
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
