{ buildGoModule
, lib
}:

let
  version = "0.1.0";
in

buildGoModule {
  pname = "zb";
  version = version;

  ldflags = [
    "-s -w -X main.zbVersion=${version}"
  ];

  src = ./.;

  vendorHash = "sha256-ANQ4gVjYrFdGj/019FZzKO68Oq11P3/qKsAYzJb4X/E=";

  meta = {
    description = "An experiment in hermetic, reproducible build systems.";
    homepage = "https://zb.256lights.llc/";
    downloadPage = "https://github.com/256lights/zb/releases/latest";
    license = lib.licenses.mit;
  };
}
