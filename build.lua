-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

-- This is the zb build script that builds zb. :)

local stdlib <const> = fetchArchive {
  url = "https://github.com/256lights/zb-stdlib/archive/171d05f2a6532210c6b0befd0912a714303b394c.zip";
  hash = "sha256:8a2e874e37b5c95173a50b597b183a9965a7e689e0167d8ba4b44793523c4086";
  name = "zb-stdlib-171d05f2a.zip";
}

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

function getters.src()
  return path {
    path = ".";
    name = "zb-source";
    filter = function(name)
      local base = strings.baseNameOf(name)
      -- TODO(256lights/zb-stdlib#21): name ~= "internal/ui/public"
      return name ~= "zb" and
          name ~= "zb.exe" and
          name ~= "cmd/zb/zb" and
          name ~= "cmd/zb/zb.exe" and
          name ~= "internal/ui/build" and
          name ~= ".github" and
          name ~= "demo" and
          name ~= "docs" and
          name ~= "installer" and
          name ~= "tools" and
          name ~= "website" and
          name ~= "build.lua" and
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
---}
---@return derivation
function module.new(args)
  local modules = (args.makeDerivationNoCC or args.makeDerivation) {
    pname = "zb-go-modules";
    src = module.gomod;
    buildSystem = args.buildSystem;

    PATH = strings.makeBinPath {
      args.go,
    };

    __network = true;

    preBuild = [[export GOCACHE="$ZB_BUILD_TOP/cache"; export GOMODCACHE="$(pwd)/mod"]];
    buildPhase = [[go mod download]];
    installPhase = [[cp --reflink=auto -R "$GOMODCACHE" "$out"]];
  }
  return args.makeDerivation {
    pname = "zb";
    src = module.src;
    buildSystem = args.buildSystem;

    GOMODCACHE = modules;
    PATH = strings.makeBinPath {
      args.go,
    };

    preBuild = [[export GOCACHE="$ZB_BUILD_TOP/cache"]];
    buildPhase = [[go build zb.256lights.llc/pkg/cmd/zb]];
    installPhase = [[mkdir -p "$out/bin" && cp --reflink=auto zb "$out/bin/zb"]];
  }
end

local supportedSystems <const> = {
  "x86_64-unknown-linux",
}

for _, system in ipairs(supportedSystems) do
  module[system] = tables.lazyModule {
    zb = function()
      local go <const> = import(stdlib.."/packages/go/go.lua")
      local stdenv <const> = import(stdlib.."/stdenv/stdenv.lua")

      return module.new {
        makeDerivation = stdenv.makeDerivationNoCC;
        buildSystem = system;
        go = go[system]["1.24.2"];
      }
    end;
  }
end

return setmetatable(module, { __index = tables.lazyModule(getters) })
