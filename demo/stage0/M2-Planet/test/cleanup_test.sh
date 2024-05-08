#! /bin/sh
## Copyright (C) 2021 deesix <deesix@tuta.io>
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

for ARCH in aarch64 amd64 armv7l knight-native knight-posix x86 riscv32 riscv64; do
	rm -rf "test/test$1/tmp-$ARCH"
done

# Not all, but most tests generate a 'proof' file.
rm -f "test/test$1/proof"

# Test 0106 generates these two files when the host is x86.
if [ "0106" = "$1" ] ; then
	rm -f "test/test$1/cc1" "test/test$1/cc2"
fi

exit 0
