-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

--- baseNameOf returns the last element of path.
--- Trailing slashes are removed before extracting the last element.
--- If the path is empty, baseNameOf returns "".
--- If the path consists entirely of slashes, baseNameOf returns "/".
---@param path string slash-separated path
---@return string
function baseNameOf(path)
  if path == "" then return "." end
  local base = path:match("([^/]*)/*$")
  -- If empty now, it had only slashes.
  if base == "" then return path:sub(1, 1) end
  return base
end

---Cut a string with a separator
---and produces a list of strings which were separated by this separator.
---@param sep string
---@param s string
---@return string[]
function splitString(sep, s)
  local result = {}
  local i = 1
  while true do
    local j = s:find(sep, i, true)
    if not j then break end
    result[#result + 1] = s:sub(i, j - 1)
    i = j + #sep
  end
  result[#result + 1] = s:sub(i)
  return result
end

---Construct a Unix-style search path by appending `subDir`
---to the specified `output` of each of the packages.
---@param output string
---@param subDir string
---@param paths derivation[]
---@return string
function makeSearchPathOutput(output, subDir, paths)
  local parts = {}
  for i, x in ipairs(paths) do
    local xout = x[output]
    if xout then
      if #parts > 0 then
        parts[#parts + 1] = ":"
      end
      parts[#parts + 1] = tostring(xout)
      parts[#parts + 1] = "/"
      parts[#parts + 1] = subDir
    end
  end
  return table.concat(parts)
end

---Construct a binary search path (such as `$PATH`)
---containing the binaries for a set of packages.
---@param pkgs derivation[]
---@return string # colon-separated paths
function mkBinPath(pkgs)
  return makeSearchPathOutput("out", "bin", pkgs)
end

---@param pkgs derivation[]
---@return string
function mkIncludePath(pkgs)
  return makeSearchPathOutput("out", "include", pkgs)
end

---@param pkgs derivation[]
---@return string
function mkLibraryPath(pkgs)
  return makeSearchPathOutput("out", "lib", pkgs)
end
