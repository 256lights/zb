{
  inputs = {
    nixpkgs.url = "nixpkgs";
    flake-utils.url = "flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils, ... }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        packages.default = pkgs.hello;

        apps.default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/hello";
        };

        devShells.default = pkgs.mkShell {
          packages = [
            pkgs.delve
            pkgs.go_1_22
            pkgs.gopls
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
