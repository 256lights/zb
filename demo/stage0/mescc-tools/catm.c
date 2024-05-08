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
#include <unistd.h>
#include <fcntl.h>

// CONSTANT BUFFER_SIZE 4096
#define BUFFER_SIZE 4096

int main(int argc, char** argv)
{
	if(2 > argc)
	{
		fputs("catm requires 2 or more arguments\n", stderr);
		exit(EXIT_FAILURE);
	}

	int output = open(argv[1], 577 , 384);
	if(-1 == output)
	{
		fputs("The file: ", stderr);
		fputs(argv[1], stderr);
		fputs(" is not a valid output file name\n", stderr);
		exit(EXIT_FAILURE);
	}

	int i;
	int bytes;
	char* buffer = calloc(BUFFER_SIZE + 1, sizeof(char));
	int input;
	for(i = 2; i < argc ; i =  i + 1)
	{
		input = open(argv[i], 0, 0);
		if(-1 == input)
		{
			fputs("The file: ", stderr);
			fputs(argv[i], stderr);
			fputs(" is not a valid input file name\n", stderr);
			exit(EXIT_FAILURE);
		}
keep:
		bytes = read(input, buffer, BUFFER_SIZE);
		write(output, buffer, bytes);
		if(BUFFER_SIZE == bytes) goto keep;
	}

	free(buffer);
	return EXIT_SUCCESS;
}
