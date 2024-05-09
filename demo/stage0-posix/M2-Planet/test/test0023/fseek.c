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

int main(int argc, char** argv)
{
	if(2 != argc) return 2;
	FILE* f = fopen(argv[1], "r");
	fseek(f, 15, SEEK_SET);
	int c = fgetc(f);
	fputc(c, stdout);

	fseek(f, -8, SEEK_END);
	c = fgetc(f);
	fputc(c, stdout);

	fseek(f, -19, SEEK_CUR);
	c = fgetc(f);
	fputc(c, stdout);

	fseek(f, 7, SEEK_CUR);
	c = fgetc(f);
	fputc(c, stdout);

	fseek(f, -2, SEEK_END);
	c = fgetc(f);
	fputc(c, stdout);

	fseek(f, 34, SEEK_SET);
	c = fgetc(f);
	fputc(c, stdout);

	fseek(f, 5, SEEK_CUR);
	c = fgetc(f);
	fputc(c, stdout);

	fseek(f, -5, SEEK_CUR);
	c = fgetc(f);
	fputc(c, stdout);

	fseek(f, 6, SEEK_CUR);
	c = fgetc(f);
	fputc(c, stdout);

	fseek(f, -1, SEEK_END);
	c = fgetc(f);
	fputc(c, stdout);

	fclose(f);

	return 0;
}
