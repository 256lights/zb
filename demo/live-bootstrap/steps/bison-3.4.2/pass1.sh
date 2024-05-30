# SPDX-FileCopyrightText: 2021-22 fosslinux <fosslinux@aussies.space>
# SPDX-FileCopyrightText: 2022 Andrius Štikonas <andrius@stikonas.eu>
#
# SPDX-License-Identifier: GPL-3.0-or-later

src_prepare() {
    default

    sed -i -e "/^BISON =/s:.*:BISON = $(command -v bison):" Makefile.am

    # Remove pre-generated flex/bison files
    rm src/parse-gram.c src/parse-gram.h
    rm src/scan-code.c
    rm src/scan-gram.c
    rm src/scan-skel.c

    # Remove pregenerated info files
    rm doc/bison.info

    ../../import-gnulib.sh

    AUTOPOINT=true autoreconf -fi
}

src_configure() {
    ./configure --prefix="${PREFIX}" \
        --libdir="${LIBDIR}" \
        --disable-nls
}

src_compile() {
    make -j1 MAKEINFO=true
}

src_install() {
    make MAKEINFO=true DESTDIR="${DESTDIR}" install
}
