/* Copyright (C) 2019 Jeremiah Orians
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
#include "M2libc/bootstrappable.h"

char* input_name;
FILE* input;
char* output_name;
FILE* output;
char* pattern;
size_t pattern_length;
char* replacement;
char* buffer;
size_t buffer_index;
char* hold;

void read_next_byte()
{
	int c= hold[0];
	size_t i = 0;
	while(i < pattern_length)
	{
		hold[i] = hold[i+1];
		i = i + 1;
	}

	hold[pattern_length-1] = buffer[buffer_index];
	buffer_index = buffer_index + 1;

	/* NEVER WRITE NULLS!!! */
	if(0 != c) fputc(c, output);
}

void clear_hold()
{
	/* FILL hold with NULLS */
	size_t i = 0;
	while(i < pattern_length)
	{
		hold[i] = 0;
		i = i + 1;
	}
}

void check_match()
{
	/* Do the actual replacing */
	if(match(pattern, hold))
	{
		fputs(replacement, output);
		clear_hold();
	}
}

int main(int argc, char** argv)
{
	output_name = "/dev/stdout";
	pattern = NULL;
	replacement = NULL;
	buffer_index = 0;

	int i = 1;
	while (i < argc)
	{
		if(NULL == argv[i])
		{
			i = i + 1;
		}
		else if(match(argv[i], "-f") || match(argv[i], "--file"))
		{
			input_name = argv[i+1];
			require(NULL != input_name, "the --file option requires a filename to be given\n");
			i = i + 2;
		}
		else if(match(argv[i], "-o") || match(argv[i], "--output"))
		{
			output_name = argv[i+1];
			require(NULL != output_name, "the --output option requires a filename to be given\n");
			i = i + 2;
		}
		else if(match(argv[i], "-m") || match(argv[i], "--match-on"))
		{
			pattern = argv[i+1];
			require(NULL != pattern, "the --match-on option requires a string to be given\n");
			i = i + 2;
		}
		else if(match(argv[i], "-r") || match(argv[i], "--replace-with"))
		{
			replacement = argv[i+1];
			require(NULL != replacement, "the --replace-with option requires a string to be given\n");
			i = i + 2;
		}
		else if(match(argv[i], "-h") || match(argv[i], "--help"))
		{
			fputs("Usage: ", stderr);
			fputs(argv[0], stderr);
			fputs(" --file $input", stderr);
			fputs(" --match-on $string", stderr);
			fputs(" --replace-with $string", stderr);
			fputs(" [--output $output] (or it'll dump to stdout)\n", stderr);
			fputs("--help to get this message\n", stderr);
			exit(EXIT_SUCCESS);
		}
		else
		{
			fputs("Unknown option:", stderr);
			fputs(argv[i], stderr);
			fputs("\nAborting to avoid problems\n", stderr);
			exit(EXIT_FAILURE);
		}
	}

	/* Sanity check that we got everything we need */
	require(NULL != input_name, "You need to pass an input file with --file\n");
	require(NULL != output_name, "You need to pass an output file with --output\n");
	require(NULL != pattern, "You can't do a replacement without something to match on\n");
	require(NULL != replacement, "You can't do a replacement without something to replace it with\n");

	input = fopen(input_name, "r");
	require(NULL != input, "unable to open requested input file!\n");

	/* Get enough buffer to read it all */
	fseek(input, 0, SEEK_END);
	size_t size = ftell(input);
	buffer = malloc((size + 8) * sizeof(char));

	/* Save ourself work if the input file is too small */
	pattern_length = strlen(pattern);
	require(pattern_length < size, "input file is to small for pattern\n");

	/* Now read it all into buffer */
	fseek(input, 0, SEEK_SET);
	size_t r = fread(buffer,sizeof(char), size, input);
	require(r == size, "incomplete read of input\n");
	fclose(input);

	/* Now we can safely open the output (which could have been the same as the input */
	output = fopen(output_name, "w");
	require(NULL != input, "unable to open requested output file!\n");

	/* build our match buffer */
	hold = calloc(pattern_length + 4, sizeof(char));
	require(NULL != hold, "temp memory allocation failed\n");

	/* Replace it all */
	while((size + pattern_length + 4) >= buffer_index)
	{
		read_next_byte();
		check_match();
	}
	fclose(output);
}
