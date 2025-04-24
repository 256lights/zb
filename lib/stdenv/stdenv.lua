-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local strings <const> = import "../strings.lua"
local tables <const> = import "../tables.lua"

local module <const> = {}

module.builderScript = path "builder.sh"

---@param bash string|derivation
---@param deps string|derivation[]
---@param args {
---pname: string?,
---version: string?,
---system: string,
---[string]: string|number|boolean|(string|number|boolean)[],
---}
---@return derivation
local function makeDerivation(bash, deps, args)
  local binPath = strings.makeBinPath(deps)
  local argsPath = args.PATH
  local argsPathType = type(argsPath)
  if argsPathType == "table" then
    binPath = table.concat(argsPath, ":")..":"..binPath
  elseif argsPathType ~= "nil" then
    binPath = argsPath..":"..binPath
  end

  args = tables.clone(args)
  args.PATH = binPath
  if not args.name then
    local name = args.pname
    if not name then
      error("makeDerivation: name or pname must be present", 2)
    end
    local version = args.version
    if version and version ~= "" then
      name = name.."-"..version
    end
    args.name = name
  end
  if not args.builder then
    args.builder = bash.."/bin/bash"
  end
  if not args.args then
    args.args = { module.builderScript }
  end
  if not args.SOURCE_DATE_EPOCH then
    args.SOURCE_DATE_EPOCH = 0
  end
  if not args.KBUILD_BUILD_TIMESTAMP then
    args.KBUILD_BUILD_TIMESTAMP = "@0"
  end
  return derivation(args)
end

---Build a derivation using the bootstrap toolchain.
---@param args {
---pname: string?,
---version: string?,
---system: string,
---[string]: string|number|boolean|(string|number|boolean)[],
---}
---@return derivation
function module.makeBootstrapDerivation(args)
  local gcc = import("../packages/gcc/gcc.lua")[args.system].bootstrap
  local gnumake = import("../packages/gnumake/gnumake.lua")[args.system].bootstrap
  local busybox = import("../bootstrap/seeds.lua")[args.system].busybox
  local bash = import("../packages/bash/bash.lua")[args.system].bootstrap
  if not args.LDFLAGS then
    args = tables.clone(args)
    args.LDFLAGS = "-static"
  end
  return makeDerivation(bash, { gcc, gnumake, bash, busybox }, args)
end

return module
