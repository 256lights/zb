{
  inputs = {
    nixpkgs.url = "nixpkgs";
    flake-utils.url = "flake-utils";
  };

  outputs = { nixpkgs, flake-utils, ... }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        devShells.default = pkgs.mkShell {
          packages = [
            pkgs.delve
            pkgs.go_1_23
            (pkgs.gopls.override {
              buildGoModule = pkgs.buildGo123Module;
            })
          ];

          hardeningDisable = [ "fortify" ];
        };
      }
    ) // {
      lib = {};
      overlays = {};
      nixosModules = {};
      nixosConfigurations = {};
    };
}
