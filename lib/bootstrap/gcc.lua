-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local fetchGNU <const> = import "../fetchgnu.lua"

local module <const> = {}

local tarballArgs <const> = {
  ["13.1.0"] = {
    path = "gcc/gcc-13.1.0/gcc-13.1.0.tar.xz";
    hash = "sha256:61d684f0aa5e76ac6585ad8898a2427aade8979ed5e7f85492286c4dfc13ee86";
  };
}

module.tarballs = setmetatable({}, {
  __index = function (_, k)
    local args = tarballArgs[k]
    if args then return fetchGNU(args) end
  end;
})

return module
