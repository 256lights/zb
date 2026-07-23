{ buildGoModule
, lib
, installShellFiles
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
  tags = [
    "disable_grpc_modules"
  ];

  nativeBuildInputs = [
    installShellFiles
  ];

  src = ./.;

  vendorHash = "sha256-Dq8xShcG8RmV3wFTz+s8HBMgyHgzOfDi+2beeMb4zw4=";
  goSum = builtins.readFile ./go.sum;

  postInstall = ''
    installShellCompletion --cmd zb \
      --bash <($out/bin/zb completion -c bash) \
      --fish <($out/bin/zb completion -c fish) \
      --zsh <($out/bin/zb completion -c zsh)
  '';

  meta = {
    description = "An experiment in hermetic, reproducible build systems.";
    homepage = "https://zb.256lights.llc/";
    downloadPage = "https://github.com/256lights/zb/releases/latest";
    license = lib.licenses.mit;
  };
}
