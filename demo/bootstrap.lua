-- Copyright 2024 Ross Light
-- SPDX-License-Identifier: MIT

local stage0 <const> = dofile("stage0-posix/x86_64-linux.lua")

local boot <const> = {}

local gnuMirrors <const> = {
  "https://mirrors.kernel.org/gnu/",
  "https://ftp.gnu.org/gnu/",
}

local badGNUURLs <const> = {
  -- Nix's fetchurl seems to un-lzma the tarball on this URL.
  -- Unclear why.
  "https://mirrors.kernel.org/gnu/coreutils/coreutils-6.10.tar.lzma",
}

---@param args {path: string, hash: string}
local function fetchGNU(args)
  for _, mirror in ipairs(gnuMirrors) do
    local url = mirror..args.path
    if not table.elem(url, badGNUURLs) then
      return fetchurl({
        url = url;
        hash = args.hash;
      })
    end
  end
end

---Construct a binary search path (such as `$PATH`)
---containing the binaries for a set of packages.
---@param pkgs (string|derivation)[]
---@return string # colon-separated paths
local function mkBinPath(pkgs)
  local parts = {}
  for i, x in ipairs(pkgs) do
    if i > 1 then
      parts[#parts + 1] = ":"
    end
    parts[#parts + 1] = tostring(x)
    parts[#parts + 1] = "/bin"
  end
  return table.concat(parts)
end

---@param args table
local function kaemDerivation(args)
  local actualArgs = {
    system = "x86_64-linux";

    OPERATING_SYSTEM = "Linux";
    ARCH = "amd64";
  }
  for k, v in pairs(args) do
    if k ~= "script" then
      actualArgs[k] = v
    end
  end
  actualArgs.builder = stage0.stage0.."/bin/kaem"
  actualArgs.args = { "-f", toFile(args.name.."-builder.kaem", args.script) }
  return derivation(actualArgs)
end

---@param pname string
---@param version string?
---@return string
local function stepPath(pname, version)
  local name = pname
  if version then
    name = name.."-"..version
  end
  return path {
    name = "live-bootstrap-steps-"..name;
    path = "live-bootstrap/steps/"..name;
  }
end

--- Issue cp commands for the directory.
---@param manifest string[]
---@return string
local function mkStepDir(step, manifest)
  local sortedManifest = { table.unpack(manifest) }
  table.sort(sortedManifest)

  -- TODO(soon): Also issue mkdirs.
  local parts = {}
  for _, path in ipairs(sortedManifest) do
    parts[#parts + 1] = "cp "
    parts[#parts + 1] = step
    parts[#parts + 1] = "/"
    parts[#parts + 1] = path
    parts[#parts + 1] = " "
    parts[#parts + 1] = path
    parts[#parts + 1] = "\n"
  end
  return table.concat(parts)
end

-- simple-patch
do
  local pname <const> = "simple-patch"
  local version <const> = "1.0"
  local step <const> = stepPath(pname, version)

  boot.simple_patch = kaemDerivation {
    name = pname.."-"..version;
    pname = pname;
    version = version;

    PATH = stage0.stage0.."/bin";
    M2LIBC_PATH = stage0.M2libc;

    step = step;

    script = "\z
      mkdir ${out} ${out}/bin\n\z
      M2-Mesoplanet --architecture ${ARCH} -f ${step}/src/simple-patch.c -o ${out}/bin/simple-patch\n";
  }
end

-- mes
local mes_version = "0.26"
local mes_tarball = fetchGNU {
  path = "mes/mes-"..mes_version..".tar.gz";
  hash = "sha256:0f2210ad5896249466a0fc9a509e86c9a16db2b722741c6dfb5e8f7b33e385d4";
}
do
  local pname <const> = "mes"
  local version <const> = mes_version
  local nyacc_tarball <const> = fetchurl {
    url = "https://archive.org/download/live-bootstrap-sources/nyacc-1.00.2-lb1.tar.gz";
    hash = "sha256:708c943f89c972910e9544ee077771acbd0a2c0fc6d33496fe158264ddb65327";
  }
  local step <const> = stepPath(pname, version)

  boot.mes = kaemDerivation {
    name = pname.."-"..version;
    pname = pname;
    version = version;

    PATH = stage0.stage0.."/bin";
    M2LIBC_PATH = stage0.M2libc;
    NYACC_PKG = "nyacc-1.00.2";
    MES_PKG = "mes-0.26";
    ARCH = "x86"; -- 64-bit doesn't build correctly.

    mes_tarball = mes_tarball;
    nyacc_tarball = nyacc_tarball;

    script = "\z
      PREFIX=${out}\n\z
      BINDIR=${PREFIX}/bin\n\z
      LIBDIR=${PREFIX}/lib\n\z
      INCDIR=${PREFIX}/include\n\z
      \z
      mkdir ${PREFIX} ${BINDIR} ${LIBDIR} ${INCDIR}\n\z
      \z
      DISTFILES=${TEMPDIR}/distfiles\n\z
      mkdir ${DISTFILES}\n\z
      cp ${mes_tarball} ${DISTFILES}/${MES_PKG}.tar.gz\n\z
      cp ${nyacc_tarball} ${DISTFILES}/${NYACC_PKG}-lb1.tar.gz\n\z
      \z
      SRCDIR=${TEMPDIR}/src\n\z
      MES_PREFIX=${SRCDIR}/${MES_PKG}/build/${MES_PKG}\n\z
      GUILE_LOAD_PATH=${MES_PREFIX}/mes/module:${MES_PREFIX}/module:${SRCDIR}/${MES_PKG}/build/${NYACC_PKG}/module\n\z
      mkdir ${SRCDIR} ${SRCDIR}/${MES_PKG}\n\z
      cd ${SRCDIR}/${MES_PKG}\n\z
      mkdir files\n\z
      "..mkStepDir(step, {
      "pass1.kaem",
      "mes-0.26.x86.checksums",
      "files/config.h",
    }).."\z
      exec kaem -f pass1.kaem\n";
  }
end

local guile_load_path <const> = table.concat({
  boot.mes.."/share/mes/module",
  boot.mes.."/share/nyacc/module",
}, ":")

-- tcc-0.9.26
boot.tcc = {}
do
  local pname <const> = "tcc"
  local version <const> = "0.9.26-1147-gee75a10c"
  local tcc_tarball <const> = fetchurl {
    url = "https://lilypond.org/janneke/tcc/tcc-"..version..".tar.gz";
    hash = "sha256:6b8cbd0a5fed0636d4f0f763a603247bc1935e206e1cc5bda6a2818bab6e819f";
  }
  local step <const> = stepPath(pname, "0.9.26")

  boot.tcc["0.9.26"] = kaemDerivation {
    name = pname.."-"..version;
    pname = pname;
    version = version;

    PATH = mkBinPath { stage0.stage0, boot.simple_patch, boot.mes };
    M2LIBC_PATH = stage0.M2libc;
    MES_PKG = "mes-0.26";
    MES_PREFIX = boot.mes;
    INCDIR = boot.mes.."/include";
    GUILE_LOAD_PATH = guile_load_path;
    -- The 64-bit build will hang indefinitely.
    -- Force 32-bit for this build.
    ARCH = "x86";

    mes_tarball = mes_tarball;
    tcc_tarball = tcc_tarball;

    script = "\z
      PREFIX=${out}\n\z
      BINDIR=${PREFIX}/bin\n\z
      LIBDIR=${PREFIX}/lib\n\z
      PATH=${BINDIR}:${PATH}\n\z
      \z
      mkdir ${PREFIX} ${BINDIR} ${LIBDIR}\n\z
      \z
      DISTFILES=${TEMPDIR}/distfiles\n\z
      mkdir ${DISTFILES}\n\z
      cp ${tcc_tarball} ${DISTFILES}/tcc-0.9.26.tar.gz\n\z
      cp ${mes_tarball} ${DISTFILES}/${MES_PKG}.tar.gz\n\z
      \z
      SRCDIR=${TEMPDIR}/src\n\z
      mkdir ${SRCDIR} ${SRCDIR}/${name}\n\z
      cd ${SRCDIR}/${name}\n\z
      mkdir simple-patches\n\z
      "..mkStepDir(step, {
      "pass1.kaem",
      "tcc-0.9.26.x86.checksums",
      "simple-patches/addback-fileopen.after",
      "simple-patches/addback-fileopen.before",
      "simple-patches/remove-fileopen.after",
      "simple-patches/remove-fileopen.before",
    }).."\z
        exec kaem -f pass1.kaem\n";
  }
end

-- tcc-0.9.27 (pass1)
local tcc_0_9_27_tarball = fetchurl {
  url = "https://download.savannah.gnu.org/releases/tinycc/tcc-0.9.27.tar.bz2";
  hash = "sha256:de23af78fca90ce32dff2dd45b3432b2334740bb9bb7b05bf60fdbfc396ceb9c";
}
do
  local pname <const> = "tcc"
  local version <const> = "0.9.27"
  local step <const> = stepPath(pname, version)

  boot.tcc["0.9.27-pass1"] = kaemDerivation {
    name = pname.."-"..version;
    pname = pname;
    version = version;

    pkg = pname.."-"..version;
    PATH = mkBinPath {
      boot.tcc["0.9.26"],
      boot.simple_patch,
      stage0.stage0,
    };
    M2LIBC_PATH = stage0.M2libc;
    MES_PKG = "mes-0.26";
    MES_PREFIX = boot.mes;
    INCDIR = boot.mes.."/include";
    GUILE_LOAD_PATH = guile_load_path;

    mes_tarball = mes_tarball;
    tcc = boot.tcc["0.9.26"];
    tcc_tarball = tcc_0_9_27_tarball;

    script = "\z
      PREFIX=${out}\n\z
      BINDIR=${PREFIX}/bin\n\z
      LIBDIR=${PREFIX}/lib\n\z
      PATH=${BINDIR}:${PATH}\n\z
      \z
      mkdir ${PREFIX} ${BINDIR} ${LIBDIR} ${LIBDIR}/tcc\n\z
      \z
      DISTFILES=${TEMPDIR}/distfiles\n\z
      mkdir ${DISTFILES}\n\z
      cp ${tcc_tarball} ${DISTFILES}/${name}.tar.bz2\n\z
      cp ${mes_tarball} ${DISTFILES}/${MES_PKG}.tar.gz\n\z
      \z
      SRCDIR=${TEMPDIR}/src\n\z
      mkdir ${SRCDIR} ${SRCDIR}/${name}\n\z
      cd ${SRCDIR}/${name}\n\z
      mkdir simple-patches\n\z
      "..mkStepDir(step, {
      "pass1.kaem",
      "simple-patches/addback-fileopen.after",
      "simple-patches/addback-fileopen.before",
      "simple-patches/fiwix-paddr.after",
      "simple-patches/fiwix-paddr.before",
      "simple-patches/check-reloc-null.after",
      "simple-patches/check-reloc-null.before",
      "simple-patches/remove-fileopen.after",
      "simple-patches/remove-fileopen.before",
    }).."\z
        kaem -f pass1.kaem\n\z
        cp ${tcc}/lib/libgetopt.a ${LIBDIR}/libgetopt.a\n";
  }
end

-- make-3.82 pass1
boot.make = {}
do
  local pname <const> = "make"
  local version <const> = "3.82"
  local step <const> = stepPath(pname, version)
  local tarball <const> = fetchGNU {
    path = "make/make-"..version..".tar.bz2";
    hash = "sha256:e2c1a73f179c40c71e2fe8abf8a8a0688b8499538512984da4a76958d0402966";
  }

  boot.make[version.."-pass1"] = kaemDerivation {
    name = pname.."-"..version;
    pname = pname;
    version = version;

    pkg = pname.."-"..version;
    PATH = mkBinPath { boot.tcc["0.9.27-pass1"], boot.simple_patch, stage0.stage0 };
    INCDIR = boot.tcc["0.9.27-pass1"].INCDIR;
    tcc = boot.tcc["0.9.27-pass1"];

    tarball = tarball;

    script = "\z
      PREFIX=${out}\n\z
      BINDIR=${PREFIX}/bin\n\z
      PATH=${BINDIR}:${PATH}\n\z
      \z
      mkdir ${PREFIX} ${BINDIR}\n\z
      \z
      DISTFILES=${TEMPDIR}/distfiles\n\z
      mkdir ${DISTFILES}\n\z
      cp ${tarball} ${DISTFILES}/${name}.tar.bz2\n\z
      \z
      SRCDIR=${TEMPDIR}/src\n\z
      mkdir ${SRCDIR} ${SRCDIR}/${name}\n\z
      cd ${SRCDIR}/${name}\n\z
      mkdir files\n\z
      "..mkStepDir(step, {
      "pass1.kaem",
      "files/putenv_stub.c",
    }).."\z
        exec kaem -f pass1.kaem\n";
  }
end

-- patch-2.5.9
boot.patch = {}
do
  local pname <const> = "patch"
  local version <const> = "2.5.9"
  local step <const> = stepPath(pname, version)
  local tarball <const> = fetchGNU {
    path = "patch/patch-"..version..".tar.gz";
    hash = "sha256:ecb5c6469d732bcf01d6ec1afe9e64f1668caba5bfdb103c28d7f537ba3cdb8a";
  }

  boot.patch[version] = kaemDerivation {
    name = pname.."-"..version;
    pname = pname;
    version = version;

    pkg = pname.."-"..version;
    PATH = mkBinPath {
      boot.make["3.82-pass1"],
      boot.tcc["0.9.27-pass1"],
      boot.simple_patch,
      stage0.stage0,
    };

    tarball = tarball;

    script = "\z
      PREFIX=${out}\n\z
      BINDIR=${PREFIX}/bin\n\z
      PATH=${BINDIR}:${PATH}\n\z
      \z
      mkdir ${PREFIX} ${BINDIR}\n\z
      \z
      DISTFILES=${TEMPDIR}/distfiles\n\z
      mkdir ${DISTFILES}\n\z
      cp ${tarball} ${DISTFILES}/${name}.tar.gz\n\z
      \z
      SRCDIR=${TEMPDIR}/src\n\z
      mkdir ${SRCDIR} ${SRCDIR}/${name}\n\z
      cd ${SRCDIR}/${name}\n\z
      mkdir mk\n\z
      "..mkStepDir(step, {
      "pass1.kaem",
      "mk/main.mk",
    }).."\z
        exec kaem -f pass1.kaem\n";
  }
end

-- gzip-1.2.4
boot.gzip = {}
do
  local pname <const> = "gzip"
  local version <const> = "1.2.4"
  local step <const> = stepPath(pname, version)
  local tarball <const> = fetchGNU {
    path = "gzip/gzip-"..version..".tar.gz";
    hash = "sha256:1ca41818a23c9c59ef1d5e1d00c0d5eaa2285d931c0fb059637d7c0cc02ad967";
  }

  boot.gzip[version] = kaemDerivation {
    name = pname.."-"..version;
    pname = pname;
    version = version;

    pkg = pname.."-"..version;
    PATH = mkBinPath {
      boot.patch["2.5.9"],
      boot.make["3.82-pass1"],
      boot.tcc["0.9.27-pass1"],
      stage0.stage0,
    };

    tarball = tarball;

    script = "\z
      PREFIX=${out}\n\z
      BINDIR=${PREFIX}/bin\n\z
      PATH=${BINDIR}:${PATH}\n\z
      \z
      mkdir ${PREFIX} ${BINDIR}\n\z
      \z
      DISTFILES=${TEMPDIR}/distfiles\n\z
      mkdir ${DISTFILES}\n\z
      cp ${tarball} ${DISTFILES}/${name}.tar.gz\n\z
      \z
      SRCDIR=${TEMPDIR}/src\n\z
      mkdir ${SRCDIR} ${SRCDIR}/${name}\n\z
      cd ${SRCDIR}/${name}\n\z
      mkdir files mk patches\n\z
      "..mkStepDir(step, {
      "pass1.kaem",
      "files/stat_override.c",
      "mk/main.mk",
      "patches/makecrc-write-to-file.patch",
      "patches/removecrc.patch",
    }).."\z
        exec kaem -f pass1.kaem\n";
  }
end

-- tar-1.12
boot.tar = {}
do
  local pname <const> = "tar"
  local version <const> = "1.12"
  local step <const> = stepPath(pname, version)
  local tarball <const> = fetchGNU {
    path = "tar/tar-"..version..".tar.gz";
    hash = "sha256:c6c37e888b136ccefab903c51149f4b7bd659d69d4aea21245f61053a57aa60a";
  }

  boot.tar[version] = kaemDerivation {
    name = pname.."-"..version;
    pname = pname;
    version = version;

    pkg = pname.."-"..version;
    PATH = mkBinPath {
      boot.gzip["1.2.4"],
      boot.patch["2.5.9"],
      boot.make["3.82-pass1"],
      boot.tcc["0.9.27-pass1"],
      stage0.stage0,
    };

    tarball = tarball;

    script = "\z
      PREFIX=${out}\n\z
      BINDIR=${PREFIX}/bin\n\z
      PATH=${BINDIR}:${PATH}\n\z
      \z
      mkdir ${PREFIX} ${BINDIR}\n\z
      \z
      DISTFILES=${TEMPDIR}/distfiles\n\z
      mkdir ${DISTFILES}\n\z
      cp ${tarball} ${DISTFILES}/${name}.tar.gz\n\z
      \z
      SRCDIR=${TEMPDIR}/src\n\z
      mkdir ${SRCDIR} ${SRCDIR}/${name}\n\z
      cd ${SRCDIR}/${name}\n\z
      mkdir files mk patches\n\z
      "..mkStepDir(step, {
      "pass1.kaem",
      "files/getdate_stub.c",
      "files/stat_override.c",
      "mk/main.mk",
      "patches/no-special-modes.patch",
    }).."\z
        exec kaem -f pass1.kaem\n";
  }
end

-- sed pass1
boot.sed = {}
local sed_4_0_9_tarball <const> = fetchGNU {
  path = "sed/sed-4.0.9.tar.gz";
  hash = "sha256:c365874794187f8444e5d22998cd5888ffa47f36def4b77517a808dec27c0600";
}
do
  local pname <const> = "sed"
  local version <const> = "4.0.9"
  local step <const> = stepPath(pname, version)

  boot.sed[version.."-pass1"] = kaemDerivation {
    name = pname.."-"..version;
    pname = pname;
    version = version;

    pkg = pname.."-"..version;
    PATH = mkBinPath {
      boot.tar["1.12"],
      boot.gzip["1.2.4"],
      boot.patch["2.5.9"],
      boot.make["3.82-pass1"],
      boot.tcc["0.9.27-pass1"],
      stage0.stage0,
    };

    tarball = sed_4_0_9_tarball;

    script = "\z
      PREFIX=${out}\n\z
      BINDIR=${PREFIX}/bin\n\z
      PATH=${BINDIR}:${PATH}\n\z
      \z
      mkdir ${PREFIX} ${BINDIR}\n\z
      \z
      DISTFILES=${TEMPDIR}/distfiles\n\z
      mkdir ${DISTFILES}\n\z
      cp ${tarball} ${DISTFILES}/${name}.tar.gz\n\z
      \z
      SRCDIR=${TEMPDIR}/src\n\z
      mkdir ${SRCDIR} ${SRCDIR}/${name}\n\z
      cd ${SRCDIR}/${name}\n\z
      mkdir mk\n\z
      "..mkStepDir(step, {
      "pass1.kaem",
      "mk/main.mk",
    }).."\z
        exec kaem -f pass1.kaem\n";
  }
end

-- bzip2 pass1
boot.bzip2 = {}
local bzip2_version <const> = "1.0.8"
local bzip2_tarball <const> = fetchurl {
  url = "https://mirrors.kernel.org/slackware/slackware-14.0/patches/source/bzip2/bzip2-"..bzip2_version..".tar.xz";
  hash = "sha256:47fd74b2ff83effad0ddf62074e6fad1f6b4a77a96e121ab421c20a216371a1f";
}
do
  local pname <const> = "bzip2"
  local step <const> = stepPath(pname, bzip2_version)

  boot.bzip2.pass1 = kaemDerivation {
    name = pname.."-"..bzip2_version;
    pname = pname;
    version = bzip2_version;

    pkg = pname.."-"..bzip2_version;
    PATH = mkBinPath {
      boot.tar["1.12"],
      boot.gzip["1.2.4"],
      boot.patch["2.5.9"],
      boot.make["3.82-pass1"],
      boot.tcc["0.9.27-pass1"],
      stage0.stage0,
    };

    tarball = bzip2_tarball;

    script = "\z
      PREFIX=${out}\n\z
      BINDIR=${PREFIX}/bin\n\z
      PATH=${BINDIR}:${PATH}\n\z
      \z
      mkdir ${PREFIX} ${BINDIR}\n\z
      \z
      DISTFILES=${TEMPDIR}/distfiles\n\z
      mkdir ${DISTFILES}\n\z
      cp ${tarball} ${DISTFILES}/${name}.tar.xz\n\z
      \z
      SRCDIR=${TEMPDIR}/src\n\z
      mkdir ${SRCDIR} ${SRCDIR}/${name}\n\z
      cd ${SRCDIR}/${name}\n\z
      mkdir patches\n\z
      "..mkStepDir(step, {
      "pass1.kaem",
      "patches/coreutils.patch",
      "patches/mes-libc.patch",
    }).."\z
        exec kaem -f pass1.kaem\n";
  }
end

-- coreutils pass1
boot.coreutils = {}
local coreutils_5_0_tarball <const> = fetchGNU {
  path = "coreutils/coreutils-5.0.tar.bz2";
  hash = "sha256:c25b36b8af6e0ad2a875daf4d6196bd0df28a62be7dd252e5f99a4d5d7288d95";
}
do
  local pname <const> = "coreutils"
  local version <const> = "5.0"
  local step <const> = stepPath(pname, version)

  boot.coreutils["5.0-pass1"] = kaemDerivation {
    name = pname.."-"..version;
    pname = pname;
    version = version;

    pkg = pname.."-"..version;
    PATH = mkBinPath {
      boot.bzip2.pass1,
      boot.sed["4.0.9-pass1"],
      boot.tar["1.12"],
      boot.gzip["1.2.4"],
      boot.patch["2.5.9"],
      boot.make["3.82-pass1"],
      boot.tcc["0.9.27-pass1"],
      stage0.stage0,
    };

    tarball = coreutils_5_0_tarball;

    script = "\z
      PREFIX=${out}\n\z
      BINDIR=${PREFIX}/bin\n\z
      PATH=${BINDIR}:${PATH}\n\z
      \z
      mkdir ${PREFIX} ${BINDIR}\n\z
      \z
      DISTFILES=${TEMPDIR}/distfiles\n\z
      mkdir ${DISTFILES}\n\z
      cp ${tarball} ${DISTFILES}/${name}.tar.bz2\n\z
      \z
      SRCDIR=${TEMPDIR}/src\n\z
      mkdir ${SRCDIR} ${SRCDIR}/${name}\n\z
      cd ${SRCDIR}/${name}\n\z
      mkdir mk patches\n\z
      "..mkStepDir(step, {
      "pass1.kaem",
      "mk/main.mk",
      "patches/expr-strcmp.patch",
      "patches/ls-strcmp.patch",
      "patches/mbstate.patch",
      "patches/modechange.patch",
      "patches/sort-locale.patch",
      "patches/tac-uint64.patch",
      "patches/touch-dereference.patch",
      "patches/touch-getdate.patch",
      "patches/uniq-fopen.patch",
    }).."\z
        exec kaem -f pass1.kaem\n";
  }
end

-- byacc
do
  local pname <const> = "byacc"
  local version <const> = "20240109"
  local step <const> = stepPath(pname, version)
  local tarball <const> = fetchurl {
    url = "https://invisible-island.net/archives/"..pname.."/"..pname.."-"..version..".tgz";
    hash = "sha256:f2897779017189f1a94757705ef6f6e15dc9208ef079eea7f28abec577e08446";
  }

  boot.byacc = kaemDerivation {
    name = pname.."-"..version;
    pname = pname;
    version = version;

    pkg = pname.."-"..version;
    PATH = mkBinPath {
      boot.coreutils["5.0-pass1"],
      boot.sed["4.0.9-pass1"],
      boot.tar["1.12"],
      boot.gzip["1.2.4"],
      boot.patch["2.5.9"],
      boot.make["3.82-pass1"],
      boot.tcc["0.9.27-pass1"],
      stage0.stage0,
    };

    tarball = tarball;

    script = "\z
      PREFIX=${out}\n\z
      BINDIR=${PREFIX}/bin\n\z
      PATH=${BINDIR}:${PATH}\n\z
      \z
      mkdir ${PREFIX} ${BINDIR}\n\z
      \z
      DISTFILES=${TEMPDIR}/distfiles\n\z
      mkdir ${DISTFILES}\n\z
      cp ${tarball} ${DISTFILES}/${name}.tgz\n\z
      \z
      SRCDIR=${TEMPDIR}/src\n\z
      mkdir ${SRCDIR}\n\z
      cp -R "..step.." ${SRCDIR}/${name}\n\z
      cd ${SRCDIR}/${name}\n\z
      chmod -R +w .\n\z
      exec kaem -f pass1.kaem\n";
  }
end

-- bash pass1
boot.bash = {}
local bash_2_05_tarball <const> = fetchurl {
  url = "https://src.fedoraproject.org/repo/pkgs/bash/bash-2.05b.tar.bz2/f3e5428ed52a4f536f571a945d5de95d/bash-2.05b.tar.bz2";
  hash = "sha256:1ce4e5b47a6354531389f0adefb54dee2823227bf6e1e59a31c0e9317a330822";
}
do
  local pname <const> = "bash"
  local version <const> = "2.05b"
  local step <const> = stepPath(pname, version)

  boot.bash["2.05b-pass1"] = kaemDerivation {
    name = pname.."-"..version;
    pname = pname;
    version = version;

    pkg = pname.."-"..version;
    PATH = mkBinPath {
      boot.byacc,
      boot.coreutils["5.0-pass1"],
      boot.sed["4.0.9-pass1"],
      boot.tar["1.12"],
      boot.bzip2.pass1,
      boot.patch["2.5.9"],
      boot.make["3.82-pass1"],
      boot.tcc["0.9.27-pass1"],
      stage0.stage0,
    };

    tarball = bash_2_05_tarball;

    script = "\z
      PREFIX=${out}\n\z
      BINDIR=${PREFIX}/bin\n\z
      PATH=${BINDIR}:${PATH}\n\z
      \z
      mkdir ${PREFIX} ${BINDIR}\n\z
      \z
      DISTFILES=${TEMPDIR}/distfiles\n\z
      mkdir ${DISTFILES}\n\z
      cp ${tarball} ${DISTFILES}/${name}.tar.bz2\n\z
      \z
      SRCDIR=${TEMPDIR}/src\n\z
      mkdir ${SRCDIR}\n\z
      cp -R "..step.." ${SRCDIR}/${name}\n\z
      cd ${SRCDIR}/${name}\n\z
      chmod -R +w .\n\z
      exec kaem -f pass1.kaem\n";
  }
end

---@class bashStepArgs
---@field pname string
---@field version string
---@field setup string?
---@field tarballs derivation[]
---@field [string] any

---@param args bashStepArgs
local function bashStep(args)
  local pname <const> = args.pname
  local version <const> = args.version
  local defaultName <const> = pname.."-"..version
  local actualArgs <const> = {
    name = defaultName;
    system = "x86_64-linux";

    OPERATING_SYSTEM = "Linux";
    ARCH = "amd64";
    MAKEJOBS = "-j1";
    pkg = defaultName;
    revision = 0;

    builder = boot.bash["2.05b-pass1"].."/bin/bash";
  }
  for k, v in pairs(args) do
    if k ~= "setup" then
      actualArgs[k] = v
    end
  end
  actualArgs.SHELL = actualArgs.builder

  ---@type (string|number|derivation)[]
  local scriptChunks <const> = {
    "\z
      #!/usr/bin/env bash\n\z
      set -e\n\z
      export PREFIX=${out}\n\z
      export DESTDIR=''\n\z
      export BINDIR=${PREFIX}/bin\n\z
      export LIBDIR=${PREFIX}/lib\n\z
      PATH=${BINDIR}:${PATH}\n\z
      mkdir ${PREFIX}\n",
    ". ",
    path "live-bootstrap/steps/helpers.sh",
    "\n",
    args.setup or "",
    "\n\z
      DISTFILES=${TEMPDIR}/distfiles\n\z
      mkdir ${DISTFILES}\n\z
      _select() {\n\z
        local i=\"$1\"\n\z
        shift\n\z
        eval \"echo \\$$i\"\n\z
      }\n",
  }
  for i, t in ipairs(args.tarballs) do
    scriptChunks[#scriptChunks + 1] = "cp \"$(_select "
    scriptChunks[#scriptChunks + 1] = i
    scriptChunks[#scriptChunks + 1] = " $tarballs)\" ${DISTFILES}/"
    ---@diagnostic disable-next-line: param-type-mismatch
    scriptChunks[#scriptChunks + 1] = baseNameOf(t.name)
    scriptChunks[#scriptChunks + 1] = "\n"
  end
  scriptChunks[#scriptChunks + 1] = "\z
      unset _select\n\z
      SRCDIR=${TEMPDIR}/src\n\z
      mkdir ${SRCDIR}\n\z
      cp -R "
  scriptChunks[#scriptChunks + 1] = stepPath(actualArgs.pkg)
  scriptChunks[#scriptChunks + 1] = " ${SRCDIR}/${pkg}\n\z
      chmod -R +w ${SRCDIR}/${pkg}\n\z
      build ${pkg}\n"
  local script <const> = table.concat(scriptChunks)
  actualArgs.args = { toFile(actualArgs.name.."-builder.sh", script) }

  return derivation(actualArgs)
end

do
  local tcc = boot.tcc["0.9.26"]

  boot.tcc["0.9.27-pass2"] = bashStep {
    pname = "tcc";
    version = "0.9.27";
    revision = 1;

    PATH = mkBinPath {
      boot.bash["2.05b-pass1"],
      boot.byacc,
      boot.coreutils["5.0-pass1"],
      boot.sed["4.0.9-pass1"],
      boot.tar["1.12"],
      boot.bzip2.pass1,
      boot.patch["2.5.9"],
      boot.make["3.82-pass1"],
      tcc,
      stage0.stage0,
    };
    INCDIR = boot.mes.."/include";
    tcc = tcc;

    tarballs = { tcc_0_9_27_tarball };

    setup = "\z
      cp -R ${tcc}/lib $LIBDIR\n\z
      chmod -R +w $LIBDIR\n";
  }
end

local musl_1_1_24_tarball = fetchurl {
  url = "https://musl.libc.org/releases/musl-1.1.24.tar.gz";
  hash = "sha256:1370c9a812b2cf2a7d92802510cca0058cc37e66a7bedd70051f0a34015022a3";
}
boot.musl = {}
boot.musl["1.1.24-pass1"] = bashStep {
  pname = "musl";
  version = "1.1.24";
  revision = 0;

  PATH = mkBinPath {
    boot.tcc["0.9.27-pass2"],
    boot.bash["2.05b-pass1"],
    boot.byacc,
    boot.coreutils["5.0-pass1"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = { musl_1_1_24_tarball };
}

do
  local tcc <const> = boot.tcc["0.9.26"]
  local musl <const> = boot.musl["1.1.24-pass1"]

  boot.tcc["0.9.27-pass3"] = bashStep {
    pname = "tcc";
    version = "0.9.27";
    revision = 2;

    PATH = mkBinPath {
      boot.bash["2.05b-pass1"],
      boot.byacc,
      boot.coreutils["5.0-pass1"],
      boot.sed["4.0.9-pass1"],
      boot.tar["1.12"],
      boot.bzip2.pass1,
      boot.patch["2.5.9"],
      boot.make["3.82-pass1"],
      tcc,
      stage0.stage0,
    };
    INCDIR = musl.."/include";
    tcc = tcc;
    musl = musl;

    tarballs = { tcc_0_9_27_tarball };
  }
end

boot.musl["1.1.24-pass2"] = bashStep {
  pname = "musl";
  version = "1.1.24";
  revision = 1;

  PATH = mkBinPath {
    boot.tcc["0.9.27-pass3"],
    boot.bash["2.05b-pass1"],
    boot.byacc,
    boot.coreutils["5.0-pass1"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = { musl_1_1_24_tarball };
}

do
  local tcc <const> = boot.tcc["0.9.27-pass3"]
  local musl <const> = boot.musl["1.1.24-pass2"]

  boot.tcc["0.9.27-pass4"] = bashStep {
    pname = "tcc";
    version = "0.9.27";
    revision = 3;

    PATH = mkBinPath {
      boot.bash["2.05b-pass1"],
      boot.byacc,
      boot.coreutils["5.0-pass1"],
      boot.sed["4.0.9-pass1"],
      boot.tar["1.12"],
      boot.bzip2.pass1,
      boot.patch["2.5.9"],
      boot.make["3.82-pass1"],
      tcc,
      stage0.stage0,
    };
    INCDIR = musl.."/include";
    tcc = tcc;
    musl = musl;

    tarballs = { tcc_0_9_27_tarball };
  }
end

boot.sed["4.0.9-pass2"] = bashStep {
  pname = "sed";
  version = "4.0.9";
  revision = 1;

  PATH = mkBinPath {
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.byacc,
    boot.coreutils["5.0-pass1"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = { sed_4_0_9_tarball };
}

boot.bzip2.pass2 = bashStep {
  pname = "bzip2";
  version = bzip2_version;
  revision = 1;

  PATH = mkBinPath {
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["5.0-pass1"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = { bzip2_tarball };
}

boot.m4 = {}
boot.m4["1.4.7"] = bashStep {
  pname = "m4";
  version = "1.4.7";

  PATH = mkBinPath {
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["5.0-pass1"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchGNU {
      path = "m4/m4-1.4.7.tar.bz2";
      hash = "sha256:a88f3ddaa7c89cf4c34284385be41ca85e9135369c333fdfa232f3bf48223213";
    },
  };
}

local heirloom_devtools = bashStep {
  pname = "heirloom-devtools";
  version = "070527";

  PATH = mkBinPath {
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.byacc,
    boot.coreutils["5.0-pass1"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchurl {
      url = "http://downloads.sourceforge.net/project/heirloom/heirloom-devtools/070527/heirloom-devtools-070527.tar.bz2";
      hash = "sha256:9f233d8b78e4351fe9dd2d50d83958a0e5af36f54e9818521458a08e058691ba";
    },
  };
}

boot.flex = {}
boot.flex["2.5.11"] = bashStep {
  pname = "flex";
  version = "2.5.11";

  PATH = mkBinPath {
    heirloom_devtools,
    boot.byacc,
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["5.0-pass1"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  LIBRARY_PATH = heirloom_devtools.."/lib";

  tarballs = {
    fetchurl {
      url = "http://download.nust.na/pub2/openpkg1/sources/DST/flex/flex-2.5.11.tar.gz";
      hash = "sha256:bc79b890f35ca38d66ff89a6e3758226131e51ccbd10ef78d5ff150b7bd73689";
    },
  };
}
boot.flex["2.6.4"] = bashStep {
  pname = "flex";
  version = "2.6.4";

  PATH = mkBinPath {
    boot.flex["2.5.11"],
    boot.byacc,
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["5.0-pass1"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  LIBRARY_PATH = heirloom_devtools.."/lib";

  tarballs = {
    fetchurl {
      url = "https://github.com/westes/flex/releases/download/v2.6.4/flex-2.6.4.tar.gz";
      hash = "sha256:e87aae032bf07c26f85ac0ed3250998c37621d95f8bd748b31f15b33c45ee995";
    },
  };
}

boot.bison = {}
do
  local yacc = boot.byacc
  local tarball <const> = fetchGNU {
    path = "bison/bison-3.4.1.tar.xz";
    hash = "sha256:27159ac5ebf736dffd5636fd2cd625767c9e437de65baa63cb0de83570bd820d";
  }
  for i = 1, 3 do
    yacc = bashStep {
      pname = "bison";
      version = "3.4.1";
      revision = i - 1;

      m4 = boot.m4["1.4.7"];

      PATH = mkBinPath {
        yacc,
        boot.flex["2.6.4"],
        boot.m4["1.4.7"],
        boot.tcc["0.9.27-pass4"],
        boot.bash["2.05b-pass1"],
        boot.coreutils["5.0-pass1"],
        boot.sed["4.0.9-pass1"],
        boot.tar["1.12"],
        boot.gzip["1.2.4"],
        boot.bzip2.pass2,
        boot.patch["2.5.9"],
        boot.make["3.82-pass1"],
        stage0.stage0,
      };

      tarballs = { tarball };
    }
  end
  boot.bison["3.4.1"] = yacc
end

boot.grep = {}
boot.grep["2.4"] = bashStep {
  pname = "grep";
  version = "2.4";

  PATH = mkBinPath {
    boot.bison["3.4.1"],
    boot.flex["2.6.4"],
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["5.0-pass1"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchGNU {
      path = "grep/grep-2.4.tar.gz";
      hash = "sha256:a32032bab36208509466654df12f507600dfe0313feebbcd218c32a70bf72a16";
    },
  };
}

boot.diffutils = {}
boot.diffutils["2.7"] = bashStep {
  pname = "diffutils";
  version = "2.7";

  PATH = mkBinPath {
    boot.grep["2.4"],
    boot.bison["3.4.1"],
    boot.flex["2.6.4"],
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["5.0-pass1"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchGNU {
      path = "diffutils/diffutils-2.7.tar.gz";
      hash = "sha256:d5f2489c4056a31528e3ada4adacc23d498532b0af1a980f2f76158162b139d6";
    },
  };
}

boot.coreutils["5.0-pass2"] = bashStep {
  pname = "coreutils";
  version = "5.0";
  revision = 1;

  PATH = mkBinPath {
    boot.diffutils["2.7"],
    boot.grep["2.4"],
    boot.bison["3.4.1"],
    boot.flex["2.6.4"],
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["5.0-pass1"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = { coreutils_5_0_tarball };
}
-- This coreutils only builds date, mktemp, and sha256sum.
boot.coreutils["6.10"] = bashStep {
  pname = "coreutils";
  version = "6.10";

  PATH = mkBinPath {
    boot.diffutils["2.7"],
    boot.grep["2.4"],
    boot.bison["3.4.1"],
    boot.flex["2.6.4"],
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["5.0-pass1"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchGNU {
      path = "coreutils/coreutils-6.10.tar.lzma";
      hash = "sha256:8b05bba1b2726a164e444c314e3f359604b58216be704bed8f2e028449cc6204";
    },
  };
}

boot.gawk = {}
boot.gawk["3.0.4"] = bashStep {
  pname = "gawk";
  version = "3.0.4";

  PATH = mkBinPath {
    boot.diffutils["2.7"],
    boot.grep["2.4"],
    boot.bison["3.4.1"],
    boot.flex["2.6.4"],
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["6.10"],
    boot.coreutils["5.0-pass2"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchGNU {
      path = "gawk/gawk-3.0.4.tar.gz";
      hash = "sha256:5cc35def1ff4375a8b9a98c2ff79e95e80987d24f0d42fdbb7b7039b3ddb3fb0";
    },
  };
}

boot.perl = {}
do
  local perlSources <const> = {
    {
      version = "5.000";
      url = "https://github.com/Perl/perl5/archive/perl-5.000.tar.gz";
      hash = "sha256:1ae43c8d2983404b9eec61c96e3ffa27e7b07e08215c95c015a4ab0095373ef3";
    },
    {
      version = "5.003";
      url = "https://github.com/Perl/perl5/archive/perl-5.003.tar.gz";
      hash = "sha256:9fa29beb2fc4a3c373829fc051830796de301f32a719d0b52a400d1719bbd7b1";
    },
    {
      version = "5.004-05";
      url = "https://www.cpan.org/src/5.0/perl5.004_05.tar.gz";
      hash = "sha256:1184478b298978b164a383ed5661e3a117c48ab97d6d0ab7ef614cdbe918b9eb";
      pkg = "perl5.004-05";
    },
    {
      version = "5.005-03";
      url = "https://www.cpan.org/src/5.0/perl5.005_03.tar.gz";
      hash = "sha256:93f41cd87ab8ee83391cfa39a63b076adeb7c3501d2efa31b98d0ef037122bd1";
      pkg = "perl5.005-03";
    },
    {
      version = "5.6.2";
      url = "https://www.cpan.org/src/5.0/perl-5.6.2.tar.gz";
      hash = "sha256:a5e66f6ebf701b0567f569f57cae82abf5ce57af70a2b45ae71323b61f49134e";
    },
  }

  local perls <const> = {}
  local sed <const> = boot.sed["4.0.9-pass1"]
  for i, src in ipairs(perlSources) do
    local pathDrvs <const> = {
      boot.gawk["3.0.4"],
      boot.diffutils["2.7"],
      boot.grep["2.4"],
      boot.bison["3.4.1"],
      boot.flex["2.6.4"],
      boot.m4["1.4.7"],
      boot.tcc["0.9.27-pass4"],
      boot.bash["2.05b-pass1"],
      boot.coreutils["6.10"],
      boot.coreutils["5.0-pass2"],
      sed,
      boot.tar["1.12"],
      boot.gzip["1.2.4"],
      boot.bzip2.pass2,
      boot.patch["2.5.9"],
      boot.make["3.82-pass1"],
      stage0.stage0,
    }
    if i > 1 then
      table.insert(pathDrvs, 1, perls[#perls])
    end
    perls[i] = bashStep {
      pname = "perl";
      version = src.version;
      pkg = src.pkg;

      PATH = mkBinPath(pathDrvs);

      musl = boot.musl["1.1.24-pass2"];
      sed = sed;

      tarballs = {
        fetchurl {
          url = src.url;
          hash = src.hash;
        },
      };
    }
  end
  local lastPerl <const> = perls[#perls]
  boot.perl[lastPerl.version] = lastPerl
end

boot.autoconf = {}
boot.autoconf["2.52"] = bashStep {
  pname = "autoconf";
  version = "2.52";

  PATH = mkBinPath {
    boot.perl["5.6.2"],
    boot.gawk["3.0.4"],
    boot.diffutils["2.7"],
    boot.grep["2.4"],
    boot.bison["3.4.1"],
    boot.flex["2.6.4"],
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["6.10"],
    boot.coreutils["5.0-pass2"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchGNU {
      path = "autoconf/autoconf-2.52.tar.bz2";
      hash = "sha256:4681bcbb9c9298c506f6405a7deb62c54fc3b339d3239a8f36a5df83daaec94f";
    },
  };
}

boot.automake = {}
do
  local pname <const> = "automake"
  local version <const> = "1.6.3"
  local tarball <const> = fetchGNU {
    path = "automake/automake-"..version..".tar.bz2";
    hash = "sha256:0dbafacaf21e135cab35d357a14bdcd981d2f2d00e1387801be8091a31b7bb81";
  }
  local perl <const> = boot.perl["5.6.2"]

  local pass1 = bashStep {
    pname = pname;
    version = version;

    PATH = mkBinPath {
      boot.autoconf["2.52"],
      perl,
      boot.gawk["3.0.4"],
      boot.diffutils["2.7"],
      boot.grep["2.4"],
      boot.bison["3.4.1"],
      boot.flex["2.6.4"],
      boot.m4["1.4.7"],
      boot.tcc["0.9.27-pass4"],
      boot.bash["2.05b-pass1"],
      boot.coreutils["6.10"],
      boot.coreutils["5.0-pass2"],
      boot.sed["4.0.9-pass1"],
      boot.tar["1.12"],
      boot.gzip["1.2.4"],
      boot.bzip2.pass2,
      boot.patch["2.5.9"],
      boot.make["3.82-pass1"],
      stage0.stage0,
    };
    perl = perl;

    tarballs = { tarball };
  }
  boot.automake[version] = bashStep {
    pname = pname;
    version = version;
    revision = 1;

    PATH = mkBinPath {
      pass1,
      boot.autoconf["2.52"],
      perl,
      boot.gawk["3.0.4"],
      boot.diffutils["2.7"],
      boot.grep["2.4"],
      boot.bison["3.4.1"],
      boot.flex["2.6.4"],
      boot.m4["1.4.7"],
      boot.tcc["0.9.27-pass4"],
      boot.bash["2.05b-pass1"],
      boot.coreutils["6.10"],
      boot.coreutils["5.0-pass2"],
      boot.sed["4.0.9-pass1"],
      boot.tar["1.12"],
      boot.gzip["1.2.4"],
      boot.bzip2.pass2,
      boot.patch["2.5.9"],
      boot.make["3.82-pass1"],
      stage0.stage0,
    };
    perl = perl;

    tarballs = { tarball };
  }
end

boot.autoconf["2.53"] = bashStep {
  pname = "autoconf";
  version = "2.53";

  PATH = mkBinPath {
    boot.automake["1.6.3"],
    boot.autoconf["2.52"],
    boot.perl["5.6.2"],
    boot.gawk["3.0.4"],
    boot.diffutils["2.7"],
    boot.grep["2.4"],
    boot.bison["3.4.1"],
    boot.flex["2.6.4"],
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["6.10"],
    boot.coreutils["5.0-pass2"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchGNU {
      path = "autoconf/autoconf-2.53.tar.bz2";
      hash = "sha256:6b217a064c6d06603d50a3ad05129aef9435367810c10894210b8dad965d2306";
    },
  };
}

boot.automake["1.7"] = bashStep {
  pname = "automake";
  version = "1.7";

  PATH = mkBinPath {
    boot.automake["1.6.3"],
    boot.autoconf["2.53"],
    boot.perl["5.6.2"],
    boot.gawk["3.0.4"],
    boot.diffutils["2.7"],
    boot.grep["2.4"],
    boot.bison["3.4.1"],
    boot.flex["2.6.4"],
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["6.10"],
    boot.coreutils["5.0-pass2"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchGNU {
      path = "automake/automake-1.7.tar.bz2";
      hash = "sha256:6633ee1202375e3c8798a92e1b7f46894f78d541aeea7f49654503fdc0b28835";
    },
  };
}

boot.autoconf["2.54"] = bashStep {
  pname = "autoconf";
  version = "2.54";

  PATH = mkBinPath {
    boot.automake["1.7"],
    boot.autoconf["2.53"],
    boot.perl["5.6.2"],
    boot.gawk["3.0.4"],
    boot.diffutils["2.7"],
    boot.grep["2.4"],
    boot.bison["3.4.1"],
    boot.flex["2.6.4"],
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["6.10"],
    boot.coreutils["5.0-pass2"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchGNU {
      path = "autoconf/autoconf-2.54.tar.bz2";
      hash = "sha256:a74aea954f36c7beeb6cc47b96a408c3e04e7ad635f614e65250dbcd8ec0bd28";
    },
  };
}

boot.autoconf["2.55"] = bashStep {
  pname = "autoconf";
  version = "2.55";

  PATH = mkBinPath {
    boot.automake["1.7"],
    boot.autoconf["2.54"],
    boot.perl["5.6.2"],
    boot.gawk["3.0.4"],
    boot.diffutils["2.7"],
    boot.grep["2.4"],
    boot.bison["3.4.1"],
    boot.flex["2.6.4"],
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["6.10"],
    boot.coreutils["5.0-pass2"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchGNU {
      path = "autoconf/autoconf-2.55.tar.bz2";
      hash = "sha256:f757158a04889b265203eecd8ca92568e2a67c3b9062fa6bff7a0a6efd2244ac";
    },
  };
}

boot.automake["1.7.8"] = bashStep {
  pname = "automake";
  version = "1.7.8";

  PATH = mkBinPath {
    boot.automake["1.7"],
    boot.autoconf["2.55"],
    boot.perl["5.6.2"],
    boot.gawk["3.0.4"],
    boot.diffutils["2.7"],
    boot.grep["2.4"],
    boot.bison["3.4.1"],
    boot.flex["2.6.4"],
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["6.10"],
    boot.coreutils["5.0-pass2"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchGNU {
      path = "automake/automake-1.7.8.tar.bz2";
      hash = "sha256:2dddc3b51506e702647ccc6757e15c05323fa67245d2d53e81ed36a832f9be42";
    },
  };
}

boot.autoconf["2.57"] = bashStep {
  pname = "autoconf";
  version = "2.57";

  PATH = mkBinPath {
    boot.automake["1.7.8"],
    boot.autoconf["2.55"],
    boot.perl["5.6.2"],
    boot.gawk["3.0.4"],
    boot.diffutils["2.7"],
    boot.grep["2.4"],
    boot.bison["3.4.1"],
    boot.flex["2.6.4"],
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["6.10"],
    boot.coreutils["5.0-pass2"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchGNU {
      path = "autoconf/autoconf-2.57.tar.bz2";
      hash = "sha256:e1035aa2c21fae2a934d1ab56c774ce9d22717881dab8a1a5b16d294fb793489";
    },
  };
}

boot.autoconf["2.59"] = bashStep {
  pname = "autoconf";
  version = "2.59";

  PATH = mkBinPath {
    boot.automake["1.7.8"],
    boot.autoconf["2.57"],
    boot.perl["5.6.2"],
    boot.gawk["3.0.4"],
    boot.diffutils["2.7"],
    boot.grep["2.4"],
    boot.bison["3.4.1"],
    boot.flex["2.6.4"],
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["6.10"],
    boot.coreutils["5.0-pass2"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchGNU {
      path = "autoconf/autoconf-2.59.tar.bz2";
      hash = "sha256:f0cde70a8f135098a6a3e85869f2e1cc3f141beea766fa3d6636e086cd8b90a7";
    },
  };
}

boot.automake["1.8.5"] = bashStep {
  pname = "automake";
  version = "1.8.5";

  PATH = mkBinPath {
    boot.automake["1.7.8"],
    boot.autoconf["2.59"],
    boot.perl["5.6.2"],
    boot.gawk["3.0.4"],
    boot.diffutils["2.7"],
    boot.grep["2.4"],
    boot.bison["3.4.1"],
    boot.flex["2.6.4"],
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["6.10"],
    boot.coreutils["5.0-pass2"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchGNU {
      path = "automake/automake-1.8.5.tar.bz2";
      hash = "sha256:84c93aaa3c3651a9e7474b721b0e6788318592509e7de604bafe4ea8049dc410";
    },
  };
}

boot.help2man = bashStep {
  pname = "help2man";
  version = "1.36.4";

  PATH = mkBinPath {
    boot.automake["1.8.5"],
    boot.autoconf["2.59"],
    boot.perl["5.6.2"],
    boot.gawk["3.0.4"],
    boot.diffutils["2.7"],
    boot.grep["2.4"],
    boot.bison["3.4.1"],
    boot.flex["2.6.4"],
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["6.10"],
    boot.coreutils["5.0-pass2"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchGNU {
      path = "help2man/help2man-1.36.4.tar.gz";
      hash = "sha256:a4adadf76b496a6bc50795702253ecfcb6f0d159b68038f31a5362009340bca2";
    },
  };
}

boot.autoconf["2.61"] = bashStep {
  pname = "autoconf";
  version = "2.61";

  PATH = mkBinPath {
    boot.help2man,
    boot.automake["1.8.5"],
    boot.autoconf["2.59"],
    boot.perl["5.6.2"],
    boot.gawk["3.0.4"],
    boot.diffutils["2.7"],
    boot.grep["2.4"],
    boot.bison["3.4.1"],
    boot.flex["2.6.4"],
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["6.10"],
    boot.coreutils["5.0-pass2"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchGNU {
      path = "autoconf/autoconf-2.61.tar.bz2";
      hash = "sha256:93a2ceab963618b021db153f0c881a2de82455c1dc7422be436fcd5c554085a1";
    },
  };
}

boot.automake["1.9.6"] = bashStep {
  pname = "automake";
  version = "1.9.6";

  PATH = mkBinPath {
    boot.help2man,
    boot.automake["1.8.5"],
    boot.autoconf["2.61"],
    boot.perl["5.6.2"],
    boot.gawk["3.0.4"],
    boot.diffutils["2.7"],
    boot.grep["2.4"],
    boot.bison["3.4.1"],
    boot.flex["2.6.4"],
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["6.10"],
    boot.coreutils["5.0-pass2"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchGNU {
      path = "automake/automake-1.9.6.tar.bz2";
      hash = "sha256:8eccaa98e1863d10e4a5f861d8e2ec349a23e88cb12ad10f6b6f79022ad2bb8d";
    },
  };
}

boot.automake["1.10.3"] = bashStep {
  pname = "automake";
  version = "1.10.3";

  PATH = mkBinPath {
    boot.help2man,
    boot.automake["1.9.6"],
    boot.autoconf["2.61"],
    boot.perl["5.6.2"],
    boot.gawk["3.0.4"],
    boot.diffutils["2.7"],
    boot.grep["2.4"],
    boot.bison["3.4.1"],
    boot.flex["2.6.4"],
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["6.10"],
    boot.coreutils["5.0-pass2"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchGNU {
      path = "automake/automake-1.10.3.tar.bz2";
      hash = "sha256:e98ab43bb839c31696a4202e5b6ff388b391659ef2387cf9365019fad17e1adc";
    },
  };
}

boot.autoconf["2.64"] = bashStep {
  pname = "autoconf";
  version = "2.64";

  PATH = mkBinPath {
    boot.help2man,
    boot.automake["1.10.3"],
    boot.autoconf["2.61"],
    boot.perl["5.6.2"],
    boot.gawk["3.0.4"],
    boot.diffutils["2.7"],
    boot.grep["2.4"],
    boot.bison["3.4.1"],
    boot.flex["2.6.4"],
    boot.m4["1.4.7"],
    boot.tcc["0.9.27-pass4"],
    boot.bash["2.05b-pass1"],
    boot.coreutils["6.10"],
    boot.coreutils["5.0-pass2"],
    boot.sed["4.0.9-pass1"],
    boot.tar["1.12"],
    boot.gzip["1.2.4"],
    boot.bzip2.pass2,
    boot.patch["2.5.9"],
    boot.make["3.82-pass1"],
    stage0.stage0,
  };

  tarballs = {
    fetchGNU {
      path = "autoconf/autoconf-2.64.tar.xz";
      hash = "sha256:32d977213320b8ae76c71175305301197f2b0e04e72d70694bc3d3e2ae6c7248";
    },
  };
}

return boot
