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
#include<stdio.h>

void write_string(FILE* f)
{
	int i = 65;
	int j;
	do
	{
		j = i;
		do
		{
			fputc(j, f);
			j = j + 1;
		} while (j <= 90);
		i = i + 1;
		fputc(10, f);
	} while (i <= 90);
}

int main(int argc, char** argv)
{
	FILE* f = stdout;
	if(2 == argc)
	{
		f = fopen(argv[1], "w");
	}
	write_string(f);
	if (f != stdout)
	{
		fclose(f);
	}
	return 0;
}
