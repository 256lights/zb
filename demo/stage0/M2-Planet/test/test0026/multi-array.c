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
#include <stdio.h>

char** env_argv;

char getargchar(int n, int k)
{
	return env_argv[n][k];
}

void setargchar(int n, int k)
{
	env_argv[n][k] = 'Z';
}

int main(int argc, char** argv)
{
	if(4 != argc) return 1;
	env_argv = argv;
	fputc(getargchar(2, 4), stdout);
	setargchar(3, 5);
	fputs(argv[3], stdout);
	return 0;
}
