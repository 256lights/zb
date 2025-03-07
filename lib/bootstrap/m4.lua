-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local fetchGNU <const> = import "../fetchgnu.lua"
local steps <const> = import "./steps.lua"

local module <const> = { version = "1.4.7" }
local getters <const> = {}

function getters.tarball()
  return fetchGNU {
    path = "m4/m4-1.4.7.tar.bz2";
    hash = "sha256:a88f3ddaa7c89cf4c34284385be41ca85e9135369c333fdfa232f3bf48223213";
  }
end

setmetatable(module, {
  __index = function(_, k)
    local g = getters[k]
    if g then return g() end
  end;

  __call = function(_, args)
    return steps.bash {
      pname = "m4";
      version = module.version;
      builder = args.bash.."/bin/bash";

      ARCH = args.ARCH;
      CC = args.CC;
      AR = args.AR;
      CFLAGS = args.CFLAGS;
      PATH = args.PATH;

      tarballs = { module.tarball };
    }
  end;
})

return module
