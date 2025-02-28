# SPDX-FileCopyrightText: 2022 Andrius Štikonas <andrius@stikonas.eu>
# SPDX-FileCopyrightText: 2022 fosslinux <fosslinux@aussies.space>
#
# SPDX-License-Identifier: GPL-3.0-or-later

src_prepare() {
    default

    rm doc/amhello-1.0.tar.gz

    # Building doc often causes race conditions, skip it
    sed -i '/doc\/Makefile.inc/d' Makefile.am
    sed -i '/t\/Makefile.inc/d' Makefile.am

    AUTOCONF="autoconf -f" ./bootstrap

    rm doc/automake-history.info doc/automake.info*
}

src_configure() {
    AUTOCONF="autoconf -f" ./configure --prefix="${PREFIX}"
}

src_compile() {
    AUTOCONF="autoconf -f" make -j1 MAKEINFO=true
}

src_install() {
    make install MAKEINFO=true DESTDIR="${DESTDIR}"
}
