-- Copyright 2024 Ross Light
-- SPDX-License-Identifier: MIT

---@param args {url: string, hash: string, name: string?, executable: boolean?}
---@return derivation
function fetchurl(args)
  local name = args.name or baseNameOf(args.url)
  local outputHashMode = "flat"
  if args.executable then
    outputHashMode = "recursive"
  end
  return derivation {
    name = name;
    builder = "builtin:fetchurl";
    system = "builtin";

    url = args.url;
    urls = { args.url };
    executable = args.executable or false;
    unpack = false;
    outputHash = args.hash;
    outputHashMode = outputHashMode;
    preferLocalBuild = true;
    impureEnvVars = { "http_proxy", "https_proxy", "ftp_proxy", "all_proxy", "no_proxy" };
  }
end

---@generic T, U
---@param f fun(T): U
---@param list T[]
---@return U[]
function table.map(f, list)
  local result = {}
  for i, x in ipairs(list) do
    result[i] = f(x)
  end
  return result
end

---@generic T
---@param x T
---@param xs T[]
---@return boolean
function table.elem(x, xs)
  for _, y in ipairs(xs) do
    if x == y then return true end
  end
  return false
end

---@generic T
---@param ... T[]
---@return T[]
function table.concatLists(...)
  local result = {}
  for i = 1, select("#", ...) do
    local t = select(i, ...)
    table.move(t, 1, #t, #result + 1, result)
  end
  return result
end
