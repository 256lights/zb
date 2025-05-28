# Copyright 2025 The zb Authors
# SPDX-License-Identifier: MIT

{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
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
        { pkgs, ... }:
        let
          zb = self.packages.${pkgs.system}.default;

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
          environment.variables.PATH = "/opt/zb/bin";

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
              ExecStart = "/opt/zb/bin/zb serve --systemd --sandbox-path=/bin/sh=/opt/zb/store/hpsxd175dzfmjrg27pvvin3nzv3yi61k-busybox-1.36.1/bin/sh --build-users-group=${buildGroup} $ZB_SERVE_FLAGS";
              KillMode = "mixed";
            };
          };
        };
    };
}
