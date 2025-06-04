-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

-- This is the zb build script that builds zb. :)

local stdlib <const> = fetchArchive {
  url = "https://github.com/256lights/zb-stdlib/releases/download/v0.1.0/zb-stdlib-v0.1.0.tar.gz";
  hash = "sha256:dd040fe8baad8255e4ca44b7249cddfc24b5980f707a29c3b3e2b47f5193ea48";
}

local go <const> = import(stdlib.."/packages/go/go.lua")
local seeds <const> = import(stdlib.."/bootstrap/seeds.lua")
local strings <const> = import(stdlib.."/strings.lua")
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
      return (allowSubtree(name, "bytebuffer") or
            allowSubtree(name, "cmd") or
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
  local goEnv = go.envForSystem(args.targetSystem or args.buildSystem)

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
    version = "0.1.0-rc2";
    src = module.src;
    buildSystem = args.buildSystem;

    GOOS = goEnv.GOOS;
    GOARCH = goEnv.GOARCH;
    GOMODCACHE = modules;
    PATH = strings.makeBinPath {
      args.go,
    };

    busybox = busybox;

    preBuild = [[export GOCACHE="$ZB_BUILD_TOP/cache"]];
    buildPhase = [[go build -trimpath -ldflags="-s -w -X main.zbVersion=$version" zb.256lights.llc/pkg/cmd/zb]];
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
  "aarch64-apple-macos",
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
