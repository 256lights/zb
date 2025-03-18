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
