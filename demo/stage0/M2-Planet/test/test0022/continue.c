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
#include <stdlib.h>

char* int2str(int x, int base, int signed_p);

int main()
{
	int i;
	int j;
	for(i = 0; i < 10; i = i + 1)
	{
		fputs(int2str(i, 10, 0), stdout);
		if(i != 1) continue;
		fputc(' ', stdout);
	}

	fputc('\n', stdout);

	for(i = 0; i < 10; i = i + 1)
	{
		for(j = 0; j < 10; j = j + 1)
		{
			if(j == 2) continue;
			fputs(int2str(i, 10, 0), stdout);
			fputs(int2str(j, 10, 0), stdout);
			fputc(' ', stdout);
		}
	}

	fputc('\n', stdout);

	i = -1;
	while(i < 9)
	{
		i = i + 1;
		fputs(int2str(i, 10, 0), stdout);
		if(i != 3) continue;
		fputc(' ', stdout);
	}

	fputc('\n', stdout);

	i = -1;
	while(i < 9)
	{
		i = i + 1;
		j = -1;
		while(j < 9)
		{
			j = j + 1;
			if(j == 4) continue;
			fputs(int2str(i, 10, 0), stdout);
			fputs(int2str(j, 10, 0), stdout);
			fputc(' ', stdout);
		}
	}

	fputc('\n', stdout);

	i = -1;
	do
	{
		i = i + 1;
		fputs(int2str(i, 10, 0), stdout);
		if(i != 5) continue;
		fputc(' ', stdout);
	}while(i < 9);

	fputc('\n', stdout);

	i = -1;
	do
	{
		i = i + 1;
		j = -1;
		do
		{
			j = j + 1;
			if(j == 6) continue;
			fputs(int2str(i, 10, 0), stdout);
			fputs(int2str(j, 10, 0), stdout);
			fputc(' ', stdout);
		}while(j < 9);
	}while(i < 9);

	fputc('\n', stdout);

	/* All tests passed */
	return 0;
}
