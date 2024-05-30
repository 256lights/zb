# SPDX-FileCopyrightText: 2021-22 fosslinux <fosslinux@aussies.space>
#
# SPDX-License-Identifier: GPL-3.0-or-later

src_prepare() {
    default

    rm doc/make.info
    touch doc/make.info

    # We don't have autopoint from gettext yet
    AUTOPOINT=true AUTOMAKE=automake-1.10 ACLOCAL=aclocal-1.10 autoreconf -fi
}

src_configure() {
    ./configure \
        --prefix="${PREFIX}" \
        --build=i386-unknown-linux-gnu \
        --disable-nls
}

src_compile() {
    make "${MAKEJOBS}" MAKEINFO="true"
}

src_install() {
    make install MAKEINFO="true" DESTDIR="${DESTDIR}"
}
