-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local function forArchitecture(arch)
  local m4 = import("../m4.lua") {
    bash = "/usr";
    PATH = os.getenv("PATH");
    ARCH = arch;
    CC = "gcc";
    AR = "ar";
    CFLAGS = "-Wno-implicit-function-declaration";
  }

  return m4
end

return setmetatable({}, {
  __index = function(_, k)
    for arch, zbArch in pairs(import("../steps.lua").archToZBTable) do
      if zbArch == k then return forArchitecture(arch) end
    end
    return nil
  end;
})
