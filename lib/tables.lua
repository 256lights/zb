-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

---Copy the pairs from each argument into the first argument.
---@generic T: table
---@param t T
---@param ... table
---@return T
function update(t, ...)
  local n <const> = select("#", ...)
  for i = 1, n do
    for k, v in pairs(select(i, ...)) do
      t[k] = v
    end
  end
  return t
end
