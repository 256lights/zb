-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local bootstrap <const> = import "../../bootstrap/seeds.lua"
local fetchGNU <const> = import "../../fetchgnu.lua"
local tables <const> = import "../../tables.lua"

local module <const> = {}

local tarballArgs <const> = {
  ["3.12"] = {
    path = "grep/grep-3.12.tar.xz";
    hash = "sha256:2649b27c0e90e632eadcd757be06c6e9a4f48d941de51e7c0f83ff76408a07b9";
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
    error("gnugrep.new: unsupported version "..version)
  end
  return makeDerivation {
    pname = "gnugrep";
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
      return module.new(stdenv.makeBootstrapDerivation, system, "3.12")
    end;
  }
end

return module
