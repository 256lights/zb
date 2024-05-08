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
#include<string.h>

char* numerate_number(int a)
{
	char* result = malloc(16);
	memset(result, 0, 16);
	int i = 0;

	/* Deal with Zero case */
	if(0 == a)
	{
		result[0] = '0';
		result[1] = 10;
		return result;
	}

	/* Deal with negatives */
	if(0 > a)
	{
		result[0] = '-';
		i = 1;
		a = a * -1;
	}

	/* Using the largest 10^n number possible in 32bits */
	int divisor = 0x3B9ACA00;
	/* Skip leading Zeros */
	while(0 == (a / divisor)) divisor = divisor / 10;

	/* Now simply collect numbers until divisor is gone */
	while(0 < divisor)
	{
		result[i] = ((a / divisor) + 48);
		a = a % divisor;
		divisor = divisor / 10;
		i = i + 1;
	}

	result[i] = 10;
	return result;
}

void write_string(char* s, FILE* f)
{
	while(0 != s[0])
	{
		fputc(s[0], f);
		s = s + 1;
	}
}

int main()
{
	write_string(numerate_number(1248), stdout);
	write_string(numerate_number(0), stdout);
	write_string(numerate_number(-1248), stdout);
	return 0;
}
