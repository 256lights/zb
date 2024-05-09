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
./bin/hex2 -f elf_headers/elf32.hex2  -f test/test0/mini-libc-mes.hex2 -f test/test0/hello.hex2 --little-endian --architecture x86 --base-address 0x8048000 -o test/results/test0-binary
if [ "$(./bin/get_machine ${GET_MACHINE_FLAGS})" = "x86" ] && [ "$(./bin/get_machine ${GET_MACHINE_OS_FLAGS} --os)" = "Linux" ]
then
	out=$(./test/results/test0-binary 2>&1)
	r=$?
	[ $r = 42 ] || exit 1
	[ "$out" = "Hello, Mescc!" ] || exit 2
fi
exit 0
