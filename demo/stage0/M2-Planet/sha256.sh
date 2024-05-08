#! /usr/bin/env sh
# Copyright (C) 2019 ng0 <ng0@n0.is>
# Copyright (C) 2019 Jeremiah Orians
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

set -ex
# It's bad to rely on the uname here, but it's a start.
# What this does is to consider the major implementations of
# sha256sum tools and their differences and call them
# accordingly.
sha256_check()
{
	if [ -e "$(which sha256sum)" ]; then
		LANG=C sha256sum -c "$1"
	elif [ "$(get_machine --OS)" = "FreeBSD" ]; then
		LANG=C awk '
		BEGIN { status = 0 }
		{
			rc=system(">/dev/null sha256 -q -c "$1" "$2);
			if (rc == 0) print($2": OK")
			else {
				print($2": NOT OK");
				status=rc
			}
		}
		END { exit status}
		' "$1"
	elif [ -e "$(which sum)" ]; then
		LANG=C sum -a SHA256 -n -c "$1"
	elif [ -e "$(which sha256)" ]; then
		LANG=C sha256 -r -c "$1"
	else
		echo "Unsupported sha256 tool, please send a patch to support it"
		exit 77
	fi
}
