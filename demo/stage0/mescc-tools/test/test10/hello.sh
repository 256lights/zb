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
./bin/M1 --little-endian --architecture armv7l -f test/test10/exit_42.M1 -o test/test10/exit_42.hex2 || exit 1
./bin/hex2 --little-endian --architecture armv7l --base-address 0x10000 -f elf_headers/elf32-ARM.hex2 -f test/test10/exit_42.hex2 -o test/results/test10-binary || exit 2
if [ "$(./bin/get_machine ${GET_MACHINE_FLAGS})" = "armv7l" ] && [ "$(./bin/get_machine ${GET_MACHINE_OS_FLAGS} --os)" = "Linux" ]
then
	./test/results/test10-binary
	r=$?
	[ $r = 42 ] || exit 3
fi
exit 0
