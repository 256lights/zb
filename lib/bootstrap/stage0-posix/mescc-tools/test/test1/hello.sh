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
./bin/M1 -f test/test1/hex.M1 --little-endian --architecture amd64 -o test/test1/hold
./bin/hex2 -f elf_headers/elf64.hex2 -f test/test1/hold --little-endian --architecture amd64 --base-address 0x00600000 -o test/results/test1-binary

. ./sha256.sh

if [ "$(./bin/get_machine ${GET_MACHINE_FLAGS})" = "amd64" ] && [ "$(./bin/get_machine ${GET_MACHINE_OS_FLAGS} --os)" = "Linux" ]
then
	./test/results/test1-binary < test/test1/hex0.hex0 > test/test1/proof1
	r=$?
	[ $r = 0 ] || exit 1
	out=$(sha256_check test/test1/proof1.answer)
	[ "$out" = "test/test1/proof1: OK" ] || exit 2
	chmod u+x test/test1/proof1
	./test/test1/proof1 < test/test1/hex1.hex0 > test/test1/proof2
	r=$?
	[ $r = 0 ] || exit 3
	out=$(sha256_check test/test1/proof2.answer)
	[ "$out" = "test/test1/proof2: OK" ] || exit 4
fi
exit 0
