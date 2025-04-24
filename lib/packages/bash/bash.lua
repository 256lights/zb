-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local bootstrap <const> = import "../../bootstrap/seeds.lua"
local fetchGNU <const> = import "../../fetchgnu.lua"
local gcc <const> = import "../gcc/gcc.lua"
local gnumake <const> = import "../gnumake/gnumake.lua"
local strings <const> = import "../../strings.lua"
local tables <const> = import "../../tables.lua"

local module <const> = {}

local tarballArgs <const> = {
  ["5.2.15"] = {
    path = "bash/bash-5.2.15.tar.gz";
    hash = "sha256:13720965b5f4fc3a0d4b61dd37e7565c741da9a5be24edc2ae00182fc1b3588c";
  };
}

module.tarballs = tables.lazyMap(fetchGNU, tarballArgs)

---@param args {
---system: string,
---sh: string|derivation,
---gcc: string|derivation,
---gnumake: string|derivation,
---coreutils: string|derivation,
---gnutar: string|derivation,
---gzip: string|derivation,
---}
---@return derivation
function module.makeBootstrap(args)
  local version <const> = "5.2.15"
  return derivation {
    name = "bash-"..version;
    pname = "bash";
    version = version;

    system = args.system;
    builder = args.sh.."/bin/sh";
    args = { path "build.sh" };

    src = module.tarballs[version];
    patches = {
      path "patches/5.2.15/01-strtoimax.diff",
    };

    PATH = strings.makeBinPath {
      args.gnumake,
      args.gcc,
      args.sh,
      args.coreutils,
      args.gnutar,
      args.gzip,
    };
    LDFLAGS = { "-static" };
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
        gnumake = gnumake[system].bootstrap;
      }
    end;
  }
end

return module
