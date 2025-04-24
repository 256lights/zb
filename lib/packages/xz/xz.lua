-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local bootstrap <const> = import "../../bootstrap/seeds.lua"
local tables <const> = import "../../tables.lua"

local module <const> = {}

local tarballArgs <const> = {
  ["5.6.3"] = {
    url = "https://github.com/tukaani-project/xz/releases/download/v5.6.3/xz-5.6.3.tar.xz";
    hash = "sha256:db0590629b6f0fa36e74aea5f9731dc6f8df068ce7b7bafa45301832a5eebc3a";
  };
}

module.tarballs = tables.lazyMap(fetchurl, tarballArgs)

---@param makeDerivation function
---@param system string
---@param version string
---@return derivation
function module.new(makeDerivation, system, version)
  local src = module.tarballs[version]
  if not src then
    error("xz.new: unsupported version "..version)
  end
  return makeDerivation {
    pname = "xz";
    version = version;
    system = system;
    src = src;

    -- TODO(someday): Allow dynamic linking and pass -static properly.
    LDFLAGS = "--static";
    configureFlags = { "--disable-shared" };
  }
end

for system in pairs(bootstrap) do
  local system <const> = system
  module[system] = tables.lazyModule {
    stdenv = function()
      local stdenv <const> = import "../../stdenv/stdenv.lua"
      return module.new(stdenv.makeBootstrapDerivation, system, "5.6.3")
    end;
  }
end

return module
