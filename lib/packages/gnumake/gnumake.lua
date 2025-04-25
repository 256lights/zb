-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local gcc <const> = import "../gcc/gcc.lua"
local bootstrap <const> = import "../../bootstrap/seeds.lua"
local fetchGNU <const> = import "../../fetchgnu.lua"
local strings <const> = import "../../strings.lua"
local tables <const> = import "../../tables.lua"

local module <const> = {}

local tarballArgs <const> = {
  ["3.82"] = {
    path = "make/make-3.82.tar.bz2";
    hash = "sha256:e2c1a73f179c40c71e2fe8abf8a8a0688b8499538512984da4a76958d0402966";
  };
  ["4.4.1"] = {
    path = "make/make-4.4.1.tar.gz";
    hash = "sha256:dd16fb1d67bfab79a72f5e8390735c49e3e8e70b4945a15ab1f81ddb78658fb3";
  };
}

module.tarballs = tables.lazyMap(fetchGNU, tarballArgs)

---@param args {
---system: string,
---sh: string|derivation,
---gcc: string|derivation,
---coreutils: string|derivation,
---gnutar: string|derivation,
---bzip2: string|derivation,
---}
---@return derivation
function module.makeBootstrap(args)
  local version <const> = "3.82"
  return derivation {
    name = "make-"..version;
    pname = "make";
    version = version;

    system = args.system;
    builder = args.sh.."/bin/sh";
    args = { path "build.sh" };

    src = module.tarballs[version];
    patches = {
      path "patches/3.82/01-include-limits.diff",
    };

    PATH = strings.makeBinPath {
      args.gcc,
      args.sh,
      args.coreutils,
      args.gnutar,
      args.bzip2,
    };
    SOURCE_DATE_EPOCH = 0;
    KBUILD_BUILD_TIMESTAMP = "@0";
  }
end

for system, seeds in pairs(bootstrap) do
  local system <const> = system
  local seeds <const> = seeds
  module[system] = tables.lazyModule {
    bootstrap = function()
      return module.makeBootstrap {
        system = system;
        sh = seeds.busybox;
        gcc = gcc[system].bootstrap;
        coreutils = seeds.busybox;
        gnutar = seeds.busybox;
        bzip2 = seeds.busybox;
      }
    end;
  }
end

return module
