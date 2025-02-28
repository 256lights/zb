# SPDX-FileCopyrightText: 2022 Andrius Štikonas <andrius@stikonas.eu>
# SPDX-FileCopyrightText: 2022 fosslinux <fosslinux@aussies.space>
#
# SPDX-License-Identifier: GPL-3.0-or-later

src_prepare() {
    patchShebangs configure

    rm doc/standards.info man/*.1
    AUTOMAKE=automake-1.11 ACLOCAL=aclocal-1.11 autoreconf -f
}

src_configure() {
    ./configure --prefix="${PREFIX}"
}

src_compile() {
    make "${MAKEJOBS}" MAKEINFO=true
}

src_install() {
    make install MAKEINFO=true DESTDIR="${DESTDIR}"

    patchShebangs --update "${DESTDIR}${PREFIX}/bin"/*
}
