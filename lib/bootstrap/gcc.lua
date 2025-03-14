-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local fetchGNU <const> = import "../fetchgnu.lua"

local module <const> = {}

local tarballArgs <const> = {
  ["4.2.1"] = {
    path = "gcc/gcc-4.2.1/gcc-4.2.1.tar.bz2";
    hash = "sha256:ca0a12695b3bccfa8628509e08cb9ed7d8ed48deff0a299e4cb8de87d2c1fced";
  };
  ["4.7.4"] = {
    path = "gcc/gcc-4.7.4/gcc-4.7.4.tar.bz2";
    hash = "sha256:92e61c6dc3a0a449e62d72a38185fda550168a86702dea07125ebd3ec3996282";
  };
  ["13.1.0"] = {
    path = "gcc/gcc-13.1.0/gcc-13.1.0.tar.xz";
    hash = "sha256:61d684f0aa5e76ac6585ad8898a2427aade8979ed5e7f85492286c4dfc13ee86";
  };
}

module.tarballs = setmetatable({}, {
  __index = function(_, k)
    local args = tarballArgs[k]
    if args then return fetchGNU(args) end
  end;
})

return module
