# Copyright 2025 The zb Authors
# SPDX-License-Identifier: MIT

{ stdenvNoCC
, fetchurl
, lib
}:

let
  version = "0.1.0";

  fetchurlArgs = {
    "x86_64-linux" = {
      url = "https://github.com/256lights/zb/releases/download/v${version}/zb-v${version}-x86_64-unknown-linux.tar.bz2";
      hash = "sha256:b9a5167bdd192041573f79476e5d576714bdeb535fd8b51c0aae0419da641165";
    };

    "aarch64-darwin" = {
      url = "https://github.com/256lights/zb/releases/download/v${version}/zb-v${version}-aarch64-apple-macos.tar.bz2";
      hash = "sha256:b22135691b404c04d53b3890f8fa9e69be2a282c43818fd9d46f89e7deb58354";
    };
  };
in

stdenvNoCC.mkDerivation {
  pname = "zb-installer";
  inherit version;
  src = fetchurl fetchurlArgs.${stdenvNoCC.targetPlatform.system};

  dontConfigure = true;
  dontBuild = true;
  dontFixup = true;
  installPhase = ''
    mkdir -p $out
    cp -a --reflink=auto . $out
  '';

  meta = {
    description = "The zb installer";
    homepage = "https://zb.256lights.llc/";
    downloadPage = "https://github.com/256lights/zb/releases/latest";
    platforms = builtins.attrNames fetchurlArgs;
    license = lib.licenses.mit;
    sourceProvenance = [lib.sourceTypes.binaryNativeCode];
  };
}
