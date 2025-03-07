-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local fetchGNU <const> = import "../fetchgnu.lua"

local module <const> = { version = "1.2.1" }
local getters <const> = {}

setmetatable(module, {
  __index = function(_, k)
    local g = getters[k]
    if g then return g() end
  end;
})

function getters.tarball()
  return fetchGNU {
    path = "mpc/mpc-1.2.1.tar.gz";
    hash = "sha256:17503d2c395dfcf106b622dc142683c1199431d095367c6aacba6eec30340459";
  }
end

return module
