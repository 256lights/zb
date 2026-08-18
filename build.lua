-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

-- This is the zb build script that builds zb. :)

local stdlib <const> = fetchArchive {
  url = "https://github.com/256lights/zb-stdlib/releases/download/v0.2.0/zb-stdlib-v0.2.0.tar.gz";
  hash = "sha256:9991a0f854c4302b83b50e731e25e2b3120e23a807794b87c29a2c7e6520469b";
}

local go <const> = import(stdlib.."/packages/go/go.lua")
local seeds <const> = import(stdlib.."/bootstrap/seeds.lua")
local stdenv <const> = import(stdlib.."/stdenv/stdenv.lua")
local strings <const> = import(stdlib.."/strings.lua")
local tables <const> = import(stdlib.."/tables.lua")

local module <const> = {}
local getters <const> = {}

local version <const> = "0.2.0-beta5"

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
          base ~= ".env" and
          not base:find("%.js%.map$") and
          not base:find("%.css%.map$")
    end;
  }
end

local zbTarget = {}

---@param args {
---makeDerivation: (fun(args: table<string, any>): derivation),
---makeDerivationNoCC: (fun(args: table<string, any>): derivation)?,
---go: any,
---targetSystem: string?,
---}
---@return derivation
function module.new(args)
  local t = tables.clone(args)
  t.version = version
  t.gomod = module.gomod
  return setmetatable(t, zbTarget)
end

function zbTarget:__index(key)
  if key == "src" then
    return module.src
  else
    return nil
  end
end

function zbTarget:__outputs(buildSystem)
  local goEnv = go.envForSystem(self.targetSystem or buildSystem)

  local goToolchain = outputs(self.go, buildSystem)[""]

  local modules = (self.makeDerivationNoCC or self.makeDerivation) {
    pname = "zb-go-modules";
    src = module.gomod;
    buildSystem = buildSystem;

    -- GOOS/GOARCH not needed for downloading.
    -- Omitting it allows all targets to reuse the same derivation.
    PATH = strings.makeBinPath {
      goToolchain,
    };

    __network = true;

    preBuild = [[export GOCACHE="$ZB_BUILD_TOP/cache"; export GOMODCACHE="$(pwd)/mod"]];
    buildPhase = [[go mod download]];
    installPhase = [[cp --reflink=auto -R "$GOMODCACHE" "$out"]];
  }
  local busybox
  local seedsForSystem = seeds[self.targetSystem or buildSystem]
  if seedsForSystem then
    busybox = seedsForSystem.busybox
  end
  return self.makeDerivation {
    pname = "zb";
    version = version;
    src = module.src;
    buildSystem = buildSystem;

    GOOS = goEnv.GOOS;
    GOARCH = goEnv.GOARCH;
    GOMODCACHE = modules;
    PATH = strings.makeBinPath {
      goToolchain,
    };

    busybox = busybox;

    preBuild = [[export GOCACHE="$ZB_BUILD_TOP/cache"]];
    -- See https://pkg.go.dev/cloud.google.com/go/storage#hdr-gRPC_API
    -- for info about -tags=disable_grpc_modules.
    buildPhase = [[go build -trimpath -ldflags="-s -w -X main.zbVersion=$version" -tags=disable_grpc_modules zb.256lights.llc/pkg/cmd/zb]];
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

local supportedTargetSystems <const> = {
  "x86_64-unknown-linux",
  "x86_64-pc-windows",
  "aarch64-apple-macos",
}

local mygo = setmetatable({}, {
  __outputs = function(_, system)
    return go[system]["1.26"]
  end;
})

module.zb = module.new {
  makeDerivation = stdenv.makeDerivationNoCC;
  go = mygo;
}
for _, targetSystem in ipairs(supportedTargetSystems) do
  module["zb-"..targetSystem] = module.new {
    makeDerivation = stdenv.makeDerivationNoCC;
    go = mygo;
    targetSystem = targetSystem;
  }
end

return setmetatable(module, { __index = tables.lazyModule(getters) })
