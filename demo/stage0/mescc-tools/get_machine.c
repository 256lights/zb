/* -*- c-file-style: "linux";indent-tabs-mode:t -*- */
/* Copyright (C) 2017 Jeremiah Orians
 * This file is part of mescc-tools.
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
#include <sys/utsname.h>
int match(char* a, char* b);

#define TRUE 1
//CONSTANT TRUE 1
#define FALSE 0
//CONSTANT FALSE 0

/* Standard C main program */
int main(int argc, char **argv)
{
	int exact = FALSE;
	int override = FALSE;
	char* override_string;
	int option_index = 1;

	struct utsname* unameData = calloc(1, sizeof(struct utsname));
	uname(unameData);

	while(option_index <= argc)
	{
		if(NULL == argv[option_index])
		{
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "--exact"))
		{
			exact = TRUE;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "--override"))
		{
			override = TRUE;
			if((option_index + 1) < argc)
			{
				override_string = argv[option_index + 1];
				option_index = option_index + 2;
			}
			else
			{
				fputs("--override requires an actual override string\n", stderr);
				exit(EXIT_FAILURE);
			}
		}
		else if(match(argv[option_index], "--os") || match(argv[option_index], "--OS"))
		{
			if(override) fputs(override_string, stdout);
			else fputs(unameData->sysname, stdout);
			fputc('\n', stdout);
			exit(EXIT_SUCCESS);
		}
		else if(match(argv[option_index], "--blood"))
		{
			if(override) fputs(override_string, stdout);
			else if(match("aarch64", unameData->machine)
			     || match("amd64", unameData->machine)
			     || match("ppc64le", unameData->machine)
			     || match("riscv64", unameData->machine)
			     || match("x86_64", unameData->machine)) fputs("--64", stdout);
			fputc('\n', stdout);
			exit(EXIT_SUCCESS);
		}
		else if(match(argv[option_index], "--endian"))
		{
			if(override) fputs(override_string, stdout);
			else if(match("aarch64", unameData->machine)
			     || match("amd64", unameData->machine)
			     || match("ppc64le", unameData->machine)
			     || match("riscv64", unameData->machine)
			     || match("x86_64", unameData->machine)
			     || match("i386", unameData->machine)
			     || match("i486", unameData->machine)
			     || match("i586", unameData->machine)
			     || match("i686", unameData->machine)
			     || match("i686-pae", unameData->machine))fputs("--little-endian", stdout);
			else fputs("--big-endian", stdout);
			fputc('\n', stdout);
			exit(EXIT_SUCCESS);
		}
		else if(match(argv[option_index], "--hex2"))
		{
			if(override) fputs(override_string, stdout);
			else if(match("aarch64", unameData->machine)) fputs("0x400000", stdout);
			else if(match("armv7l", unameData->machine)) fputs("0x10000", stdout);
			else if(match("amd64", unameData->machine)
			     || match("x86_64", unameData->machine)) fputs("0x600000", stdout);
			else if(match("ppc64le", unameData->machine)) fputs("0x10000", stdout);
			else if(match("riscv64", unameData->machine)) fputs("0x600000", stdout);
			else if(match("i386", unameData->machine)
			     || match("i486", unameData->machine)
			     || match("i586", unameData->machine)
			     || match("i686", unameData->machine)
			     || match("i686-pae", unameData->machine)) fputs("0x08048000", stdout);
			else fputs("0x0", stdout);
			fputc('\n', stdout);
			exit(EXIT_SUCCESS);
		}
		else if(match(argv[option_index], "-V") || match(argv[option_index], "--version"))
		{
			fputs("get_machine 1.5.0\n", stdout);
			exit(EXIT_SUCCESS);
		}
		else if(match(argv[option_index], "-h") || match(argv[option_index], "--help"))
		{
			fputs("If you want exact architecture use --exact\n", stderr);
			fputs("If you want to know the Operating system use --os\n", stderr);
			fputs("If you wish to override the output to anything you want use --override\n", stderr);
			exit(EXIT_SUCCESS);
		}
		else
		{
			fputs("Unknown option\n", stderr);
			exit(EXIT_FAILURE);
		}
	}

	if(override) fputs(override_string, stdout);
	else if(!exact)
	{
		if(match("i386", unameData->machine) ||
		   match("i486", unameData->machine) ||
		   match("i586", unameData->machine) ||
		   match("i686", unameData->machine) ||
		   match("i686-pae", unameData->machine)) fputs("x86", stdout);
		else if(match("x86_64", unameData->machine)) fputs("amd64", stdout);
		else fputs(unameData->machine, stdout);
	}
	else fputs(unameData->machine, stdout);
	fputs("\n", stdout);
	return EXIT_SUCCESS;
}
