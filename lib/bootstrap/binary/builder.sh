#!/usr/bin/env bash
# Copyright 2025 The zb Authors
# SPDX-License-Identifier: MIT

set -e
mkdir "${TEMPDIR}/src"
cd "${TEMPDIR}/src"
case "$src" in
  *.tar.bz2)
    tar -xf "$src" --bzip2
    ;;
  *.tar.gz)
    tar -xf "$src" --gzip
    ;;
  *.tar.xz)
    tar -xf "$src" --xz
    ;;
  *)
    echo "unhandled source $src"
    exit 1
    ;;
esac
if [[ -n "$sourceRoot" ]]; then
  cd "$sourceRoot"
else
  cd *
fi
if [[ -n "$configGuess" ]]; then
  cp "$configGuess" config.guess
  chmod +x config.guess
fi
if [[ -n "$configSub" ]]; then
  cp "$configSub" config.sub
  chmod +x config.sub
fi
if [[ -n "$patchPhase" ]]; then
  eval "$patchPhase"
elif [[ -n "$patches" ]]; then
  for i in $patches; do
    patch ${patchFlags:--p1} < "$i"
  done
fi
./configure --prefix=$out $configureFlags
make -j${ZB_BUILD_CORES:-1} $makeFlags $buildFlags
make install $makeFlags $installFlags
