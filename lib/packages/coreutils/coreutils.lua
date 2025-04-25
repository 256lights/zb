-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local bootstrap <const> = import "../../bootstrap/seeds.lua"
local fetchGNU <const> = import "../../fetchgnu.lua"
local tables <const> = import "../../tables.lua"

local module <const> = {}

local tarballArgs <const> = {
  ["9.7"] = {
    path = "coreutils/coreutils-9.7.tar.xz";
    hash = "sha256:e8bb26ad0293f9b5a1fc43fb42ba970e312c66ce92c1b0b16713d7500db251bf";
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
    error("coreutils.new: unsupported version "..version)
  end
  return makeDerivation {
    pname = "coreutils";
    version = version;
    system = system;
    src = src;
    -- stdbuf insists on creating a .so file, which fails. Disable it.
    configureFlags = { "utils_cv_stdbuf_supported=no" };
  }
end

for system in pairs(bootstrap) do
  local system <const> = system
  module[system] = tables.lazyModule {
    stdenv = function()
      local stdenv <const> = import "../../stdenv/stdenv.lua"
      return module.new(stdenv.makeBootstrapDerivation, system, "9.7")
    end;
  }
end

return module
