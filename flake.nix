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
        inherit (pkgs.lib.attrsets) getOutput;
        inherit (pkgs.lib.strings) concatStringsSep makeIncludePath makeLibraryPath;

        go = pkgs.go_1_24;
        buildGoModule = pkgs.buildGo124Module;

        gcc = pkgs.gcc-unwrapped;
        libc_dev = gcc.libc_dev;

        gccVersion = "14.2.1"; # name used in directory, which sadly != gcc.version.
        cIncludePath = [
          "${gcc}/lib/gcc/${pkgs.targetPlatform.config}/${gccVersion}/include"
          "${gcc}/include"
          "${gcc}/lib/gcc/${pkgs.targetPlatform.config}/${gccVersion}/include-fixed"
          (makeIncludePath [libc_dev])
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
            # C/C++ tooling.
            gcc
            (libc_dev.bin or libc_dev)
            pkgs.binutils-unwrapped

            # Go tooling.
            (pkgs.delve.override {
              inherit buildGoModule;
            })
            go
            pkgs.gotools  # stringer, etc.
            (pkgs.gopls.override {
              inherit buildGoModule;
            })

            # JavaScript tooling.
            pkgs.nodejs_22
          ];

          shellHook = ''
            export C_INCLUDE_PATH='${concatStringsSep ":" cIncludePath}'
            export CPLUS_INCLUDE_PATH='${concatStringsSep ":" cplusIncludePath}'
            export LIBRARY_PATH='${makeLibraryPath [ gcc (libc_dev.lib or libc_dev.out or libc_dev) ]}'
            export ZB_BOOTSTRAP_SYSTEM_HEADERS='${makeIncludePath [ libc_dev ]}'
          '';

          hardeningDisable = [ "fortify" ];
        };
      }
    );
}
