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
      in
      {
        devShells.default = pkgs.mkShell {
          packages = [
            pkgs.delve
            pkgs.go_1_24
            pkgs.gotools  # stringer, etc.
            (pkgs.gopls.override {
              buildGoModule = pkgs.buildGo124Module;
            })
          ];

          hardeningDisable = [ "fortify" ];
        };
      }
    );
}
