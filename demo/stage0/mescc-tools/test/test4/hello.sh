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

set -x
./bin/M1 -f test/test4/MES_defs.M1 -f test/test4/mini-libc-mes.M1 -f test/test4/hello.M1 --little-endian --architecture x86 -o test/test4/hold
./bin/hex2 -f elf_headers/elf32.hex2 -f test/test4/hold --little-endian --architecture x86 --base-address 0x8048000 -o test/results/test4-binary

if [ "$(./bin/get_machine ${GET_MACHINE_FLAGS})" = "x86" ] && [ "$(./bin/get_machine ${GET_MACHINE_OS_FLAGS} --os)" = "Linux" ]
then
	out=$(./test/results/test4-binary 2>&1)
	r=$?
	[ $r = 42 ] || exit 1
	[ "$out" = "Hello, Mescc!" ] || exit 2
fi
exit 0
