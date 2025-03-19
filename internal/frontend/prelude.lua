-- Copyright 2024 The zb Authors
-- SPDX-License-Identifier: MIT

--- baseNameOf returns the last element of path.
--- Trailing slashes are removed before extracting the last element.
--- If the path is empty, baseNameOf returns "".
--- If the path consists entirely of slashes, baseNameOf returns "/".
---@param path string slash-separated path
---@return string
local function baseNameOf(path)
  if path == "" then return "." end
  local base = path:match("([^/]*)/*$")
  -- If empty now, it had only slashes.
  if base == "" then return path:sub(1, 1) end
  return base
end

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

---Strip the store hash from a name, if present.
---@param name string
---@return string
local function stripHash(name)
  local i, j = name:match("^[0123456789abcdfghijklmnpqrsvwxyz]+()-()")
  if i - 1 == 32 then
    return name:sub(j)
  end
  return name
end

---Strip the first suffix found in the argument list.
---@param name string
---@param ... string suffixes to test
---@return string
local function stripSuffixes(name, ...)
  for i = 1, select("#", ...) do
    local suffix = select(i, ...)
    local n = #suffix
    if name:sub(-n) == suffix then
      return name:sub(1, -(n + 1))
    end
  end
  return name
end

---@param args string|{src: string, name: string?, stripFirstComponent: boolean?}
---@return derivation
function extract(args)
  ---@type string
  local src
  ---@type string?
  local name
  ---@type boolean?
  local stripFirstComponent
  if type(args) == "string" then
    src = args
  else
    src = args.src
    stripFirstComponent = args.stripFirstComponent
    name = args.name
  end
  if not name then
    name = stripHash(stripSuffixes(baseNameOf(src), ".tar", ".tar.gz", ".tar.bz2", ".zip"))
  end
  if stripFirstComponent == nil then
    stripFirstComponent = true
  end
  return derivation {
    name = name;
    builder = "builtin:extract";
    system = "builtin";

    src = src;
    stripFirstComponent = stripFirstComponent;
  }
end
