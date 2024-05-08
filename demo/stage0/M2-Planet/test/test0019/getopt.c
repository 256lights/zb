/* Copyright (C) 2016 Jeremiah Orians
 * This file is part of M2-Planet.
 *
 * M2-Planet is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * M2-Planet is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with M2-Planet.  If not, see <http://www.gnu.org/licenses/>.
 */
#include<stdlib.h>
#include<stdio.h>
char* int2str(int x, int base, int signed_p);

int char2int(char c)
{
	if((48 <= c) && (57>= c))
	{
		return (c - 48);
	}
	return 0;
}

void write_string(char* c, FILE* f)
{
	while(0 != c[0])
	{
		fputc(c[0], f);
		c = c + 1;
	}
}

void sum_file(FILE* input, FILE* output)
{
	int c = fgetc(input);
	int sum = 0;
	while(0 <= c)
	{
		sum = sum + char2int(c);
		c = fgetc(input);
	}

	write_string(int2str(sum, 10, 0), output);
	fputc(10, output);
}

int match(char* a, char* b);
int main(int argc, char** argv)
{
	FILE* in = stdin;
	FILE* out = stdout;
	int i = 1;
	while(i <= argc)
	{
		if(NULL == argv[i])
		{
			i = i + 1;
		}
		else if(match(argv[i], "-f"))
		{
			in = fopen(argv[i + 1], "r");
			if(NULL == in)
			{
				write_string("Unable to open for reading file: ", stderr);
				write_string(argv[i + 1], stderr);
				write_string("\x0A Aborting to avoid problems\x0A", stderr);
				exit(EXIT_FAILURE);
			}
			i = i + 2;
		}
		else if(match(argv[i], "-o"))
		{
			out = fopen(argv[i + 1], "w");
			if(NULL == out)
			{
				write_string("Unable to open for writing file: ", stderr);
				write_string(argv[i + 1], stderr);
				write_string("\x0A Aborting to avoid problems\x0A", stderr);
				exit(EXIT_FAILURE);
			}
			i = i + 2;
		}
		else if(match(argv[i], "--help"))
		{
			write_string(" -f input file\x0A -o output file\x0A --help for this message\x0A --version for file version\x0A", stdout);
			exit(EXIT_SUCCESS);
		}
		else if(match(argv[i], "--version"))
		{
			write_string("Basic test version 0.0.0.1a\x0A", stderr);
			exit(EXIT_SUCCESS);
		}
		else
		{
			write_string("UNKNOWN ARGUMENT\x0A", stdout);
			exit(EXIT_FAILURE);
		}
	}

	sum_file(in, out);

	if (in != stdin)
	{
		fclose(in);
	}
	if (out != stdout)
	{
		fclose(out);
	}
	return 0;
}
