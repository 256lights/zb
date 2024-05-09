/* Copyright (C) 2020 fosslinux
 * This file is part of mescc-tools
 *
 * mescc-tools is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * mescc-tools is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with mescc-tools.  If not, see <http://www.gnu.org/licenses/>.
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <fcntl.h>
#include <sys/stat.h>
#include "M2libc/bootstrappable.h"

/* Define all of the constants */
#define MAX_STRING 4096
#define MAX_ARRAY 256

struct files
{
	char* name;
	struct files* next;
};

/* Globals */
int verbose;

/* PROCESSING FUNCTIONS */
int main(int argc, char** argv)
{
	/* Initialize variables */
	char* mode = NULL;
	struct files* f = NULL;
	struct files* n;
	int ok;

	/* Set defaults */
	verbose = FALSE;

	int i = 1;
	/* Loop arguments */
	while(i <= argc)
	{
		if(NULL == argv[i])
		{ /* Ignore and continue */
			i = i + 1;
		}
		else if(match(argv[i], "-h") || match(argv[i], "--help"))
		{
			fputs("Usage: ", stdout);
			fputs(argv[0], stdout);
			fputs(" [-h | --help] [-V | --version] [-v | --verbose]\n", stdout);
			exit(EXIT_SUCCESS);
		}
		else if(match(argv[i], "-V") || match(argv[i], "--version"))
		{ /* Output version */
			fputs("chmod version 1.3.0\n", stdout);
			exit(EXIT_SUCCESS);
		}
		else if(match(argv[i], "-v") || match(argv[i], "--verbose"))
		{
			verbose = TRUE;
			i = i + 1;
		}
		else
		{ /* It must be the file or the mode */
			if(mode == NULL)
			{ /* Mode always comes first */
				mode = calloc(MAX_STRING, sizeof(char));
				require(mode != NULL, "Memory initialization of mode failed\n");
				/* We need to indicate it is octal */
				strcat(mode, "0");
				strcat(mode, argv[i]);
			}
			else
			{ /* It's a file, as the mode is already done */
				n = calloc(1, sizeof(struct files));
				require(n != NULL, "Memory initialization of files failed\n");
				n->next = f;
				f = n;
				f->name = argv[i];
			}
			i = i + 1;
		}
	}

	/* Ensure the two values have values */
	require(mode != NULL, "Provide a mode\n");
	require(f != NULL, "Provide a file\n");

	/* Convert the mode str into octal */
	int omode = strtoint(mode);

	/* Loop over files to be operated on */
	while(NULL != f)
	{
		/* Make sure the file can be opened */
		ok = access(f->name, 0);
		if(ok != 0)
		{
			fputs("The file: ", stderr);
			fputs(f->name, stderr);
			fputs(" does not exist\n", stderr);
			exit(EXIT_FAILURE);
		}

		/* Verbose message */
		if(verbose)
		{
			fputs("mode of '", stdout);
			fputs(f->name, stdout);
			fputs("' changed to ", stdout);
			fputs(mode, stdout);
			fputs("\n", stdout);
		}

		/* Perform the chmod */
		chmod(f->name, omode);
		f = f->next;
	}
}
