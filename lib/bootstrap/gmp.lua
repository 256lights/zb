-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local fetchGNU <const> = import "../fetchgnu.lua"

local module <const> = { version = "6.2.1" }
local getters <const> = {}

setmetatable(module, {
  __index = function(_, k)
    local g = getters[k]
    if g then return g() end
  end;
})

function getters.tarball()
  return fetchGNU {
    path = "gmp/gmp-6.2.1.tar.xz";
    hash = "sha256:fd4829912cddd12f84181c3451cc752be224643e87fac497b69edddadc49b4f2";
  }
end

return module
