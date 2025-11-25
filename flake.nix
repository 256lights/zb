# Copyright 2025 The zb Authors
# SPDX-License-Identifier: MIT
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs =
    {
      self,
      nixpkgs,
      ...
    }:
    let
      inherit (nixpkgs) lib;

      forEachSystem =
        fn: lib.genAttrs lib.systems.flakeExposed (system: fn system nixpkgs.legacyPackages.${system});
    in
    {
      devShells = forEachSystem (
        _: pkgs: {
          default = pkgs.mkShellNoCC {
            packages = [
              # Go tooling.
              (pkgs.delve.override {
                buildGoModule = pkgs.buildGo125Module;
              })
              pkgs.go_1_25
              pkgs.gopls

              # JavaScript tooling.
              pkgs.nodejs_22
            ];

            # Since using Go 1.25 from nixpkgs,
            # the Go tool seems to try to use cgo for net and os/user,
            # even though it is not necessary.
            # We disable cgo forcibly for consistency.
            CGO_ENABLED = "0";

            hardeningDisable = [ "fortify" ];
          };
        }
      );

      packages = forEachSystem (
        system: pkgs:
        let
          buildGoModule = pkgs.buildGo125Module;

          zbPackage = pkgs.callPackage ./package.nix {
            inherit buildGoModule;
          };

          installerPackage = pkgs.callPackage ./installer { };
        in
        (
          {
            default = zbPackage;
          }
          // lib.optionalAttrs (builtins.elem system installerPackage.meta.platforms) {
            installer = installerPackage;
          }
        )
      );

      nixosModules.default =
        {
          pkgs,
          lib,
          ...
        }:
        let
          # Slightly higher than option defaults (1500),
          # but lower than lib.mkDefault (1000).
          mkDefault = lib.mkOverride 1400;

          selfPackages = self.packages.${pkgs.system};
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
