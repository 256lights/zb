-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local bootstrap <const> = import "../../bootstrap/seeds.lua"
local fetchGNU <const> = import "../../fetchgnu.lua"
local strings <const> = import "../../strings.lua"
local tables <const> = import "../../tables.lua"

local module <const> = {}

local tarballArgs <const> = {
  ["6.2.1"] = {
    path = "gmp/gmp-6.2.1.tar.xz";
    hash = "sha256:fd4829912cddd12f84181c3451cc752be224643e87fac497b69edddadc49b4f2";
  };
}

module.tarballs = tables.lazyMap(fetchGNU, tarballArgs)

---@param args {
---makeDerivation: function,
---system: string,
---version: string,
---gnum4: derivation|string,
---}
---@return derivation
function module.new(args)
  local src = module.tarballs[args.version]
  if not src then
    error("gmp.new: unsupported version "..args.version)
  end
  return args.makeDerivation {
    pname = "gmp";
    version = args.version;
    system = args.system;
    src = src;
    configureFlags = { "--disable-shared" };
    PATH = strings.makeBinPath {
      args.gnum4,
    };
  }
end

for system in pairs(bootstrap) do
  local system <const> = system
  module[system] = tables.lazyModule {
    stdenv = function()
      local stdenv <const> = import "../../stdenv/stdenv.lua"
      local gnum4 <const> = import("../gnum4/gnum4.lua")[system].stdenv
      return module.new {
        makeDerivation = stdenv.makeBootstrapDerivation;
        system = system;
        version = "6.2.1";
        gnum4 = gnum4;
      }
    end;
  }
end

return module
