/* Copyright (C) 2021 Andrius Štikonas
 * This file is part of mescc-tools-extra
 *
 * mescc-tools-extra is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * mescc-tools-extra is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with mescc-tools-extra.  If not, see <http://www.gnu.org/licenses/>.
 */

/********************************************************************************
 * "match" can be used to compare strings. It is useful to write conditional    *
 * code in kaem.                                                                *
 *                                                                              *
 * Usage: match string1 string2                                                 *
 * Returns: 0 if strings match                                                  *
 ********************************************************************************/

#include <stdio.h>
#include <string.h>

#include "M2libc/bootstrappable.h"

int main(int argc, char **argv)
{
	/* ensure correct number of arguments */
	if(argc != 3)
	{
		fputs("match needs exactly 2 arguments.\n", stderr);
		return 2;
	}

	/* deal with badly behaving shells calling */
	if(NULL == argv[1])
	{
		fputs("You passed a null string\n", stderr);
		return 3;
	}
	if(NULL == argv[2])
	{
		fputs("You passed a null string\n", stderr);
		return 3;
	}

	return !match(argv[1], argv[2]);
}
