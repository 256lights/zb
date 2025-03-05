-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local gpath <const> = path
local helpers <const> = gpath "live-bootstrap/steps/helpers.sh"
local helpers_nix <const> = gpath "live-bootstrap/steps/helpers-nix.sh"

archToZBTable = {
  amd64 = "x86_64";
  aarch64 = "aarch64";
}

---@param pname string
---@param version string?
---@return string
function path(pname, version)
  local name = pname
  if version then
    name = name.."-"..version
  end
  return gpath {
    name = "live-bootstrap-steps-"..name;
    path = "live-bootstrap/steps/"..name;
  }
end

---@class bashStepArgs
---@field pname string
---@field version string
---@field builder string
---@field setup string?
---@field tarballs derivation[]
---@field [string] any

---@param args bashStepArgs
function bash(args)
  if not args.builder then
    error("missing builder from steps.bash args", 2)
  end
  local pname <const> = args.pname
  local version <const> = args.version
  local defaultName <const> = pname.."-"..version
  local actualArgs <const> = {
    name = defaultName;
    system = "x86_64-linux";

    OPERATING_SYSTEM = "Linux";
    ARCH = "amd64";
    MAKEJOBS = "-j1";
    SOURCE_DATE_EPOCH = 0;
    KBUILD_BUILD_TIMESTAMP = "@0";
    pkg = defaultName;
    revision = 0;
  }
  if args.ARCH then
    local a = archToZBTable[args.ARCH]
    if a then actualArgs.system = a.."-linux" end
  end
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
    helpers,
    "\n",
    ". ",
    helpers_nix,
    "\n",
    args.setup or "",
    "\n\z
      DISTFILES=${TEMPDIR}/distfiles\n\z
      mkdir ${DISTFILES}\n\z
      _select() {\n\z
        local i=\"$1\"\n\z
        shift\n\z
        eval \"echo \\${$i}\"\n\z
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
  scriptChunks[#scriptChunks + 1] = path(actualArgs.pkg)
  scriptChunks[#scriptChunks + 1] = " ${SRCDIR}/${pkg}\n\z
      chmod -R +w ${SRCDIR}/${pkg}\n\z
      build ${pkg}\n"
  local script <const> = table.concat(scriptChunks)
  actualArgs.args = { toFile(actualArgs.name.."-builder.sh", script) }

  return derivation(actualArgs)
end
