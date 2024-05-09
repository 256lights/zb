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

#define TRUE 1
#define FALSE 0
#define H 72
#define e 101
#define l 108
#define o 111
#define space 32
#define newline 10
#define m 109
#define s 115
// CONSTANT TRUE 1
// CONSTANT FALSE 0
// CONSTANT H 72
// CONSTANT e 101
// CONSTANT l 108
// CONSTANT o 111
// CONSTANT space 32
// CONSTANT newline 10
// CONSTANT m 109
// CONSTANT s 115

int main()
{
	if(TRUE)
	{
		putchar(H);
		putchar(e);
		putchar(l);
		putchar(l);
		putchar(o);
		putchar(space);
	}
	else
	{
		exit(2);
	}

	if(FALSE)
	{
		exit(3);
	}
	else
	{
		putchar(m);
		putchar(e);
		putchar(s);
		putchar(newline);
	}

	if(1 < 2)
	{
		exit(42);
	}

	return 1;
}
