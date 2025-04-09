-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local module <const> = { version = "1.36.1" }
local getters <const> = {}

setmetatable(module, {
  __index = function(_, k)
    local g = getters[k]
    if g then return g() end
  end;
})

function getters.tarball()
  return fetchurl {
    url = "https://busybox.net/downloads/busybox-1.36.1.tar.bz2";
    hash = "sha256:b8cc24c9574d809e7279c3be349795c5d5ceb6fdf19ca709f80cde50e47de314";
  }
end

return module
