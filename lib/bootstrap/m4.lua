-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local fetchGNU <const> = import "../fetchgnu.lua"
local steps <const> = import "./steps.lua"
local strings <const> = import "../strings.lua"
local tables <const> = import "../tables.lua"

return function(args)
  return steps.bash(tables.update(
    {
      pname = "m4";
      version = "1.4.7";
      builder = args.bash.."/bin/bash";

      PATH = args.PATH or strings.mkBinPath {
        args.tcc,
        args.bash,
        args.coreutils,
        args.sed,
        args.tar,
        args.gzip,
        args.bzip2,
        args.patch,
        args.make,
        args.stage0,
      };

      tarballs = {
        fetchGNU {
          path = "m4/m4-1.4.7.tar.bz2";
          hash = "sha256:a88f3ddaa7c89cf4c34284385be41ca85e9135369c333fdfa232f3bf48223213";
        },
      };
    },
    args
  ))
end
