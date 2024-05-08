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

TMPDIR="test/test0102/tmp-knight-posix"
mkdir -p ${TMPDIR}

# Build the test
./bin/M2-Planet \
	--architecture knight-posix \
	-f M2libc/sys/types.h \
	-f M2libc/stddef.h \
	-f M2libc/knight/linux/unistd.c \
	-f M2libc/knight/linux/fcntl.c \
	-f M2libc/fcntl.c \
	-f M2libc/stdlib.c \
	-f M2libc/stdio.h \
	-f M2libc/stdio.c \
	-f M2libc/bootstrappable.c \
	-f test/test0102/M1-macro.c \
	-o ${TMPDIR}/M1-macro.M1 \
	|| exit 1

# Macro assemble with libc written in M1-Macro
M1 \
	-f M2libc/knight/knight_defs.M1 \
	-f M2libc/knight/libc-full.M1 \
	-f ${TMPDIR}/M1-macro.M1 \
	--big-endian \
	--architecture knight-posix \
	-o ${TMPDIR}/M1-macro.hex2 \
	|| exit 3

# Resolve all linkages
hex2 \
	-f M2libc/knight/ELF-knight.hex2 \
	-f ${TMPDIR}/M1-macro.hex2 \
	--big-endian \
	--architecture knight-posix \
	--base-address 0x00 \
	-o test/results/test0102-knight-posix-binary \
	|| exit 4

# Ensure binary works if host machine supports test
if [ "$(get_machine ${GET_MACHINE_FLAGS})" = "knight" ] && [ ! -z "${KNIGHT_EMULATION}" ]
then
	# Verify that the compiled program returns the correct result
	execve_image \
		./test/results/test0102-knight-posix-binary \
		--version \
		>| ${TMPDIR}/image || exit 5
	out=$(vm --POSIX-MODE --rom ${TMPDIR}/image --memory 2M)
	[ 0 = $? ] || exit 6
	[ "$out" = "M1 1.0.0" ] || exit 7

	# Verify that the resulting file works
	execve_image \
		./test/results/test0102-knight-posix-binary \
		-f M2libc/x86/x86_defs.M1 \
		-f M2libc/x86/libc-core.M1 \
		-f test/test0100/test.M1 \
		--little-endian \
		--architecture x86 \
		-o test/test0102/proof \
		>| ${TMPDIR}/image || exit 8
	vm --POSIX-MODE --rom ${TMPDIR}/image --memory 2M || exit 9

	. ./sha256.sh
	out=$(sha256_check test/test0102/proof.answer)
	[ "$out" = "test/test0102/proof: OK" ] || exit 8

elif [ "$(get_machine ${GET_MACHINE_FLAGS})" = "knight" ]
then
	# Verify that the compiled program returns the correct result
	out=$(./test/results/test0102-knight-posix-binary --version 2>&1 )
	[ 0 = $? ] || exit 6
	[ "$out" = "M1 1.0.0" ] || exit 7

	# Verify that the resulting file works
	./test/results/test0102-knight-posix-binary \
		-f M2libc/x86/x86_defs.M1 \
		-f M2libc/x86/libc-core.M1 \
		-f test/test0100/test.M1 \
		--little-endian \
		--architecture x86 \
		-o test/test0102/proof \
		|| exit 9

	. ./sha256.sh
	out=$(sha256_check test/test0102/proof.answer)
	[ "$out" = "test/test0102/proof: OK" ] || exit 8
fi
exit 0
