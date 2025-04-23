-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local tables <const> = import "../tables.lua"

local seeds <const> = {
  ["x86_64-linux"] = {
    gcc = "/zb/store/c84havjwrngg0n9y2ccswp8hm4p495jv-gcc-4.2.1";
    busybox = "/zb/store/z5yrbqk8sjlzyvw8wpicsn2ybk0sc470-busybox-1.36.1";
    -- TODO(someday): Callers should be able to infer based on system string.
    target = "x86_64-unknown-linux-musl";
  };
}

return tables.map(function(t)
  return tables.lazyModule {
    gcc = function () return storePath(t.gcc) end;
    busybox = function () return storePath(t.busybox) end;
    target = t.target;
  }
end, seeds)
