/* Copyright (C) 2009 Tim Kientzle
 * Copyright (C) 2021 Jeremiah Orians
 * Copyright (C) 2021 Andrius Štikonas
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
 * "mkdir" can be used to create empty directories. It can also create          *
 * required parent directories.                                                 *
 *                                                                              *
 * Usage: mkdir <dir1>/<dir2> <dir3>                                            *
 *                                                                              *
 * These are all highly standard and portable headers.                          *
 ********************************************************************************/
#include <stdio.h>
#include <string.h>

/* This is for mkdir(); this may need to be changed for some platforms. */
#include <sys/stat.h>  /* For mkdir() */
#include <stdlib.h>
#include "M2libc/bootstrappable.h"

#define MAX_STRING 4096

int parents;

/* Create a directory, including parent directories as necessary. */
void create_dir(char *pathname, int mode)
{
	char *p;
	int r;

	/* Strip trailing '/' */
	if(pathname[strlen(pathname) - 1] == '/')
	{
		pathname[strlen(pathname) - 1] = '\0';
	}

	/* Try creating the directory. */
	r = mkdir(pathname, mode);

	if((r != 0) && parents)
	{
		/* On failure, try creating parent directory. */
		p = strrchr(pathname, '/');

		if(p != NULL)
		{
			p[0] = '\0';
			create_dir(pathname, mode);
			p[0] = '/';
			r = mkdir(pathname, mode);
		}
	}

	if((r != 0) && !parents)
	{
		fputs("Could not create directory ", stderr);
		fputs(pathname, stderr);
		fputc('\n', stderr);
		exit(EXIT_FAILURE);
	}
}

int main(int argc, char **argv)
{
	/* This adds some quasi-compatibility with GNU coreutils' mkdir. */
	parents = FALSE;
	int i;
	int mode = 0755;
	char* raw_mode = NULL;

	for(i = 1; argc > i; i = i + 1)
	{
		if(match(argv[i], "-p") || match(argv[i], "--parents"))
		{
			parents = TRUE;
		}
		else if(match(argv[i], "-h") || match(argv[i], "--help"))
		{
			fputs("mescc-tools-extra mkdir supports --parents and --mode 0750 "
			      "but the last argument always must be the directly to make\n", stdout);
			return 0;
		}
		else if(match(argv[i], "-v") || match(argv[i], "--version"))
		{
			fputs("mescc-tools-extra mkdir version 1.3.0\n", stdout);
			return 0;
		}
		else if(match(argv[i], "-m") || match(argv[i], "--mode"))
		{
			raw_mode = calloc(MAX_STRING, sizeof(char));
			require(raw_mode != NULL, "Memory initialization of mode failed\n");
			/* We need to indicate it is octal */
			strcat(raw_mode, "0");
			strcat(raw_mode, argv[i+1]);
			mode = strtoint(raw_mode);
			i = i + 1;
		}
		else create_dir(argv[i], mode);
	}

	return 0;
}
