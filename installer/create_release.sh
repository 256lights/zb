#!/usr/bin/env bash
# Copyright 2025 The zb Authors
# SPDX-License-Identifier: MIT

# Create a binary release tarball for the given system.
# This will include the installer script
# and the bootstrap store objects.

set -euo pipefail

toAbs() {
  cd -- "$1" &> /dev/null && pwd
}

SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-0}"
export SOURCE_DATE_EPOCH

# First argument is target system and the rest are store objects.
if [[ $# -ne 1 ]]; then
  echo "usage: $(basename "$0") TARGET_SYSTEM" >&2
  exit 64
fi
targetSystem="$1"

projectDir="$( toAbs "$( dirname -- "${BASH_SOURCE[0]}" )/.." )"
zbTargetURL="$projectDir/build.lua#zb-$targetSystem"
version="$(zb eval "$zbTargetURL/version")"
if [[ "$version" = '<nil>' ]]; then
  version=''
fi
if [[ -z "$version" ]]; then
  archiveName="zb-dev-${targetSystem}"
else
  archiveName="zb-v${version}-${targetSystem}"
fi

buildTopDir="$(mktemp -d 2>/dev/null || mktemp -d -t "zb-release-")"
trap 'rm -rf "$buildTopDir"' EXIT INT TERM
buildDir="$buildTopDir/$archiveName"
mkdir "$buildDir" "$buildDir/store"
touch "$buildDir/registry.txt"

cpToStore() {
  for i in "$@"; do
    cp -R --preserve=mode --no-dereference -- "$i" "$buildDir/store/$(basename "$i")"
    chmod -R u+w -- "$buildDir/store/$(basename "$i")"
    zb store object info -- "$i" >> "$buildDir/registry.txt"
  done
}

# Copy seeds first, since Linux's sandbox depends on Busybox.
case "$targetSystem" in
  x86_64-unknown-linux)
    cpToStore \
      /opt/zb/store/6ssl5z26zmr9dn2iz4xi17a13ia7qz8y-gcc-4.2.1 \
      /opt/zb/store/hpsxd175dzfmjrg27pvvin3nzv3yi61k-busybox-1.36.1
    ;;
esac

zbStorePath="$(zb build "$zbTargetURL")"
cpToStore "$zbStorePath"

case "$targetSystem" in
  *-windows)
    ;;
  *)
    install -m 755 "$projectDir/installer/install.sh" "$buildDir/install"
    ;;
esac

# Set timestamp to given epoch.
find "$buildDir" -exec touch -h -d "@${SOURCE_DATE_EPOCH}" '{}' \;

currentDir="$(pwd)"
case "$targetSystem" in
  *-windows)
    rm -f "$currentDir/$archiveName.zip"
    ( cd "$buildTopDir" && \
      zip -rX "$currentDir/$archiveName.zip" \
        "$archiveName" )
    ;;
  *)
    ( cd "$buildTopDir" && \
      tar -jcf "$currentDir/$archiveName.tar.bz2" \
        --owner=root:0 \
        --group=root:0 \
        "$archiveName" )
    ;;
esac
