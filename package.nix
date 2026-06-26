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

  nativeBuildInputs = [
    installShellFiles
  ];

  src = ./.;

  vendorHash = "sha256-NoNRo3MhT2o2EoD6Jd28meXigHzNV2EcLTBEWTMH8vo=";
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
