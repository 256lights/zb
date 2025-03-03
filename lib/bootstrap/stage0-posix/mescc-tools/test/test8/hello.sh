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
{
	./bin/M1 -f test/test8/sample.M1 --little-endian --architecture amd64
	./bin/M1 -f test/test8/sample.M1 --big-endian --architecture amd64
	./bin/M1 -f test/test8/sample.M1 --little-endian --architecture x86
	./bin/M1 -f test/test8/sample.M1 --big-endian --architecture x86
} >| test/test8/proof

. ./sha256.sh

out=$(sha256_check test/test8/proof.answer)
[ "$out" = "test/test8/proof: OK" ] || exit 2

./bin/hex2 -f test/test8/proof --big-endian --architecture knight-native --base-address 0 -o test/results/test8-binary

exit 0
