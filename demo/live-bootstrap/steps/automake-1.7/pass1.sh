# SPDX-FileCopyrightText: 2022 Andrius Štikonas <andrius@stikonas.eu>
#
# SPDX-License-Identifier: GPL-3.0-or-later

src_prepare() {
    rm -- configure Makefile.in lib/Automake/Makefile.in lib/Makefile.in lib/am/Makefile.in m4/Makefile.in tests/Makefile.in aclocal.m4 automake.info*
    sed -i -e 's/2.54/2.53/' -e '/AC_PROG_EGREP/d' -e '/AC_PROG_FGREP/d' configure.in
    aclocal-1.6
    autoconf-2.53
    automake-1.6
}

src_configure() {
    ./configure --prefix="${PREFIX}"
}

src_compile() {
    make "${MAKEJOBS}" MAKEINFO=true
}

src_install() {
    make install MAKEINFO=true DESTDIR="${DESTDIR}"
    rm "${DESTDIR}${PREFIX}/bin/automake" "${DESTDIR}${PREFIX}/bin/aclocal"
}
