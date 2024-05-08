#! /usr/bin/env bash
# Copyright © 2017 Jan Nieuwenhuizen <janneke@gnu.org>
# Copyright © 2017 Jeremiah Orians
# Copyright (C) 2019 ng0 <ng0@n0.is>
#
# This file is part of mescc-tools
#
# mescc-tools is free software; you can redistribute it and/or modify it
# under the terms of the GNU General Public License as published by
# the Free Software Foundation; either version 3 of the License, or (at
# your option) any later version.
#
# mescc-tools is distributed in the hope that it will be useful, but
# WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with mescc-tools.  If not, see <http://www.gnu.org/licenses/>.

set -eux
[ -e bin ] || mkdir -p bin
[ -f bin/M1 ] || exit 1
[ -f bin/hex2 ] || exit 2
[ -f bin/blood-elf ] || exit 3
#[ -f bin/kaem ] || exit 4
[ -f bin/get_machine ] || exit 5
[ -f bin/exec_enable ] || exit 6
[ -e test/results ] || mkdir -p test/results
./test/test0/hello.sh
./test/test1/hello.sh
./test/test2/hello.sh
./test/test3/hello.sh
./test/test4/hello.sh
./test/test5/hello.sh
./test/test6/hello.sh
./test/test7/hello.sh
./test/test8/hello.sh
./test/test9/hello.sh
./test/test10/hello.sh
./test/test11/hello.sh
. sha256.sh
sha256_check test/test.answers
