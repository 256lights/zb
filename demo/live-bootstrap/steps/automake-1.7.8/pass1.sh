# SPDX-FileCopyrightText: 2022 Andrius Štikonas <andrius@stikonas.eu>
#
# SPDX-License-Identifier: GPL-3.0-or-later

src_prepare() {
    rm -- configure Makefile.in lib/Automake/Makefile.in lib/Makefile.in lib/am/Makefile.in m4/Makefile.in tests/Makefile.in aclocal.m4 automake.info*

    AUTOMAKE=automake-1.7 ACLOCAL=aclocal-1.7 AUTOCONF=autoconf-2.55 autoreconf-2.55
}

src_configure() {
    AUTOCONF=autoconf-2.55 ./configure --prefix="${PREFIX}"
}

src_compile() {
    make "${MAKEJOBS}" MAKEINFO=true
}

src_install() {
    make install MAKEINFO=true DESTDIR="${DESTDIR}"
}
