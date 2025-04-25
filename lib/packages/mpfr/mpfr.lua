-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local bootstrap <const> = import "../../bootstrap/seeds.lua"
local fetchGNU <const> = import "../../fetchgnu.lua"
local tables <const> = import "../../tables.lua"

local module <const> = {}

local tarballArgs <const> = {
  ["4.1.0"] = {
    path = "mpfr/mpfr-4.1.0.tar.xz";
    hash = "sha256:0c98a3f1732ff6ca4ea690552079da9c597872d30e96ec28414ee23c95558a7f";
  };
}

module.tarballs = tables.lazyMap(fetchGNU, tarballArgs)

---@param args {
---makeDerivation: function,
---system: string,
---version: string,
---gmp: derivation|string,
---shared: boolean?,
---}
---@return derivation
function module.new(args)
  local src = module.tarballs[args.version]
  if not src then
    error("mpfr.new: unsupported version "..args.version)
  end
  local configureFlags = {
    "--with-gmp="..args.gmp,
  }
  if args.shared == false then
    configureFlags[#configureFlags + 1] = "--disable-shared"
  end
  return args.makeDerivation {
    pname = "mpfr";
    version = args.version;
    system = args.system;
    src = src;
    configureFlags = configureFlags;
  }
end

for system in pairs(bootstrap) do
  local system <const> = system
  module[system] = tables.lazyModule {
    stdenv = function()
      local stdenv <const> = import "../../stdenv/stdenv.lua"
      local gmp <const> = import("../gmp/gmp.lua")[system].stdenv
      return module.new {
        makeDerivation = stdenv.makeBootstrapDerivation;
        system = system;
        version = "4.1.0";
        gmp = gmp;
        shared = false;
      }
    end;
  }
end

return module
