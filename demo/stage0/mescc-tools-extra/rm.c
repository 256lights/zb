/* Copyright (C) 2021 Jeremiah Orians
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
 * "rm" can be used to delete files. It can also delete                         *
 * parent directories.                                                          *
 *                                                                              *
 * Usage: rm <dir1>/<file1> <file2>                                             *
 *                                                                              *
 * These are all highly standard and portable headers.                          *
 ********************************************************************************/
#include <stdio.h>
#include <string.h>

/* This is for unlink() ; this may need to be changed for some platforms. */
#include <unistd.h>  /* For unlink() */
#include <stdlib.h>
#include "M2libc/bootstrappable.h"

void delete_dir(char* name)
{
	int r = unlink(name);
	if(0 != r)
	{
		fputs("unable to delete file: ", stderr);
		fputs(name, stderr);
		fputs(" !!!\n", stderr);
	}
}

int main(int argc, char **argv)
{
	int i;
	for(i = 1; argc > i; i = i + 1)
	{
		delete_dir(argv[i]);
	}

	return 0;
}
