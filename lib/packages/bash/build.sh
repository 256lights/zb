#!/bin/sh
# Copyright 2025 The zb Authors
# SPDX-License-Identifier: MIT

set -eu

tar -zxf "${src?}"
cd "${name?}"
for i in ${patches:-}; do
  patch -p1 < "$i"
done
# Make build deterministic.
printf '%s\n%s\n' \
  '#!/bin/sh' \
  'echo "#define PIPESIZE 65536"' >builtins/psize.sh

./configure \
  --prefix="${out?}" \
  --enable-static-link \
  --without-bash-malloc \
  --disable-nls
make "-j${ZB_BUILD_CORES:-1}"
make install
