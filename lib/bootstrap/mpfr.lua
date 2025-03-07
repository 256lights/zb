-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local fetchGNU <const> = import "../fetchgnu.lua"

local module <const> = { version = "4.1.0" }
local getters <const> = {}

setmetatable(module, {
  __index = function(_, k)
    local g = getters[k]
    if g then return g() end
  end;
})

function getters.tarball()
  return fetchGNU {
    path = "mpfr/mpfr-4.1.0.tar.xz";
    hash = "sha256:0c98a3f1732ff6ca4ea690552079da9c597872d30e96ec28414ee23c95558a7f";
  }
end

return module
