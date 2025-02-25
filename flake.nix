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
        devShells.default = pkgs.mkShell {
          packages = [
            (pkgs.delve.override {
              inherit buildGoModule;
            })
            go
            pkgs.gotools  # stringer, etc.
            (pkgs.gopls.override {
              inherit buildGoModule;
            })
          ];

          hardeningDisable = [ "fortify" ];
        };
      }
    );
}
