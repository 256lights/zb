-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local tables <const> = import "tables.lua"

local gnuMirrors <const> = {
  "https://mirrors.kernel.org/gnu/",
  "https://ftp.gnu.org/gnu/",
}

local badGNUURLs <const> = {
  -- Nix's fetchurl seems to un-lzma tarballs from mirrors.kernel.org.
  -- Unclear why.
  "https://mirrors.kernel.org/gnu/coreutils/coreutils-6.10.tar.lzma",
  "https://mirrors.kernel.org/gnu/libtool/libtool-2.2.4.tar.lzma",
}

---@param args {path: string, hash: string}
return function(args)
  for _, mirror in ipairs(gnuMirrors) do
    local url = mirror..args.path
    if not tables.elem(url, badGNUURLs) then
      return fetchurl({
        url = url;
        hash = args.hash;
      })
    end
  end
end
