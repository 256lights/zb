# Copyright 2025 The zb Authors
# SPDX-License-Identifier: MIT

{ stdenvNoCC
, fetchurl
, lib
}:

let
  version = "0.1.0-rc2";

  fetchurlArgs = {
    "x86_64-linux" = {
      url = "https://github.com/256lights/zb/releases/download/v${version}/zb-v${version}-x86_64-unknown-linux.tar.bz2";
      hash = "sha256:c05bd85ab3dceeddddcd7eef45d5ee8412d62797a15769fc03f0b1396857967d";
    };

    "aarch64-darwin" = {
      url = "https://github.com/256lights/zb/releases/download/v${version}/zb-v${version}-aarch64-apple-macos.tar.bz2";
      hash = "sha256:7a351d46c4302adacb52e9880d0009183d91e04716aab1e66af1eba60b8d09e4";
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
