#! /bin/sh
## Copyright (C) 2017 Jeremiah Orians
## Copyright (C) 2020-2021 deesix <deesix@tuta.io>
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

ARCH="$1"
. test/env.inc.sh
TMPDIR="test/test0027/tmp-${ARCH}"

mkdir -p ${TMPDIR}

# Build the test
bin/M2-Planet \
	--architecture ${ARCH} \
	--debug \
	-f test/test0027/dereference.c \
	-o ${TMPDIR}/dereference.M1 \
	|| exit 1

# Build debug footer
blood-elf \
	${BLOOD_ELF_WORD_SIZE_FLAG} \
	-f ${TMPDIR}/dereference.M1 \
	${ENDIANNESS_FLAG} \
	--entry _start \
	-o ${TMPDIR}/dereference-footer.M1 \
	|| exit 2

# Macro assemble with libc written in M1-Macro
M1 \
	-f M2libc/${ARCH}/${ARCH}_defs.M1 \
	-f M2libc/${ARCH}/libc-core.M1 \
	-f ${TMPDIR}/dereference.M1 \
	-f ${TMPDIR}/dereference-footer.M1 \
	${ENDIANNESS_FLAG} \
	--architecture ${ARCH} \
	-o ${TMPDIR}/dereference.hex2 \
	|| exit 2

# Resolve all linkages
hex2 \
	-f M2libc/${ARCH}/ELF-${ARCH}-debug.hex2 \
	-f ${TMPDIR}/dereference.hex2 \
	${ENDIANNESS_FLAG} \
	--architecture ${ARCH} \
	--base-address ${BASE_ADDRESS} \
	-o test/results/test0027-${ARCH}-binary \
	|| exit 3

# Ensure binary works if host machine supports test
if [ "$(get_machine ${GET_MACHINE_FLAGS})" = "${ARCH}" ]
then
	. ./sha256.sh
	# Verify that the resulting file works
	./test/results/test0027-${ARCH}-binary || exit 4
fi
exit 0
