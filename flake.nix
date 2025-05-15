# Copyright 2025 The zb Authors
# SPDX-License-Identifier: MIT

{
  inputs = {
    nixpkgs.url = "nixpkgs";
    flake-utils.url = "flake-utils";
  };

  outputs = { nixpkgs, flake-utils, ... }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
        };

        go = pkgs.go_1_24;
        buildGoModule = pkgs.buildGo124Module;
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
      }
    );
}
