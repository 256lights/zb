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
---@generic K, T, U
---@param f fun(T, K): U
---@param list T[]
---@return U[]
function map(f, list)
  local result = {}
  for k, x in pairs(list) do
    result[k] = f(x, k)
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

---@param x any
---@return boolean
local function isLazyKey(x)
  local tp = type(x)
  return tp == "number" or tp == "string" or tp == "boolean"
end

---A plug-compatible pure Lua implementation of lazy table
---(as specified in https://github.com/256lights/zb/issues/83).
---It does not perform memoization, so it may be frozen.
---@generic K: string|boolean|number
---@param f fun(t: table<K, any>, k: K): any
---@param init? table<K, any>
---@return table<K, any>
function lazy(f, init)
  local obj = {}
  local ff = function(_, k)
    if isLazyKey(k) then
      return f(obj, k)
    else
      return nil
    end
  end
  local mt = {
    __index = ff;
    __newindex = function()
      error("cannot modify lazy table")
    end;
    __metatable = false;
  }
  if init then
    local t = {}
    for k, v in pairs(init) do
      if isLazyKey(k) then
        t[k] = v
      end
    end
    mt.__index = setmetatable(t, { __index = ff })
  end
  return setmetatable(obj, mt)
end

---Returns a lazy table derived from t
---where all function values in t are treated as getters.
---@generic K: string|boolean|number
---@param t table<K, any>
---@return table<K, any>
function lazyModule(t)
  ---@type table<any, any>
  local init = {}
  ---@type table<any, fun(): any>
  local accessors = {}

  for k, v in pairs(t) do
    if isLazyKey(k) then
      if type(v) == "function" then
        accessors[k] = v
      else
        init[k] = v
      end
    end
  end

  local function lazyNext(_, k)
    local v
    if k == nil or init[k] ~= nil then
      k, v = next(init, k)
      if k == nil then
        k, v = next(accessors, k)
        if v then v = v() end
      end
    else
      k, v = next(accessors, k)
      if v then v = v() end
    end
    if k == nil then
      return nil
    end
    return k, v
  end

  return setmetatable({}, {
    __metatable = false;
    __index = lazy(function(_, k)
      local f = accessors[k]
      if f then
        return f()
      else
        return nil
      end
    end, init);
    __setindex = function()
      error("cannot modify lazy module")
    end;
    __pairs = function(obj)
      return lazyNext, obj, nil
    end;
  })
end

---Returns an object whose fields apply f to t as they are accessed.
---@generic K: string|boolean|number
---@generic U, V
---@param f fun(U, K): V
---@param t table<K, U>
---@return table<K, V>
function lazyMap(f, t)
  local function lazyMapNext(obj, k)
    local v
    repeat
      k = next(t, k)
      if k == nil then
        return nil
      end
      if isLazyKey(k) then
        v = obj[k]
      end
    until v ~= nil
    return k, v
  end

  return setmetatable({}, {
    __metatable = false;
    __index = lazy(function(_, k)
      local x = t[k]
      if x then
        return f(x, k)
      else
        return nil
      end
    end);
    __setindex = function()
      error("cannot modify lazy module")
    end;
    __pairs = function(obj)
      return lazyMapNext, obj, nil
    end;
  })
end
