# Copyright 2025 The zb Authors
# SPDX-License-Identifier: MIT

{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      ...
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs {
          inherit system;
        };

        inherit (pkgs.lib.attrsets) optionalAttrs;

        go = pkgs.go_1_24;
        buildGoModule = pkgs.buildGo124Module;

        zbPackage = pkgs.callPackage ./package.nix {
          inherit buildGoModule;
        };

        installerPackage = pkgs.callPackage ./installer {};
      in
      {
        devShells.default = pkgs.mkShellNoCC {
          packages = [
            # Go tooling.
            (pkgs.delve.override {
              inherit buildGoModule;
            })
            go
            (pkgs.gopls.override {
              inherit buildGoModule;
            })

            # JavaScript tooling.
            pkgs.nodejs_22
          ];

          hardeningDisable = [ "fortify" ];
        };

        packages = {
          default = zbPackage;
        } // optionalAttrs (builtins.elem system installerPackage.meta.platforms) {
          installer = installerPackage;
        };
      }
    )
    // {
      nixosModules.default = {pkgs, lib, ... }:
        let
          # Slightly higher than option defaults (1500),
          # but lower than lib.mkDefault (1000).
          mkDefault = lib.mkOverride 1400;

          selfPackages = self.packages.${pkgs.hostSystem.system};
        in
        {
          imports = [ ./module.nix ];
          config.zb = {
            package = mkDefault selfPackages.default;
            installerPackage = mkDefault selfPackages.installer;
          };
        };
    };
}
