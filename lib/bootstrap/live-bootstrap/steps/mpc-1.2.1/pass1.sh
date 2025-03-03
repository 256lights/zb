# SPDX-FileCopyrightText: 2021 Andrius Štikonas <andrius@stikonas.eu>
#
# SPDX-License-Identifier: GPL-3.0-or-later

src_prepare() {
    default

    find . -name '*.info' -delete
    autoreconf -fi
}

src_configure() {
    ./configure \
        --prefix="${PREFIX}" \
        --libdir="${LIBDIR}" \
        --disable-shared
}

src_compile() {
    make "${MAKEJOBS}" MAKEINFO=true DESTDIR="${DESTDIR}"
}

src_install() {
    make MAKEINFO=true DESTDIR="${DESTDIR}" install
}
