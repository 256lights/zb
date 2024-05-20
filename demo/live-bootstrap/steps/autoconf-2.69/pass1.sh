# SPDX-FileCopyrightText: 2022 Andrius Štikonas <andrius@stikonas.eu>
# SPDX-FileCopyrightText: 2022 fosslinux <fosslinux@aussies.space>
#
# SPDX-License-Identifier: GPL-3.0-or-later

src_prepare() {
    patchShebangs configure

    rm doc/standards.info man/*.1
    AUTOMAKE=automake-1.11 ACLOCAL=aclocal-1.11 autoreconf-2.64 -f

    # Install autoconf data files into versioned directory
    for file in Makefile.in bin/Makefile.in doc/Makefile.in lib/Autom4te/Makefile.in lib/Makefile.in lib/autoconf/Makefile.in lib/autoscan/Makefile.in lib/autotest/Makefile.in lib/emacs/Makefile.in lib/m4sugar/Makefile.in man/Makefile.in tests/Makefile.in; do
        sed -i '/^pkgdatadir/s:$:-@VERSION@:' "$file"
    done
}

src_configure() {
    ./configure --prefix="${PREFIX}" --program-suffix=-2.69
}

src_compile() {
    make "${MAKEJOBS}" MAKEINFO=true
}

src_install() {
    make install MAKEINFO=true DESTDIR="${DESTDIR}"
}
