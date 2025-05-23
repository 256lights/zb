# Copyright 2025 The zb Authors
# SPDX-License-Identifier: MIT

{
  inputs = {
    nixpkgs.url = "nixpkgs";
    nixpkgs-unstable.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "flake-utils";

    dream2nix.url = "github:nix-community/dream2nix";
    dream2nix.inputs.nixpkgs.follows = "nixpkgs-unstable";
  };

  outputs =
    {
      nixpkgs,
      nixpkgs-unstable,
      flake-utils,
      dream2nix,
      ...
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs {
          inherit system;
        };
        pkgs-unstable = import nixpkgs-unstable {
          inherit system;
        };

        go = pkgs.go_1_24;
        buildGoModule = pkgs.buildGo124Module;

        nodejs = pkgs.nodejs_22;

        nodeOutputs = dream2nix.lib.evalModules {
          packageSets.nixpkgs = nixpkgs.legacyPackages.${system};
          modules = [
            ./node.nix
            {
              paths.projectRoot = ./.;
              paths.projectRootFile = "flake.nix";
              paths.package = ./.;
            }
          ];
        };
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
            nodejs
          ];

          hardeningDisable = [ "fortify" ];
        };

        packages.default = buildGoModule {
          pname = "zb";
          version = "0.1.0";

          preBuild = ''
            HOME=$PWD
            cp -r ${nodeOutputs}/lib/node_modules/zb-node/public ./internal/ui/public
          '';

          ldflags = [
            "-s -w"
          ];

          nativeBuildInputs = [
            nodejs
            pkgs-unstable.tailwindcss_4
          ];

          src = ./.;

          vendorHash = "sha256-1YGUmGGOF4MbL2ucUX0zPe8VS6kaG5ewSSlo+eBsFQk=";
        };

      }
    )
    // {
      nixosModules.default =
        { pkgs, ... }:
        let
          buildGroup = "zbld";
          buildGid = 256000;
          firstBuildUid = 256001;
          userCount = 32;
          userNames = map (i: "${buildGroup}${toString i}") (pkgs.lib.range 1 userCount);
          userConfigs = builtins.listToAttrs (
            map (i: {
              name = "${buildGroup}${toString i}";
              value = {
                description = "zb build user ${toString i}";
                uid = firstBuildUid + (i - 1);
                group = buildGroup;
                isSystemUser = true;
              };
            }) (pkgs.lib.range 1 userCount)
          );
        in
        {
          users.users = userConfigs;
          users.groups.${buildGroup} = {
            gid = buildGid;
            members = userNames;
          };
          systemd.sockets.zb-serve = {
            description = "zb Store Server Socket";
            before = [ "multi-user.target" ];
            unitConfig = {
              RequiresMountsFor = [ "/opt/zb" ];
              ConditionPathIsReadWrite = "/opt/zb/var/zb";
            };
            listenStreams = [ "/opt/zb/var/zb/server.sock" ];
            wantedBy = [ "sockets.target" ];
          };
          systemd.services.zb-serve = {
            description = "zb Store Server";
            requires = [ "zb-serve.socket" ];
            unitConfig = {
              RequiresMountsFor = [
                "/opt/zb/store"
                "/opt/zb/var"
                "/opt/zb/var/zb"
              ];
              ConditionPathIsReadWrite = "/opt/zb/var/zb";
            };
            environment = {
              ZB_BUILD_USERS_GROUP = buildGroup;
              ZB_SERVE_FLAGS = "";
            };
            serviceConfig = {
              # TODO: Point to derivation
              ExecStart = "/opt/zb/store/drp6dpilg3myng650cbn3zlqd7axari0-zb-0.1.0-rc1/bin/zb serve --systemd --sandbox-path=/bin/sh=/opt/zb/store/hpsxd175dzfmjrg27pvvin3nzv3yi61k-busybox-1.36.1/bin/sh --build-users-group=zbld $ZB_SERVE_FLAGS";
            };
          };
        };
    };
}
