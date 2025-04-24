-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local bootstrap <const> = import "../../bootstrap/seeds.lua"
local fetchGNU <const> = import "../../fetchgnu.lua"
local tables <const> = import "../../tables.lua"

local module <const> = {}

local tarballArgs <const> = {
  ["4.10.0"] = {
    path = "findutils/findutils-4.10.0.tar.xz";
    hash = "sha256:1387e0b67ff247d2abde998f90dfbf70c1491391a59ddfecb8ae698789f0a4f5";
  };
}

module.tarballs = tables.lazyMap(fetchGNU, tarballArgs)

---@param makeDerivation function
---@param system string
---@param version string
---@return derivation
function module.new(makeDerivation, system, version)
  local src = module.tarballs[version]
  if not src then
    error("findutils.new: unsupported version "..version)
  end
  return makeDerivation {
    pname = "findutils";
    version = version;
    system = system;
    src = src;
  }
end

for system in pairs(bootstrap) do
  local system <const> = system
  module[system] = tables.lazyModule {
    stdenv = function()
      local stdenv <const> = import "../../stdenv/stdenv.lua"
      return module.new(stdenv.makeBootstrapDerivation, system, "4.10.0")
    end;
  }
end

return module
