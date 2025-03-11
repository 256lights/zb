-- Copyright 2024 The zb Authors
-- SPDX-License-Identifier: MIT

---@param system string
---@return boolean
local function isWindows(system)
  return system:find("-windows$") ~= nil
end

local function forSystem(_, currentSystem)
  local drvName <const> = "hello.lua"
  local content <const> = [[return "Hello, World!"]]
  local drv
  if isWindows(currentSystem) then
    drv = derivation {
      name = drvName;
      builder = [[C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe]];
      args = {
        "-Command",
        [[${env:content} | Out-File -Encoding ascii -FilePath ${env:out}]],
      };
      content = content;
      system = currentSystem;
    }
  else
    drv = derivation {
      name = drvName;
      builder = "/bin/sh";
      args = {
        "-c",
        [[echo "$content" > "$out"]],
      };
      content = content;
      system = currentSystem;
    }
  end

  return import(drv.out)
end

local t = {}
setmetatable(t, {
  __index = forSystem;
})
return t
