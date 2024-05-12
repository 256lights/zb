local stage0 <const> = dofile("stage0-posix/x86_64-linux.lua")

local boot <const> = {}

local gnuMirrors <const> = {
  "https://mirrors.kernel.org/gnu/",
  "https://ftp.gnu.org/gnu/",
}

---@param args {path: string, hash: string}
local function fetchGNU(args)
  return fetchurl({
    url = gnuMirrors[1]..args.path;
    hash = args.hash;
  })
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
---@param version string
---@return string
local function stepPath(pname, version)
  return path {
    name = "live-bootstrap-steps-"..pname.."-"..version;
    path = "live-bootstrap/steps/"..pname.."-"..version;
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
    PATH = mkBinPath { stage0.stage0, boot.simple_patch, boot.tcc["0.9.26"] };
    M2LIBC_PATH = stage0.M2libc;
    MES_PKG = "mes-0.26";
    MES_PREFIX = boot.mes;
    INCDIR = boot.mes.."/include";
    GUILE_LOAD_PATH = guile_load_path;

    mes_tarball = mes_tarball;
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
        exec kaem -f pass1.kaem\n";
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
      mkdir files mk\n\z
      "..mkStepDir(step, {
      "pass1.kaem",
      "files/getdate_stub.c",
      "files/stat_override.c",
      "mk/main.mk",
    }).."\z
        exec kaem -f pass1.kaem\n";
  }
end

-- sed pass1
boot.sed = {}
do
  local pname <const> = "sed"
  local version <const> = "4.0.9"
  local step <const> = stepPath(pname, version)
  local tarball <const> = fetchGNU {
    path = "sed/sed-"..version..".tar.gz";
    hash = "sha256:c365874794187f8444e5d22998cd5888ffa47f36def4b77517a808dec27c0600";
  }

  boot.sed.pass1 = kaemDerivation {
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

-- bzip2 pass1
boot.bzip2 = {}
do
  local pname <const> = "bzip2"
  local version <const> = "1.0.8"
  local step <const> = stepPath(pname, version)
  local tarball <const> = fetchurl {
    url = "https://mirrors.kernel.org/slackware/slackware-14.0/patches/source/bzip2/bzip2-"..version..".tar.xz";
    hash = "sha256:47fd74b2ff83effad0ddf62074e6fad1f6b4a77a96e121ab421c20a216371a1f";
  }

  boot.bzip2.pass1 = kaemDerivation {
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
      boot.sed.pass1,
      boot.tar["1.12"],
      boot.gzip["1.2.4"],
      boot.patch["2.5.9"],
      boot.make["3.82-pass1"],
      boot.tcc["0.9.27-pass1"],
      boot.simple_patch,
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

return boot
