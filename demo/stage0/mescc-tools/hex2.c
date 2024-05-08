/* -*- c-file-style: "linux";indent-tabs-mode:t -*- */
/* Copyright (C) 2017 Jeremiah Orians
 * Copyright (C) 2017 Jan Nieuwenhuizen <janneke@gnu.org>
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

#include "hex2_globals.h"

/* The essential functions */
void first_pass(struct input_files* input);
void second_pass(struct input_files* input);
void WordFirstPass(struct input_files* input);
void WordSecondPass(struct input_files* input);

/* Standard C main program */
int main(int argc, char **argv)
{
	int InsaneArchitecture = FALSE;
	ALIGNED = FALSE;
	BigEndian = TRUE;
	jump_tables = calloc(65537, sizeof(struct entry*));
	require(NULL != jump_tables, "Failed to allocate our jump_tables\n");

	Architecture = KNIGHT;
	Base_Address = 0;
	struct input_files* input = NULL;
	output = stdout;
	char* output_file = "";
	exec_enable = TRUE;
	ByteMode = HEX;
	scratch = calloc(max_string + 1, sizeof(char));
	require(NULL != scratch, "failed to allocate our scratch buffer\n");
	char* arch;
	struct input_files* temp;

	int option_index = 1;
	while(option_index <= argc)
	{
		if(NULL == argv[option_index])
		{
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "--big-endian"))
		{
			BigEndian = TRUE;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "--little-endian"))
		{
			BigEndian = FALSE;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "--non-executable"))
		{
			exec_enable = FALSE;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "-A") || match(argv[option_index], "--architecture"))
		{
			arch = argv[option_index + 1];
			if(match("knight-native", arch) || match("knight-posix", arch)) Architecture = KNIGHT;
			else if(match("x86", arch)) Architecture = X86;
			else if(match("amd64", arch)) Architecture = AMD64;
			else if(match("armv7l", arch)) Architecture = ARMV7L;
			else if(match("aarch64", arch)) Architecture = AARM64;
			else if(match("ppc64le", arch)) Architecture = PPC64LE;
			else if(match("riscv32", arch)) Architecture = RISCV32;
			else if(match("riscv64", arch)) Architecture = RISCV64;
			else
			{
				fputs("Unknown architecture: ", stderr);
				fputs(arch, stderr);
				fputs(" know values are: knight-native, knight-posix, x86, amd64, armv7l, riscv32 and riscv64", stderr);
			}
			option_index = option_index + 2;
		}
		else if(match(argv[option_index], "-b") || match(argv[option_index], "--binary"))
		{
			ByteMode = BINARY;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "-B") || match(argv[option_index], "--base-address"))
		{
			Base_Address = strtoint(argv[option_index + 1]);
			option_index = option_index + 2;
		}
		else if(match(argv[option_index], "-h") || match(argv[option_index], "--help"))
		{
			fputs("Usage: ", stderr);
			fputs(argv[0], stderr);
			fputs(" --file FILENAME1 {-f FILENAME2} (--big-endian|--little-endian)", stderr);
			fputs(" [--base-address 0x12345] [--architecture name]\nArchitecture:", stderr);
			fputs(" knight-native, knight-posix, x86, amd64, armv7l, aarch64, riscv32 and riscv64\n", stderr);
			fputs("To leverage octal or binary input: --octal, --binary\n", stderr);
			exit(EXIT_SUCCESS);
		}
		else if(match(argv[option_index], "-f") || match(argv[option_index], "--file"))
		{
			temp = calloc(1, sizeof(struct input_files));
			require(NULL != temp, "failed to allocate file for processing\n");
			temp->filename = argv[option_index + 1];
			temp->next = input;
			input = temp;
			option_index = option_index + 2;
		}
		else if(match(argv[option_index], "-o") || match(argv[option_index], "--output"))
		{
			output_file = argv[option_index + 1];
			output = fopen(output_file, "w");

			if(NULL == output)
			{
				fputs("The file: ", stderr);
				fputs(argv[option_index + 1], stderr);
				fputs(" can not be opened!\n", stderr);
				exit(EXIT_FAILURE);
			}
			option_index = option_index + 2;
		}
		else if(match(argv[option_index], "-O") || match(argv[option_index], "--octal"))
		{
			ByteMode = OCTAL;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "-V") || match(argv[option_index], "--version"))
		{
			fputs("hex2 1.5.0\n", stdout);
			exit(EXIT_SUCCESS);
		}
		else
		{
			fputs("Unknown option\n", stderr);
			exit(EXIT_FAILURE);
		}
	}

	if((Architecture == RISCV32) || (Architecture == RISCV64))
	{
		/* Forcing me to use words instead of just byting into the problem */
		InsaneArchitecture = TRUE;
	}

	/* Catch a common mistake */
	if((KNIGHT != Architecture) && (0 == Base_Address))
	{
		fputs(">> WARNING <<\n>> WARNING <<\n>> WARNING <<\n", stderr);
		fputs("If you are not generating a ROM image this binary will likely not work\n", stderr);
	}

	/* Catch implicitly false assumptions */
	if(BigEndian && ((X86 == Architecture) || ( AMD64 == Architecture) || (ARMV7L == Architecture) || (AARM64 == Architecture) || (RISCV32 == Architecture) || (RISCV64 == Architecture)))
	{
		fputs(">> WARNING <<\n>> WARNING <<\n>> WARNING <<\n", stderr);
		fputs("You have specified big endian output on likely a little endian processor\n", stderr);
		fputs("if this is a mistake please pass --little-endian next time\n", stderr);
	}

	/* Make sure we have a program tape to run */
	if (NULL == input)
	{
		return EXIT_FAILURE;
	}

	/* Get all of the labels */
	ip = Base_Address;
	if(InsaneArchitecture) WordFirstPass(input);
	else first_pass(input);

	/* Fix all the references*/
	ip = Base_Address;
	if(InsaneArchitecture) WordSecondPass(input);
	else second_pass(input);

	/* flush all writes */
	fflush(output);

	/* Set file as executable */
	if(exec_enable && (output != stdout))
	{
		/* Close output file */
		fclose(output);

		if(0 != chmod(output_file, 0750))
		{
			fputs("Unable to change permissions\n", stderr);
			exit(EXIT_FAILURE);
		}
	}

	return EXIT_SUCCESS;
}
