# SPDX-FileCopyrightText: 2021 Andrius Štikonas <andrius@stikonas.eu>
#
# SPDX-License-Identifier: GPL-3.0-or-later

src_prepare() {
    default

    mv lib/textstyle.in.h lib/textstyle.h

    # Update configuration for Nix.
    sed -i -e "/M4/s:/usr/:${m4:?}/:" config.h
    sed -i -e "/PKGDATADIR/s:/usr/:${PREFIX:?}/:" configmake.h

    # Remove pre-generated flex/bison files
    rm src/parse-gram.c src/parse-gram.h
    rm src/scan-code.c
    rm src/scan-gram.c
    rm src/scan-skel.c

    # Simplified bison grammar
    mv parse-gram.y src/

    cp ../../mk/lib.mk lib/Makefile
    cp ../../mk/src.mk src/Makefile
}

src_compile() {
    make -j1 -f Makefile PREFIX="${PREFIX}"
}
