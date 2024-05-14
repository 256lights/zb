# SPDX-FileCopyrightText: 2021-2022 Andrius Štikonas <andrius@stikonas.eu>
#
# SPDX-License-Identifier: GPL-3.0-or-later

src_prepare() {
    default

    rm -- Makefile.in bin/Makefile.in config/Makefile.in doc/Makefile.in lib/Autom4te/Makefile.in lib/Makefile.in lib/autoconf/Makefile.in lib/autoscan/Makefile.in lib/autotest/Makefile.in lib/emacs/Makefile.in lib/m4sugar/Makefile.in man/Makefile.in tests/Makefile.in aclocal.m4 configure
    rm doc/standards.info doc/autoconf.info

    # Do not use pregenerated manpages
    sed -i '/SUBDIRS/s/ man//' Makefile.am

    aclocal-1.6
    cat config/m4.m4 >> aclocal.m4
    autoconf-2.52
    automake-1.6

    # Not supported by autoconf-2.52
    sed -i "s#@abs_top_builddir@#$PWD#" tests/wrappl.in
    sed -i "s#@abs_top_srcdir@#$PWD#" tests/wrappl.in

    # Install autoconf data files into versioned directory
    for file in Makefile.in bin/Makefile.in config/Makefile.in doc/Makefile.in lib/Autom4te/Makefile.in lib/Makefile.in lib/autoconf/Makefile.in lib/autoscan/Makefile.in lib/autotest/Makefile.in lib/emacs/Makefile.in lib/m4sugar/Makefile.in man/Makefile.in tests/Makefile.in; do
        sed -i '/^pkgdatadir/s:$:-@VERSION@:' "$file"
    done
}

src_configure() {
    ./configure --prefix="${PREFIX}" --program-suffix=-2.53
}

src_compile() {
    make "${MAKEJOBS}" MAKEINFO=true DESTDIR="${DESTDIR}"
}

src_install() {
    make install MAKEINFO=true DESTDIR="${DESTDIR}"
}
