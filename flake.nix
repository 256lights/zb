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

        version = "0.1.0-rc2";

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

        packages.default = buildGoModule {
          pname = "zb";
          version = version;

          ldflags = [
            "-s -w -X main.zbVersion=${version}"
          ];

          src = ./.;

          vendorHash = "sha256-B1DROm8KMfKPupJ7d75Oh8QcJae3UyWwVm8EhnNMayA=";
        };

        packages.installer = pkgs.stdenv.mkDerivation {
          name = "zb-installer";
          version = version;

          dontFixup = true;

          src = pkgs.fetchurl {
            url = "https://github.com/256lights/zb/releases/download/v0.1.0-rc2/zb-v0.1.0-rc2-x86_64-unknown-linux.tar.bz2";
            sha256 = "sha256-wFvYWrPc7t3dzX7vRdXuhBLWJ5ehV2n8A/CxOWhXln0=";
          };

          installPhase = ''
            mkdir -p $out
            cp -a --reflink=auto . $out
          '';
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
        let
          zb = self.packages.${pkgs.system}.default;
          zbInstaller = self.packages.${pkgs.system}.installer;
        in
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
          };

          config = {
            environment.systemPackages = [ zb ];

            users.users = builtins.listToAttrs (
              map (i: {
                name = "${config.zb.buildGroup}${toString i}";
                value = {
                  description = "zb build user ${toString i}";
                  uid = config.zb.firstBuildUid + (i - 1);
                  group = config.zb.buildGroup;
                  isSystemUser = true;
                };
              }) (lib.range 1 config.zb.userCount)
            );

            users.groups.${config.zb.buildGroup} = {
              gid = config.zb.buildGid;
              members = map (i: "${config.zb.buildGroup}${toString i}") (lib.range 1 config.zb.userCount);
            };

            systemd.services.zb-install = {
              description = "zb Install";
              unitConfig = {
                ConditionPathExists = "!/opt/zb/store";
              };
              path = [ pkgs.bash ];
              script = "bash ${zbInstaller}/install --bin '' --build-users-group '' --no-systemd";
              serviceConfig = {
                Type = "oneshot";
              };
            };

            systemd.sockets.zb-serve = {
              description = "zb Store Server Socket";
              before = [ "multi-user.target" ];
              unitConfig = {
                RequiresMountsFor = [ "/opt/zb" ];
              };
              listenStreams = [ "/opt/zb/var/zb/server.sock" ];
              wantedBy = [ "sockets.target" ];
            };

            systemd.services.zb-serve = {
              description = "zb Store Server";
              requires = [
                "zb-serve.socket"
                "zb-install.service"
              ];
              after = [ "zb-install.service" ];
              unitConfig = {
                RequiresMountsFor = [
                  "/opt/zb/store"
                  "/opt/zb/var"
                  "/opt/zb/var/zb"
                ];
                ConditionPathIsReadWrite = "/opt/zb/var/zb";
              };
              serviceConfig = {
                ExecStart = "${zb}/bin/zb serve --systemd --sandbox-path=/bin/sh=/opt/zb/store/hpsxd175dzfmjrg27pvvin3nzv3yi61k-busybox-1.36.1/bin/sh --implicit-system-dep=/bin/sh --build-users-group=${config.zb.buildGroup}";
                KillMode = "mixed";
              };
            };
          };
        };
    };
}
