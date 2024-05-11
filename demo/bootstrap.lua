local stage0 = dofile("stage0-posix/x86_64-linux.lua")

local boot = {}

local gnuMirrors = {
  "https://mirrors.kernel.org/gnu/";
  "https://ftp.gnu.org/gnu/";
}

---@param args {path: string, hash: string}
local function fetchGNU(args)
  return fetchurl({
    url = gnuMirrors[1]..args.path;
    hash = args.hash;
  })
end

---@param args table
local function kaemDerivation(args)
  local actualArgs = {
    system = "x86_64-linux";

    OPERATING_SYSTEM = "Linux";
    ARCH = "x86";
  }
  for k, v in pairs(args) do
    if k ~= "script" then
      actualArgs[k] = v
    end
  end
  actualArgs.builder = stage0.stage0.."/bin/kaem"
  actualArgs.args = { "-f"; toFile(args.name.."-builder.kaem", args.script) }
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

-- mes
do
  local pname = "mes"
  local version = "0.26"
  local mes_tarball = fetchGNU {
    path = "mes/mes-"..version..".tar.gz";
    hash = "sha256:0f2210ad5896249466a0fc9a509e86c9a16db2b722741c6dfb5e8f7b33e385d4";
  }
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

    mes_tarball = mes_tarball;
    nyacc_tarball = nyacc_tarball;

    script = "\z
      PREFIX=${out}\n\z
      BINDIR=${PREFIX}/bin\n\z
      LIBDIR=${PREFIX}/lib/mes\n\z
      INCDIR=${PREFIX}/include/mes\n\z
      \z
      mkdir ${PREFIX} ${BINDIR} ${PREFIX}/lib ${PREFIX}/include ${INCDIR}\n\z
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
      "pass1.kaem";
      "mes-0.26.x86.checksums";
      "files/config.h";
    }).."\z
      exec kaem -f pass1.kaem\n";
  }
end

return boot
