#! /bin/sh
## Copyright (C) 2017 Jeremiah Orians
## This file is part of mescc-tools.
##
## mescc-tools is free software: you can redistribute it and/or modify
## it under the terms of the GNU General Public License as published by
## the Free Software Foundation, either version 3 of the License, or
## (at your option) any later version.
##
## mescc-tools is distributed in the hope that it will be useful,
## but WITHOUT ANY WARRANTY; without even the implied warranty of
## MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
## GNU General Public License for more details.
##
## You should have received a copy of the GNU General Public License
## along with mescc-tools.  If not, see <http://www.gnu.org/licenses/>.

set -ex
./bin/blood-elf -f test/test11/hello.M1 --little-endian -o test/test11/footer.M1 || exit 1
./bin/M1 --little-endian --architecture armv7l -f test/test11/hello.M1 -f test/test11/footer.M1 -o test/test11/hello.hex2 || exit 2
./bin/hex2 --little-endian --architecture armv7l --base-address 0x10000 -f elf_headers/elf32-ARM-debug.hex2 -f test/test11/hello.hex2 -o test/results/test11-binary || exit 3

. ./sha256.sh

if [ "$(./bin/get_machine ${GET_MACHINE_FLAGS})" = "armv7l" ] && [ "$(./bin/get_machine ${GET_MACHINE_OS_FLAGS} --os)" = "Linux" ]
then
	./test/results/test11-binary > test/test11/proof
	r=$?
	[ $r = 0 ] || exit 4
	out=$(sha256_check test/test11/proof.answer)
	[ "$out" = "test/test11/proof: OK" ] || exit 5
fi
exit 0
