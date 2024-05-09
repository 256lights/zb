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

set -ex
./bin/blood-elf -f test/test12/hello.M1 --little-endian -o test/test12/footer.M1 || exit 1
./bin/M1 --little-endian --architecture armv7l -f test/test12/hello.M1 -f test/test12/footer.M1 -o test/test12/hello.hex2 || exit 2
./bin/hex2 --little-endian --architecture armv7l --base-address 0x10000 -f elf_headers/elf32-ARM-debug.hex2 -f test/test12/hello.hex2 -o test/results/test12-binary || exit 3

. ./sha256.sh

if [ "$(./bin/get_machine ${GET_MACHINE_FLAGS})" = "armv7l" ] && [ "$(./bin/get_machine ${GET_MACHINE_OS_FLAGS} --os)" = "Linux" ]
then
	./test/results/test12-binary > test/test12/proof
	r=$?
	[ $r = 0 ] || exit 4
	out=$(sha256_check test/test12/proof.answer)
	[ "$out" = "test/test12/proof: OK" ] || exit 5
fi
exit 0
