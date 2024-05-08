#!/usr/bin/env bash
# Copyright (C) 2020 fosslinux
# This file is part of mescc-tools.
#
# mescc-tools is free software: you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
#
# mescc-tools is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with mescc-tools.  If not, see <http://www.gnu.org/licenses/>.

echo "Starting kaem tests"
LANG=C ../bin/kaem -f "test/test00/kaem.test" >| "test/results/test00-output"
LANG=C ../bin/kaem -f "test/test01/kaem.test" >| "test/results/test01-output"
LANG=C ../bin/kaem -f "test/test02/kaem.test" >| "test/results/test02-output"
LANG=C ../bin/kaem -f "test/test03/kaem.test" >| "test/results/test03-output"
LANG=C ../bin/kaem -f "test/test04/kaem.test" >| "test/results/test04-output" 2>&1
LANG=C ../bin/kaem --non-strict -f "test/test05/kaem.test" >| "test/results/test05-output"
LANG=C ../bin/kaem -f "test/test06/kaem.test" >| "test/results/test06-output" 2>&1
LANG=C ../bin/kaem -f "test/test07/kaem.test" >| "test/results/test07-output"
LANG=C ../bin/kaem -f "test/test08/kaem.test" >| "test/results/test08-output"
LANG=C ../bin/kaem -f "test/test09/kaem.test" >| "test/results/test09-output"
LANG=C ../bin/kaem -f "test/test10/kaem.test" >| "test/results/test10-output"
LANG=C ../bin/kaem -f "test/test11/kaem.test" >| "test/results/test11-output"
LANG=C ../bin/kaem -f "test/test12/kaem.test" >| "test/results/test12-output"
LANG=C ../bin/kaem -f "test/test13/kaem.test" >| "test/results/test13-output"
LANG=C ../bin/kaem -f "test/test14/kaem.test" >| "test/results/test14-output" 2>&1
LANG=C ../bin/kaem -f "test/test15/kaem.test" >| "test/results/test15-output"
LANG=C ../bin/kaem -f "test/test16/kaem.test" >| "test/results/test16-output"
LANG=C ../bin/kaem -f "test/test17/kaem.test" >| "test/results/test17-output"
. ../sha256.sh
sha256_check test/test.answers
echo "kaem tests complete"
