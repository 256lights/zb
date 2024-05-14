# SPDX-FileCopyrightText: 2021-2022 Andrius Štikonas <andrius@stikonas.eu>
#
# SPDX-License-Identifier: GPL-3.0-or-later

src_prepare() {
    rm bin/autoconf.in
    rm -- Makefile.in bin/Makefile.in config/Makefile.in doc/Makefile.in lib/Autom4te/Makefile.in lib/Makefile.in lib/autoconf/Makefile.in lib/autoscan/Makefile.in lib/autotest/Makefile.in lib/emacs/Makefile.in lib/m4sugar/Makefile.in man/Makefile.in tests/Makefile.in aclocal.m4 configure
    rm doc/standards.info doc/autoconf.info

    # Do not use pregenerated manpages
    sed -i '/SUBDIRS/s/ man//' Makefile.am

    AUTOMAKE=automake-1.7 ACLOCAL=aclocal-1.7 AUTOCONF=autoconf-2.54 autoreconf-2.54

    # Install autoconf data files into versioned directory
    for file in Makefile.in bin/Makefile.in config/Makefile.in doc/Makefile.in lib/Autom4te/Makefile.in lib/Makefile.in lib/autoconf/Makefile.in lib/autoscan/Makefile.in lib/autotest/Makefile.in lib/emacs/Makefile.in lib/m4sugar/Makefile.in man/Makefile.in tests/Makefile.in; do
        sed -i '/^pkgdatadir/s:$:-@VERSION@:' "$file"
    done
}

src_configure() {
    ./configure --prefix="${PREFIX}" --program-suffix=-2.55
}

src_compile() {
    # Workaround for racy make dependencies
    make -C bin autom4te
    make -C lib

    make "${MAKEJOBS}" MAKEINFO=true
}

src_install() {
    make install MAKEINFO=true DESTDIR="${DESTDIR}"
}
