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
./bin/M1 -f test/test3/defs -f test/test3/lisp.s --big-endian --architecture knight-native -o test/test3/hold
./bin/hex2 -f test/test3/hold --big-endian --architecture knight-native --base-address 0 -o test/results/test3-binary

. ./sha256.sh

if [ "$(./bin/get_machine ${GET_MACHINE_FLAGS})" = "knight*" ] && [ "$(./bin/get_machine ${GET_MACHINE_OS_FLAGS} --os)" = "Linux" ]
then
	./test/results/test3-binary < test/test3/example.s >| test/test3/proof
	r=$?
	[ $r = 0 ] || exit 1
	out=$(sha256_check test/test3/proof.answer)
	[ "$out" = "test/test3/proof: OK" ] || exit 2
fi
exit 0
