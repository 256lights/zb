#!/bin/sh
# Copyright 2025 The zb Authors
# SPDX-License-Identifier: MIT

set -e

tar -jxf "${src?}"
cd "${name?}"
for i in ${patches:-}; do
  patch -p1 < "$i"
done
touch config.h

CC="${CC:-gcc}"
CFLAGS="${CFLAGS:-\
  -DHAVE_STRING_H \
  -DHAVE_INTTYPES_H \
  -DHAVE_SA_RESTART \
  -DHAVE_STDINT_H \
  -DHAVE_FCNTL_H \
  -DHAVE_STDARG_H \
  -DHAVE_DIRENT_H \
  -DFILE_TIMESTAMP_HI_RES=0 \
  -DHAVE_DUP2 \
  -DHAVE_GETCWD \
  -DHAVE_MKTEMP \
  -DHAVE_STRNCASECMP \
  -DHAVE_STRCHR \
  -DHAVE_STRDUP \
  -DHAVE_STRERROR \
  -DHAVE_VPRINTF \
  -DSTDC_HEADERS \
  -DHAVE_ANSI_COMPILER}"
LDFLAGS="${LDFLAGS:-}"

compile() {
  for compilefile; do true; done
  echo "CC $compilefile"
  # shellcheck disable=SC2086
  "$CC" -c $CFLAGS -I. -Iglob "$@"
}

compile getopt.c
compile getopt1.c
compile ar.c
compile arscan.c
compile commands.c
compile -DSCCS_GET=\"/nullop\" default.c
compile dir.c
compile expand.c
compile file.c
compile -Dvfork=fork function.c
compile implicit.c
compile -Dvfork=fork job.c
compile -DLOCALEDIR=\"/fake-locale\" -DPACKAGE=\"fake-make\" main.c
compile misc.c
compile "-DINCLUDEDIR=\"${out?}/include\"" read.c
compile "-DLIBDIR=\"${out?}/lib\"" remake.c
compile rule.c
compile signame.c
compile strcache.c
compile variable.c
compile -DVERSION="\"${version:-3.82}\"" version.c
compile vpath.c
compile hash.c
compile remote-stub.c
compile getloadavg.c
compile glob/fnmatch.c
compile glob/glob.c

echo "LINK make"
# shellcheck disable=SC2086
"$CC" $LDFLAGS -o make getopt.o getopt1.o ar.o arscan.o commands.o default.o dir.o expand.o file.o function.o implicit.o job.o main.o misc.o read.o remake.o rule.o signame.o strcache.o variable.o version.o vpath.o hash.o remote-stub.o getloadavg.o fnmatch.o glob.o

install -D -m 755 make "${out?}/bin/make"
