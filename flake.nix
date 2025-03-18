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
        libc = gcc.libc_dev.out;

        gccVersion = "14.2.1"; # name used in directory, which sadly != gcc.version.
        cIncludePath = [
          "${gcc}/lib/gcc/${pkgs.targetPlatform.config}/${gccVersion}/include"
          "${gcc}/include"
          "${gcc}/lib/gcc/${pkgs.targetPlatform.config}/${gccVersion}/include-fixed"
          (makeIncludePath [libc])
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
            (getOutput "bin" libc)
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
          ];

          shellHook = ''
            export C_INCLUDE_PATH='${concatStringsSep ":" cIncludePath}'
            export CPLUS_INCLUDE_PATH='${concatStringsSep ":" cplusIncludePath}'
            export LIBRARY_PATH='${makeLibraryPath [ gcc libc ]}'
            export ZB_BOOTSTRAP_SYSTEM_HEADERS='${makeIncludePath [ libc ]}'
          '';

          hardeningDisable = [ "fortify" ];
        };
      }
    );
}
