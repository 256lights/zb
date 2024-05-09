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

TMPDIR="test/test1000/tmp-knight-posix"
mkdir -p ${TMPDIR}

# Build the test
./bin/M2-Planet \
	--architecture knight-posix \
	-f M2libc/knight/linux/bootstrap.c \
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

# Macro assemble with libc written in M1-Macro
M1 \
	-f M2libc/knight/knight_defs.M1 \
	-f M2libc/knight/libc-core.M1 \
	-f ${TMPDIR}/cc.M1 \
	--big-endian \
	--architecture knight-posix \
	-o ${TMPDIR}/cc.hex2 \
	|| exit 2

# Resolve all linkages
hex2 \
	-f M2libc/knight/ELF-knight.hex2 \
	-f ${TMPDIR}/cc.hex2 \
	--big-endian \
	--architecture knight-posix \
	--base-address 0x00 \
	-o test/results/test1000-knight-posix-binary \
	|| exit 3

# Ensure binary works if host machine supports test
if [ "$(get_machine ${GET_MACHINE_FLAGS})" = "knight" ] && [ ! -z "${KNIGHT_EMULATION}" ]
then
	# Verify that the resulting file works
	execve_image \
		./test/results/test1000-knight-posix-binary \
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
		>| ${TMPDIR}/image \
		|| exit 4

	vm \
		--POSIX-MODE \
		--rom ${TMPDIR}/image \
		--memory 10M \
		|| exit 5

	. ./sha256.sh
	out=$(sha256_check test/test1000/proof.answer)
	[ "$out" = "test/test1000/proof: OK" ] || exit 6
elif [ "$(get_machine ${GET_MACHINE_FLAGS})" = "knight" ]
then
	# Verify that the resulting file works
	./test/results/test1000-knight-posix-binary \
		--architecture x86 \
		-f M2libc/sys/types.h \
		-f M2libc/stddef.h \
		-f M2libc/x86/Linux/unistd.h \
		-f M2libc/stdlib.c \
		-f M2libc/x86/Linux/fcntl.h \
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
		|| exit 7

	. ./sha256.sh
	out=$(sha256_check test/test1000/proof.answer)
	[ "$out" = "test/test1000/proof: OK" ] || exit 8
fi
exit 0
