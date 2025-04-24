-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local bootstrap <const> = import "../../bootstrap/seeds.lua"
local fetchGNU <const> = import "../../fetchgnu.lua"
local tables <const> = import "../../tables.lua"

local module <const> = {}

local tarballArgs <const> = {
  ["1.35"] = {
    path = "tar/tar-1.35.tar.xz";
    hash = "sha256:4d62ff37342ec7aed748535323930c7cf94acf71c3591882b26a7ea50f3edc16";
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
    error("gnutar.new: unsupported version "..version)
  end
  return makeDerivation {
    pname = "gnutar";
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
      return module.new(stdenv.makeBootstrapDerivation, system, "1.35")
    end;
  }
end

return module
