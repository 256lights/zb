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

---Returns a copy of the table where each value is transformed by the given function.
---@generic T, U
---@param f fun(T): U
---@param list T[]
---@return U[]
function map(f, list)
  local result = {}
  for i, x in ipairs(list) do
    result[i] = f(x)
  end
  return result
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

---Concatenate the array tables given as arguments into a new table.
---@generic T
---@param ... T[]
---@return T[]
function concatLists(...)
  local result = {}
  for i = 1, select("#", ...) do
    local t = select(i, ...)
    table.move(t, 1, #t, #result + 1, result)
  end
  return result
end
