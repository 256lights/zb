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
        inherit (pkgs.lib.strings) concatStringsSep makeIncludePath makeLibraryPath;

        go = pkgs.go_1_24;
        buildGoModule = pkgs.buildGo124Module;

        gcc = pkgs.gcc-unwrapped;
        libc = gcc.libc_dev.out;

        gccVersion = "14.2.1"; # name used in directory, which sadly != gcc.version.
        cIncludePath = [
          "${gcc}/lib/gcc/${pkgs.targetPlatform.config}/${gccVersion}/include"
          "${gcc}/include"
          "${gcc}/lib/gcc/${pkgs.targetPlatform.config}/${gccVersion}/include-fixed"
          "${libc.dev}/include"
        ];
        cplusIncludePath = [
          "${gcc}/include/c++/${gcc.version}/"
          "${gcc}/include/c++/${gcc.version}//${pkgs.targetPlatform.config}"
          "${gcc}/include/c++/${gcc.version}//backward"
        ] ++ cIncludePath;
      in
      {
        devShells.default = pkgs.mkShellNoCC {
          packages = [
            gcc
            (pkgs.delve.override {
              inherit buildGoModule;
            })
            libc.bin
            go
            pkgs.gotools  # stringer, etc.
            (pkgs.gopls.override {
              inherit buildGoModule;
            })
          ];

          shellHook = ''
            export C_INCLUDE_PATH='${concatStringsSep ":" cIncludePath}'
            export CPLUS_INCLUDE_PATH='${concatStringsSep ":" cplusIncludePath}'
            export LIBRARY_PATH='${makeLibraryPath [ gcc libc ]}'
            export ZB_BOOTSTRAP_SYSTEM_HEADERS='${makeIncludePath [ libc.dev ]}'
          '';

          hardeningDisable = [ "fortify" ];
        };
      }
    );
}
