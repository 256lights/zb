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
  local step <const> = path {
    name = "live-bootstrap-steps-simple-patch-1.0";
    path = "live-bootstrap/steps/simple-patch-1.0";
  }

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
  local pname = "mes"
  local version = mes_version
  local nyacc_tarball = fetchurl {
    url = "https://archive.org/download/live-bootstrap-sources/nyacc-1.00.2-lb1.tar.gz";
    hash = "sha256:708c943f89c972910e9544ee077771acbd0a2c0fc6d33496fe158264ddb65327";
  }
  local step = path {
    name = "live-bootstrap-steps-mes-0.26";
    path = "live-bootstrap/steps/mes-0.26";
  }

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
  local pname = "tcc"
  local version = "0.9.26-1147-gee75a10c"
  local tcc_tarball = fetchurl {
    url = "https://lilypond.org/janneke/tcc/tcc-"..version..".tar.gz";
    hash = "sha256:6b8cbd0a5fed0636d4f0f763a603247bc1935e206e1cc5bda6a2818bab6e819f";
  }
  local step = path {
    name = "live-bootstrap-steps-tcc-0.9.26";
    path = "live-bootstrap/steps/tcc-0.9.26";
  }

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
      mkdir ${SRCDIR} ${SRCDIR}/tcc-0.9.26\n\z
      cd ${SRCDIR}/tcc-0.9.26\n\z
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
  local pname = "tcc"
  local version = "0.9.27"
  local step = path {
    name = "live-bootstrap-steps-tcc-0.9.27";
    path = "live-bootstrap/steps/tcc-0.9.27";
  }

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
    -- The 64-bit build will hang indefinitely.
    -- Force 32-bit for this build.
    ARCH = "x86";

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
      cp ${tcc_tarball} ${DISTFILES}/tcc-0.9.27.tar.bz2\n\z
      cp ${mes_tarball} ${DISTFILES}/${MES_PKG}.tar.gz\n\z
      \z
      SRCDIR=${TEMPDIR}/src\n\z
      mkdir ${SRCDIR} ${SRCDIR}/tcc-0.9.26\n\z
      cd ${SRCDIR}/tcc-0.9.26\n\z
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

return boot
