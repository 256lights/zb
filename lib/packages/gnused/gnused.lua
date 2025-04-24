-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local bootstrap <const> = import "../../bootstrap/seeds.lua"
local fetchGNU <const> = import "../../fetchgnu.lua"
local tables <const> = import "../../tables.lua"

local module <const> = {}

local tarballArgs <const> = {
  ["4.8"] = {
    path = "sed/sed-4.8.tar.xz";
    hash = "sha256:f79b0cfea71b37a8eeec8490db6c5f7ae7719c35587f21edb0617f370eeff633";
  };
  ["4.9"] = {
    path = "sed/sed-4.9.tar.xz";
    hash = "sha256:6e226b732e1cd739464ad6862bd1a1aba42d7982922da7a53519631d24975181";
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
    error("gnused.new: unsupported version "..version)
  end
  return makeDerivation {
    pname = "gnused";
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
      return module.new(stdenv.makeBootstrapDerivation, system, "4.8")
    end;
  }
end

return module
