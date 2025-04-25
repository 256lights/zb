-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local bootstrap <const> = import "../../bootstrap/seeds.lua"
local fetchGNU <const> = import "../../fetchgnu.lua"
local tables <const> = import "../../tables.lua"

local module <const> = {}

local tarballArgs <const> = {
  ["1.2.1"] = {
    path = "mpc/mpc-1.2.1.tar.gz";
    hash = "sha256:17503d2c395dfcf106b622dc142683c1199431d095367c6aacba6eec30340459";
  };
}

module.tarballs = tables.lazyMap(fetchGNU, tarballArgs)

---@param args {
---makeDerivation: function,
---system: string,
---version: string,
---gmp: derivation|string,
---mpfr: derivation|string,
---shared: boolean?,
---}
---@return derivation
function module.new(args)
  local src = module.tarballs[args.version]
  if not src then
    error("libmpc.new: unsupported version "..args.version)
  end
  local configureFlags = {
    "--with-gmp="..args.gmp,
    "--with-mpfr="..args.mpfr,
  }
  if args.shared == false then
    configureFlags[#configureFlags + 1] = "--disable-shared"
  end
  return args.makeDerivation {
    pname = "libmpc";
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
      local mpfr <const> = import("../mpfr/mpfr.lua")[system].stdenv
      return module.new {
        makeDerivation = stdenv.makeBootstrapDerivation;
        system = system;
        version = "1.2.1";
        gmp = gmp;
        mpfr = mpfr;
        shared = false;
      }
    end;
  }
end

return module
