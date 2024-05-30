# SPDX-FileCopyrightText: 2022 Andrius Štikonas <andrius@stikonas.eu>
# SPDX-FileCopyrightText: 2022 fosslinux <fosslinux@aussies.space>
#
# SPDX-License-Identifier: GPL-3.0-or-later

src_prepare() {
    default

    patchShebangs bootstrap configure

    rm -f doc/amhello-1.0.tar.gz doc/automake.info* doc/aclocal-1.11.1 doc/automake-1.11.1

    # Building doc often causes race conditions, skip it
    awk '/SUBDIRS/{sub("doc ", "", $0)} {print}' Makefile.am > Makefile.am.tmp
    mv Makefile.am.tmp Makefile.am

    ./bootstrap
}

src_configure() {
    ./configure --prefix="${PREFIX}"
}

src_compile() {
    make "${MAKEJOBS}" MAKEINFO=true
}

src_install() {
    make install MAKEINFO=true DESTDIR="${DESTDIR}"
}
