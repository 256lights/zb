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
./bin/M1 -f test/test2/hex.M1 --little-endian --architecture x86 -o test/test2/hold
./bin/hex2 -f elf_headers/elf32.hex2 -f test/test2/hold --little-endian --architecture x86 --base-address 0x8048000 -o test/results/test2-binary

. ./sha256.sh

if [ "$(./bin/get_machine ${GET_MACHINE_FLAGS})" = "x86" ] && [ "$(./bin/get_machine ${GET_MACHINE_OS_FLAGS} --os)" = "Linux" ]
then
	./test/results/test2-binary < test/test2/hex0.hex0 > test/test2/proof
	r=$?
	[ $r = 0 ] || exit 1
	out=$(sha256_check test/test2/proof.answer)
	[ "$out" = "test/test2/proof: OK" ] || exit 2
fi
exit 0
