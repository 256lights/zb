# SPDX-FileCopyrightText: 2023 fosslinux <fosslinux@aussies.space>
#
# SPDX-License-Identifier: GPL-3.0-or-later

src_prepare() {
    default

    autoreconf -fi
}

src_configure() {
    CFLAGS="-std=gnu99" \
    ./configure --prefix="${PREFIX}" \
        --libdir="${LIBDIR}" \
        --disable-shared
}
