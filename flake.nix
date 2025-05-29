# Copyright 2025 The zb Authors
# SPDX-License-Identifier: MIT

{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "flake-utils";
  };

  outputs =
    {
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
    )
    // {
      nixosModules.default =
        {
          pkgs,
          lib,
          config,
          ...
        }:
        {
          options.zb = {
            buildGroup = lib.mkOption {
              type = lib.types.str;
              default = "zbld";
              description = "Group Name for the build users";
            };
            buildGid = lib.mkOption {
              type = lib.types.int;
              default = 256000;
              description = "Group ID for the build users";
            };
            firstBuildUid = lib.mkOption {
              type = lib.types.int;
              default = 256001;
              description = "First user ID for the build users, will increment for each";
            };
            userCount = lib.mkOption {
              type = lib.types.int;
              default = 32;
              description = "Number of build users to create";
            };
            binPath = lib.mkOption {
              type = lib.types.str;
              default = "/opt/zb/bin";
              description = "Path to install the zb binary";
            };
          };

          config = {
            environment.variables.PATH = config.zb.binPath;

            users.users = builtins.listToAttrs (
              map (i: {
                name = "${config.zb.buildGroup}${toString i}";
                value = {
                  description = "zb build user ${toString i}";
                  uid = config.zb.firstBuildUid + (i - 1);
                  group = config.zb.buildGroup;
                  isSystemUser = true;
                };
              }) (pkgs.lib.range 1 config.zb.userCount)
            );

            users.groups.${config.zb.buildGroup} = {
              gid = config.zb.buildGid;
              members = map (i: "${config.zb.buildGroup}${toString i}") (pkgs.lib.range 1 config.zb.userCount);
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
                ZB_BUILD_USERS_GROUP = config.zb.buildGroup;
                ZB_SERVE_FLAGS = "";
              };
              serviceConfig = {
                ExecStart = "/opt/zb/bin/zb serve --systemd --sandbox-path=/bin/sh=/opt/zb/store/hpsxd175dzfmjrg27pvvin3nzv3yi61k-busybox-1.36.1/bin/sh --build-users-group=${config.zb.buildGroup} $ZB_SERVE_FLAGS";
                KillMode = "mixed";
              };
            };
          };
        };
    };
}
