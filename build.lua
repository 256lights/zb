-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

-- This is the zb build script that builds zb. :)

local stdlib <const> = fetchArchive {
  url = "https://github.com/256lights/zb-stdlib/archive/839b839dc8194f34bf0741e01429168cbb75614c.zip";
  hash = "sha256:9b22a7000fdbef1093f7fd3a3cc16802cfc7f53575bd8e4887cc79be2252ce97";
  name = "zb-stdlib-839b839dc.zip";
}

local seeds <const> = import(stdlib.."/bootstrap/seeds.lua")
local strings <const> = import(stdlib.."/strings.lua")
local systems <const> = import(stdlib.."/systems.lua")
local tables <const> = import(stdlib.."/tables.lua")

local module <const> = {}
local getters <const> = {}

module.gomod = path {
  path = ".";
  name = "zb-source-go.mod";
  filter = function(name)
    return name == "go.mod" or name == "go.sum"
  end;
}

---@param name string
---@param prefix string
---@return boolean
local function allowSubtree(name, prefix)
  return name == prefix or name:sub(1, #prefix + 1) == prefix.."/"
end

function getters.src()
  return path {
    path = ".";
    name = "zb-source";
    filter = function(name)
      local base = strings.baseNameOf(name)
      -- TODO(256lights/zb-stdlib#21): name ~= "internal/ui/public"
      return (allowSubtree(name, "cmd") or
            allowSubtree(name, "internal") or
            allowSubtree(name, "sets") or
            allowSubtree(name, "zbstore") or
            allowSubtree(name, "launchd") or
            allowSubtree(name, "systemd") or
            name == "LICENSE" or
            name == "go.mod" or
            name == "go.sum") and
          name ~= "cmd/zb/zb" and
          name ~= "cmd/zb/zb.exe" and
          name ~= "internal/ui/build" and
          base ~= ".vscode" and
          base ~= "node_modules" and
          base ~= ".git" and
          base ~= ".env"
    end;
  }
end

---@param args {
---makeDerivation: (fun(args: table<string, any>): derivation),
---makeDerivationNoCC: (fun(args: table<string, any>): derivation)?,
---go: derivation|string,
---buildSystem: string,
---targetSystem: string?,
---}
---@return derivation
function module.new(args)
  local targetSystem = systems.parse(args.targetSystem or args.buildSystem)
  if not targetSystem then
    error(string.format("invalid target system %q", args.targetSystem or args.buildSystem))
  end
  local GOOS, GOARCH
  if targetSystem.isLinux then
    GOOS = "linux"
  elseif targetSystem.isMacOS then
    GOOS = "darwin"
  elseif targetSystem.isWindows then
    GOOS = "windows"
  else
    error(string.format("unsupported OS for %q", tostring(targetSystem)))
  end
  if targetSystem.isX86 and targetSystem.is64Bit then
    GOARCH = "amd64"
  elseif targetSystem.isX86 and targetSystem.is32Bit then
    GOARCH = "386"
  elseif targetSystem.isARM and targetSystem.is64Bit then
    GOARCH = "arm64"
  elseif targetSystem.isARM and targetSystem.is32Bit then
    GOARCH = "arm"
  else
    error(string.format("unsupported architecture for %q", tostring(targetSystem)))
  end

  local modules = (args.makeDerivationNoCC or args.makeDerivation) {
    pname = "zb-go-modules";
    src = module.gomod;
    buildSystem = args.buildSystem;

    -- GOOS/GOARCH not needed for downloading.
    -- Omitting it allows all targets to reuse the same derivation.
    PATH = strings.makeBinPath {
      args.go,
    };

    __network = true;

    preBuild = [[export GOCACHE="$ZB_BUILD_TOP/cache"; export GOMODCACHE="$(pwd)/mod"]];
    buildPhase = [[go mod download]];
    installPhase = [[cp --reflink=auto -R "$GOMODCACHE" "$out"]];
  }
  local busybox
  local seedsForSystem = seeds[args.targetSystem or args.buildSystem]
  if seedsForSystem then
    busybox = seedsForSystem.busybox
  end
  return args.makeDerivation {
    pname = "zb";
    src = module.src;
    buildSystem = args.buildSystem;

    GOOS = GOOS;
    GOARCH = GOARCH;
    GOMODCACHE = modules;
    PATH = strings.makeBinPath {
      args.go,
    };

    busybox = busybox;

    preBuild = [[export GOCACHE="$ZB_BUILD_TOP/cache"]];
    buildPhase = [[go build -trimpath -ldflags="-s -w" zb.256lights.llc/pkg/cmd/zb]];
    installPhase = [=[
mkdir -p "$out/bin"
name="zb$(go env GOEXE)"
cp --reflink=auto "$name" "$out/bin/$name"

if [[ "$GOOS" = linux ]]; then
  mkdir -p "$out/lib/systemd/system"
  cp systemd/zb-serve.socket "$out/lib/systemd/system/zb-serve.socket"
  sed \
    -e "s:@zb@:$out/bin/zb:g" \
    -e "s:@sh@:$busybox/bin/sh:g" \
    systemd/zb-serve.service.in > "$out/lib/systemd/system/zb-serve.service"
elif [[ "$GOOS" = darwin ]]; then
  mkdir -p "$out/Library/LaunchDaemons"
  sed \
    -e "s:@zb@:$out/bin/zb:g" \
    launchd/dev.zb-build.serve.plist.in > "$out/Library/LaunchDaemons/dev.zb-build.serve.plist"
fi
]=];
  }
end

local supportedBuildSystems <const> = {
  "x86_64-unknown-linux",
}

local supportedTargetSystems <const> = {
  "x86_64-unknown-linux",
  "x86_64-pc-windows",
  "aarch64-apple-macos",
}

for _, buildSystem in ipairs(supportedBuildSystems) do
  local modTable = {}
  local function new(buildSystem, targetSystem)
    return function()
      local go <const> = import(stdlib.."/packages/go/go.lua")
      local stdenv <const> = import(stdlib.."/stdenv/stdenv.lua")

      return module.new {
        makeDerivation = stdenv.makeDerivationNoCC;
        buildSystem = buildSystem;
        targetSystem = targetSystem;
        go = go[buildSystem]["1.24.2"];
      }
    end
  end
  modTable.zb = new(buildSystem, buildSystem)
  for _, targetSystem in ipairs(supportedTargetSystems) do
    modTable["zb-"..targetSystem] = new(buildSystem, targetSystem)
  end

  module[buildSystem] = tables.lazyModule(modTable)
end

return setmetatable(module, { __index = tables.lazyModule(getters) })
