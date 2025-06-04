{
  config,
  dream2nix,
  ...
}:
{
  imports = [
    dream2nix.modules.dream2nix.nodejs-package-lock-v3
    dream2nix.modules.dream2nix.nodejs-granular-v3
  ];
  deps =
    { nixpkgs, ... }:
    {
      inherit (nixpkgs)
        stdenv
        ;
    };
  mkDerivation = {
    src = ./internal/ui;
  };
  nodejs-package-lock-v3 = {
    packageLockFile = "${config.mkDerivation.src}/package-lock.json";
  };
  nodejs-granular-v3 = {
    runBuild = true;
    buildScript = "npm run build:prod";
  };
  name = "zb-node";
}
