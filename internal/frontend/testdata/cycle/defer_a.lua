-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local b = import "defer_b.lua"

local x = {
  offset = 3,
}
return setmetatable(x, {
  -- Accessing each number key calls the b function.
  __index = function(_, k)
    if type(k) == 'number' then
      return b(k)
    else
      return nil
    end
  end,
})
