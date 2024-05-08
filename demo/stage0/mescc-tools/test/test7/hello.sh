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
./bin/M1 -f test/test7/hex1_amd64.M1 --little-endian --architecture amd64 -o test/test7/hold
./bin/hex2 -f elf_headers/elf64.hex2 -f test/test7/hold --little-endian --architecture amd64 --base-address 0x00600000 -o test/results/test7-binary

. ./sha256.sh

if [ "$(./bin/get_machine ${GET_MACHINE_FLAGS})" = "amd64" ] && [ "$(./bin/get_machine ${GET_MACHINE_OS_FLAGS} --os)" = "Linux" ]
then
	./test/results/test7-binary test/test7/hex1.hex1 > test/test7/proof
	r=$?
	[ $r = 0 ] || exit 1
	out=$(sha256_check test/test7/proof.answer)
	[ "$out" = "test/test7/proof: OK" ] || exit 2
fi
exit 0
