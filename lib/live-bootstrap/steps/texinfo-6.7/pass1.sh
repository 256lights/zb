# SPDX-FileCopyrightText: 2022 Andrius Štikonas <andrius@stikonas.eu>
#
# SPDX-License-Identifier: GPL-3.0-or-later

src_prepare() {
    patchShebangs ../../import-gnulib.sh

    find . -name '*.mo' -delete
    find . -name '*.gmo' -delete

    ../../import-gnulib.sh
    autoreconf -fi

    patchShebangs configure
}

src_configure() {
    ./configure --prefix="${PREFIX}"
}

src_install() {
    default
    patchShebangs --update "${DESTDIR}${PREFIX}/bin"/*
}
