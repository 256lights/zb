-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local b = import "defer_b.lua"

local x = {
  offset = 3,
}
return setmetatable(x, {
  -- Accessing each number key calls the b function.
  __index = function(_, k)
    local n = tonumber(k)
    if n then
      return b(n)
    else
      return nil
    end
  end,
})
