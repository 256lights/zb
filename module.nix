# Copyright 2025 The zb Authors
# SPDX-License-Identifier: MIT

{ pkgs, lib, config, ...  }:

let
  cfg = config.zb;
in
{
  options.zb = {
    package = lib.mkOption {
      type = lib.types.package;
      description = "The zb package to use";
    };
    installerPackage = lib.mkOption {
      type = lib.types.package;
      description = "The zb installer package to use";
    };

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
    environment.systemPackages = [ cfg.package ];

    users.users = builtins.listToAttrs (
      map (i: {
        name = "${cfg.buildGroup}${toString i}";
        value = {
          description = "zb build user ${toString i}";
          uid = cfg.firstBuildUid + (i - 1);
          group = cfg.buildGroup;
          isSystemUser = true;
        };
      }) (lib.range 1 cfg.userCount)
    );

    users.groups.${cfg.buildGroup} = {
      gid = cfg.buildGid;
      members = map (i: "${cfg.buildGroup}${toString i}") (lib.range 1 cfg.userCount);
    };

    systemd.services.zb-install = {
      description = "zb Install";
      unitConfig = {
        ConditionPathExists = "!/opt/zb/store";
      };
      path = [ pkgs.bash ];
      script = "bash ${cfg.installerPackage}/install --bin '' --build-users-group '' --no-systemd";
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
        ExecStart = "${cfg.package}/bin/zb serve --systemd --sandbox-path=/bin/sh=/opt/zb/store/hpsxd175dzfmjrg27pvvin3nzv3yi61k-busybox-1.36.1/bin/sh --implicit-system-dep=/bin/sh --build-users-group=${cfg.buildGroup}";
        KillMode = "mixed";
      };
    };
  };
}
