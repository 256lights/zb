-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

---Reports whether a value equal to x occurs in the table t.
---@generic T
---@param x T
---@param t table<any, T>
---@return boolean
function elem(x, t)
  for _, v in pairs(t) do
    if v == x then return true end
  end
  return false
end

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
