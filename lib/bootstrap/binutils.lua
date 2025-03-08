-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local module <const> = {}

local tarballArgs <const> = {
  ["2.30"] = {
    url = "https://mirrors.kernel.org/gnu/binutils/binutils-2.30.tar.xz";
    hash = "sha256:6e46b8aeae2f727a36f0bd9505e405768a72218f1796f0d09757d45209871ae6";
  };
  ["2.41"] = {
    url = "https://mirrors.kernel.org/gnu/binutils/binutils-2.41.tar.xz";
    hash = "sha256:ae9a5789e23459e59606e6714723f2d3ffc31c03174191ef0d015bdf06007450";
  };
}

module.tarballs = setmetatable({}, {
  __index = function(_, k)
    local args = tarballArgs[k]
    if args then return fetchurl(args) end
  end;
})

return module
