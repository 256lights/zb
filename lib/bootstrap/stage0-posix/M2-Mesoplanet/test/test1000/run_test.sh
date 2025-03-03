#! /bin/sh
## Copyright (C) 2017 Jeremiah Orians
## Copyright (C) 2020-2021 deesix <deesix@tuta.io>
## This file is part of M2-Planet.
##
## M2-Planet is free software: you can redistribute it and/or modify
## it under the terms of the GNU General Public License as published by
## the Free Software Foundation, either version 3 of the License, or
## (at your option) any later version.
##
## M2-Planet is distributed in the hope that it will be useful,
## but WITHOUT ANY WARRANTY; without even the implied warranty of
## MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
## GNU General Public License for more details.
##
## You should have received a copy of the GNU General Public License
## along with M2-Planet.  If not, see <http://www.gnu.org/licenses/>.

set -ex

BUILDDIR="test/test1000/tmp"
BINDIR="test/test1000/tmp"
ARCH=$(get_machine)
M2LIBC="./M2libc"
TOOLS=" "
BLOOD_FLAG=$(get_machine --blood)
BASE_ADDRESS=$(get_machine --hex2)
ENDIAN_FLAG=$(get_machine --endian)

mkdir -p ${BUILDDIR}
./test/test1000/bootstrap.sh

## TODO FINISH WRITING TEST
