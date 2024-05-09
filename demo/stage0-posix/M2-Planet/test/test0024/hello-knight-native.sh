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

set -x

TMPDIR="test/test0024/tmp-knight-native"
mkdir -p ${TMPDIR}

# Build the test
bin/M2-Planet \
	--architecture knight-native \
	-f test/test0024/return.c \
	-o ${TMPDIR}/return.M1 \
	|| exit 1

# Macro assemble with libc written in M1-Macro
M1 \
	-f M2libc/knight/knight-native_defs.M1 \
	-f M2libc/knight/libc-native.M1 \
	-f ${TMPDIR}/return.M1 \
	--big-endian \
	--architecture knight-native \
	-o ${TMPDIR}/return.hex2 \
	|| exit 2

# Resolve all linkages
hex2 \
	-f ${TMPDIR}/return.hex2 \
	--big-endian \
	--architecture knight-native \
	--base-address 0x0 \
	-o test/results/test0024-knight-native-binary \
	|| exit 3

# Ensure binary works if host machine supports test
if [ "$(get_machine ${GET_MACHINE_FLAGS})" = "knight-native" ]
then
	# Verify that the compiled program returns the correct result
	vm --rom ./test/results/test0024-knight-native-binary
	[ 0 = $? ] || exit 3
fi
exit 0
