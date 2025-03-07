-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local steps <const> = import "./steps.lua"

local module <const> = {}

local tarballArgs <const> = {
  ["1.1.24"] = {
    url = "https://musl.libc.org/releases/musl-1.1.24.tar.gz";
    hash = "sha256:1370c9a812b2cf2a7d92802510cca0058cc37e66a7bedd70051f0a34015022a3";
  };
  ["1.2.4"] = {
    url = "https://musl.libc.org/releases/musl-1.2.4.tar.gz";
    hash = "sha256:7a35eae33d5372a7c0da1188de798726f68825513b7ae3ebe97aaaa52114f039";
  };
}

module.tarballs = setmetatable({}, {
  __index = function(_, k)
    local args = tarballArgs[k]
    if args then return fetchurl(args) end
  end;
})

setmetatable(module, {
  __call = function(_, args)
    return steps.bash {
      pname = "musl";
      version = args.version;
      revision = args.revision;
      builder = args.bash.."/bin/bash";

      PATH = args.PATH;
      ARCH = args.ARCH;
      CC = args.CC;
      AR = args.AR;
      C_INCLUDE_PATH = args.C_INCLUDE_PATH;
      LIBRARY_PATH = args.LIBRARY_PATH;
      ACLOCAL_PATH = args.ACLOCAL_PATH;

      tarballs = { module.tarballs[args.version] };
    }
  end;
})

return module
