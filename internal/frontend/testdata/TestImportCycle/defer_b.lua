-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

return function (x)
  -- Importing inside a function
  -- called after the module is imported
  -- breaks the cycle.
  local a = import "defer_a.lua"
  return x + a.offset
end
