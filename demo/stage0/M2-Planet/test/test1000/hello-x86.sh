#! /bin/sh
## Copyright (C) 2017 Jeremiah Orians
## Copyright (C) 2021 deesix <deesix@tuta.io>
## This file is part of M2-Planet.
##
## M2-Planet is free software: you can redistribute it and/or modify
## it under the terms of the GNU General Public License as published by
## the Free Software Foundation, either version 3 of the License, or
## (at your option) any later version.
##
## M2-Planet is distributed in the hope that it will be useful,
## but WITHOUT ANY WARRANTY; without even the implied warranty of
## MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
## GNU General Public License for more details.
##
## You should have received a copy of the GNU General Public License
## along with M2-Planet.  If not, see <http://www.gnu.org/licenses/>.

set -ex

TMPDIR="test/test1000/tmp-x86"
mkdir -p ${TMPDIR}

# Build the test
./bin/M2-Planet \
	--architecture x86 \
	-f M2libc/x86/linux/bootstrap.c \
	-f cc.h \
	-f M2libc/bootstrappable.c \
	-f cc_globals.c \
	-f cc_reader.c \
	-f cc_strings.c \
	-f cc_types.c \
	-f cc_core.c \
	-f cc_macro.c \
	-f cc.c \
	--debug \
	--bootstrap-mode \
	-o ${TMPDIR}/cc.M1 \
	|| exit 1

# Build debug footer
blood-elf \
	-f ${TMPDIR}/cc.M1 \
	--little-endian \
	--entry _start \
	-o ${TMPDIR}/cc-footer.M1 \
	|| exit 2

# Macro assemble with libc written in M1-Macro
M1 \
	-f M2libc/x86/x86_defs.M1 \
	-f M2libc/x86/libc-core.M1 \
	-f ${TMPDIR}/cc.M1 \
	-f ${TMPDIR}/cc-footer.M1 \
	--little-endian \
	--architecture x86 \
	-o ${TMPDIR}/cc.hex2 \
	|| exit 3

# Resolve all linkages
hex2 \
	-f M2libc/x86/ELF-x86-debug.hex2 \
	-f ${TMPDIR}/cc.hex2 \
	--little-endian \
	--architecture x86 \
	--base-address 0x8048000 \
	-o test/results/test1000-x86-binary \
	|| exit 4

# Ensure binary works if host machine supports test
if [ "$(get_machine ${GET_MACHINE_FLAGS})" = "x86" ]
then
	# Verify that the resulting file works
	./test/results/test1000-x86-binary \
		--architecture x86 \
		-f M2libc/sys/types.h \
		-f M2libc/stddef.h \
		-f M2libc/x86/linux/unistd.c \
		-f M2libc/x86/linux/fcntl.c \
		-f M2libc/fcntl.c \
		-f M2libc/stdlib.c \
		-f M2libc/stdio.h \
		-f M2libc/stdio.c \
		-f cc.h \
		-f M2libc/bootstrappable.c \
		-f cc_globals.c \
		-f cc_reader.c \
		-f cc_strings.c \
		-f cc_types.c \
		-f cc_core.c \
		-f cc_macro.c \
		-f cc.c \
		-o test/test1000/proof \
		|| exit 5

	. ./sha256.sh
	out=$(sha256_check test/test1000/proof.answer)
	[ "$out" = "test/test1000/proof: OK" ] || exit 6
	[ ! -e bin/M2-Planet ] && mv test/results/test1000-x86-binary bin/M2-Planet
fi
exit 0
