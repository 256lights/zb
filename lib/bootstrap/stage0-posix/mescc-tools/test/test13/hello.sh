#! /bin/sh
## Copyright (C) 2017 Jeremiah Orians
## This file is part of stage0.
##
## stage0 is free software: you can redistribute it and/or modify
## it under the terms of the GNU General Public License as published by
## the Free Software Foundation, either version 3 of the License, or
## (at your option) any later version.
##
## stage0 is distributed in the hope that it will be useful,
## but WITHOUT ANY WARRANTY; without even the implied warranty of
## MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
## GNU General Public License for more details.
##
## You should have received a copy of the GNU General Public License
## along with stage0.  If not, see <http://www.gnu.org/licenses/>.

set -x
./bin/M1 --little-endian --architecture ppc64le -f test/test13/hello.M1 -o test/test13/hello.hex2 || exit 1
./bin/hex2 --little-endian --architecture ppc64le --base-address 0x10000 -f elf_headers/elf64-PPC64LE.hex2 -f test/test13/hello.hex2 -o test/results/test13-binary || exit 2


if [ "$(./bin/get_machine ${GET_MACHINE_FLAGS})" = "ppc64le" ] && [ "$(./bin/get_machine ${GET_MACHINE_OS_FLAGS} --os)" = "Linux" ]
then
	./test/results/test13-binary > test/test13/proof
	[ $? = 42 ] || exit 3
	. ./sha256.sh
	out=$(sha256_check test/test13/proof.answer)
	[ "$out" = "test/test13/proof: OK" ] || exit 4
fi
exit 0
