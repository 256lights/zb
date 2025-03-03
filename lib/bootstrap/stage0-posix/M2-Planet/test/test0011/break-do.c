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

int main()
{
	int i = 65;
	do
	{
		putchar(i);
		if(90 == i)
		{
			break;
		}
		i = i + 1;
	} while (i <= 120);

	putchar(10);
	i = 65;
	int j;
	do
	{
		j = i;
		do
		{
			if(70 == i)
			{
				break;
			}
			putchar(j);
			j = j + 1;
		} while (j <= 90);
		putchar(10);
		if(90 == i) break;
		i = i + 1;
	} while (i <= 120);
	return 0;
}
