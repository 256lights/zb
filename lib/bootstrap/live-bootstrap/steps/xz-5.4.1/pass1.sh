# SPDX-FileCopyrightText: 2021 Andrius Štikonas <andrius@stikonas.eu>
#
# SPDX-License-Identifier: GPL-3.0-or-later

src_prepare() {
    # Delete translation catalogs
    find . -name "*.gmo" -delete
    rm -rf po4a/man

    autoreconf -f
}

src_configure() {
    ./configure \
        --prefix="${PREFIX}" \
        --disable-shared \
        --disable-nls \
        --build=i386-unknown-linux-musl \
        --libdir="${LIBDIR}"
}
