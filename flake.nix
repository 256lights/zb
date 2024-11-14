{
  inputs = {
    nixpkgs.url = "nixpkgs";
    flake-utils.url = "flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils, ... }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          overlays = [ self.overlays.default ];
        };
      in
      {
        devShells.default = pkgs.mkShell {
          packages = [
            pkgs.delve
            pkgs.go_1_23
            pkgs.gotools
            (pkgs.gopls.override {
              buildGoModule = pkgs.buildGo123Module;
            })
          ];

          hardeningDisable = [ "fortify" ];
        };
      }
    ) // {
      overlays.default = final: prev: {
        go_1_23 = prev.go_1_23.overrideAttrs {
          version = "1.23.2";
          src = prev.fetchurl {
            url = "https://go.dev/dl/go1.23.2.src.tar.gz";
            hash = "sha256-NpMBYqk99BfZC9IsbhTa/0cFuqwrAkGO3aZxzfqc0H8=";
          };
        };
      };
    };
}
