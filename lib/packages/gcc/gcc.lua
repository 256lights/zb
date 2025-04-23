-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local bootstrap <const> = import "../../bootstrap/seeds.lua"
local fetchGNU <const> = import "../../fetchgnu.lua"
local strings <const> = import "../../strings.lua"
local tables <const> = import "../../tables.lua"

local module <const> = {}

local tarballArgs <const> = {
  ["4.2.1"] = {
    path = "gcc/gcc-4.2.1/gcc-4.2.1.tar.bz2";
    hash = "sha256:ca0a12695b3bccfa8628509e08cb9ed7d8ed48deff0a299e4cb8de87d2c1fced";
  };
  ["4.7.4"] = {
    path = "gcc/gcc-4.7.4/gcc-4.7.4.tar.bz2";
    hash = "sha256:92e61c6dc3a0a449e62d72a38185fda550168a86702dea07125ebd3ec3996282";
  };
  ["13.1.0"] = {
    path = "gcc/gcc-13.1.0/gcc-13.1.0.tar.xz";
    hash = "sha256:61d684f0aa5e76ac6585ad8898a2427aade8979ed5e7f85492286c4dfc13ee86";
  };
}

module.tarballs = tables.lazyMap(fetchGNU, tarballArgs)

--- Creates a wrapper script that always includes GCC's includes and libraries.
---@param args {
---system: string,
---gcc: derivation|string,
---coreutils: derivation|string,
---version: string|nil,
---target: string,
---sh: derivation|string,
---}
---@return derivation
function module.makeWrapper(args)
  local version
  if args.version then
    version = args.version
  else
    local gcc = args.gcc
    if type(gcc) == "string" then
      version = strings.getVersion(gcc:match("/?(.*)$"))
      if version == "" then
        error("makeWrapper: version not specified")
      end
    else
      local v = gcc.version
      if type(v) == "string" then
        version = v
      else
        version = strings.getVersion(gcc)
        if version == "" then
          error("makeWrapper: version not specified")
        end
      end
    end
  end
  local sh = args.sh.."/bin/sh"
  return derivation {
    name = "gcc-"..version;
    pname = "gcc";
    version = version;

    system = args.system;
    builder = sh;
    args = { path "make-wrapper.sh" };

    template = path "wrapper.sh";
    PATH = strings.makeBinPath {
      args.gcc,
      args.sh,
      args.coreutils,
    };
    gcc = args.gcc;
    target = args.target;
    runtimeShell = sh;
  }
end

for system, seeds in pairs(bootstrap) do
  local system <const> = system
  local seeds <const> = seeds
  module[system] = tables.lazyModule {
    bootstrap = function()
      return module.makeWrapper {
        system = system;
        gcc = seeds.gcc;
        coreutils = seeds.busybox;
        sh = seeds.busybox;
        target = seeds.target;
      }
    end;
  }
end

return module
