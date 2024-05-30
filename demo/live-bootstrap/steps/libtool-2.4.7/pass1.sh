# SPDX-FileCopyrightText: 2022 Dor Askayo <dor.askayo@gmail.com>
# SPDX-FileCopyrightText: 2021 Andrius Štikonas <andrius@stikonas.eu>
# SPDX-FileCopyrightText: 2022 fosslinux <fosslinux@aussies.space>
#
# SPDX-License-Identifier: GPL-3.0-or-later


src_prepare() {
    default

    rm -f build-aux/ltmain.sh
    rm -f doc/*.info
    rm -f bootstrap

    patchShebangs ../../import-gnulib.sh ../../bootstrap-helper.sh

    ../../import-gnulib.sh

    LIBTOOLIZE=true AUTOPOINT=true ../../bootstrap-helper.sh

    LIBTOOLIZE=true AUTOPOINT=true autoreconf -fi
    LIBTOOLIZE=true AUTOPOINT=true autoreconf -fi libltdl
}

src_configure() {
    ./configure \
        --prefix="${PREFIX}" \
        --libdir="${LIBDIR}" \
        --disable-shared \
        --host=i386-unknown-linux \
        --target=i386-unknown-linux \
        --build=i386-unknown-linux \
        ac_path_EGREP="egrep" \
        ac_path_FGREP="fgrep" \
        ac_path_GREP="grep" \
        ac_path_SED="sed"
}

src_compile() {
    make "${MAKEJOBS}" MAKEINFO=true
}

src_install() {
    make install MAKEINFO=true DESTDIR="${DESTDIR}"

    sed -i -e "s/{EGREP=.*/{EGREP='egrep'}/" \
        -e "s/{FGREP=.*/{FREGP='fgrep'}/" \
        -e "s/{GREP=.*/{GREP='grep'}/" \
        -e "s/{SED=.*/{SED='sed'}/" \
        "${DESTDIR}${PREFIX}/bin/libtool"
    
    patchShebangs --update "${PREFIX}/bin"/*
}
