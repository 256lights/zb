-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local tables <const> = import "../../tables.lua"

local module <const> = {}

local tarballArgs <const> = {
  ["1.1.24"] = {
    url = "https://musl.libc.org/releases/musl-1.1.24.tar.gz";
    hash = "sha256:1370c9a812b2cf2a7d92802510cca0058cc37e66a7bedd70051f0a34015022a3";
  };
  ["1.2.4"] = {
    url = "https://musl.libc.org/releases/musl-1.2.4.tar.gz";
    hash = "sha256:7a35eae33d5372a7c0da1188de798726f68825513b7ae3ebe97aaaa52114f039";
  };
}

module.tarballs = tables.lazyMap(fetchurl, tarballArgs)

---@param args {
---makeDerivation: function,
---system: string,
---version: string,
---shared: boolean?,
---}
---@return derivation
function module.new(args)
  local src = module.tarballs[args.version]
  if not src then
    error("musl.new: unsupported version "..args.version)
  end
  local configureFlags
  if args.shared == false then
    configureFlags = { "--disable-shared" }
  end
  return args.makeDerivation {
    pname = "musl";
    version = args.version;
    system = args.system;
    src = src;
    configureFlags = configureFlags;
  }
end

return module
