# SPDX-FileCopyrightText: 2021 Andrius Štikonas <andrius@stikonas.eu>
# SPDX-FileCopyrightText: 2022 fosslinux <fosslinux@aussies.space>
#
# SPDX-License-Identifier: GPL-3.0-or-later

src_prepare() {
    default

    # Rewrite configuration for build prefixes.
    sed -i "/^startperl=/s:/usr/:${out:?}/:" config.sh
    sed -i "/^perlpath=/s:/usr/:${out:?}/:" config.sh
    sed -i "/^#define PRIVLIB/s:/usr/:${out:?}/:" config.h
    sed -i "/^#define LOC_SED/s:/usr/:${sed:?}/:" config.h
    sed -i "/^#define ARCHLIB/s:/usr/:${out:?}/:" config.h

    # Regenerate bison files
    sed -i '/yydestruct/d' perly.y
    rm -f perly.c perly.h
    bison -d perly.y
    mv perly.tab.c perly.c
    mv perly.tab.h perly.h

    # Regenerate other prebuilt header files
    for file in embed keywords opcode; do
        rm -f ${file}.h
        perl ${file}.pl
    done
    rm -f regnodes.h
    perl regcomp.pl
    rm -f ext/ByteLoader/byterun.h ext/ByteLoader/byterun.c
    perl bytecode.pl
    rm -f warnings.h lib/warnings.pm
    perl warnings.pl

    # Workaround for some linking problems, remove if possible
    sed -i 's/perl_call_method/Perl_call_method/' ext/Data/Dumper/Dumper.xs
    sed -i 's/perl_call_sv/Perl_call_sv/' ext/Data/Dumper/Dumper.xs
    sed -i 's/sv_setptrobj/Perl_sv_setref_iv/' ext/POSIX/POSIX.xs

    # We are using non-standard locations
    sed -i "s#/usr/include/errno.h#${musl:?}/include/bits/errno.h#" ext/Errno/Errno_pm.PL
}

src_compile() {
    make -j1 PREFIX="${PREFIX}"
}
