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

#ifndef _BOOTSTRAPPABLE_H
#define _BOOTSTRAPPABLE_H

/* Essential common CONSTANTS*/
#define TRUE 1
#define FALSE 0

#ifdef __M2__
#include <bootstrappable.c>
#else
/* Universally useful functions */
void require(int bool, char* error);
int match(char* a, char* b);
int in_set(int c, char* s);
int strtoint(char *a);
char* int2str(int x, int base, int signed_p);

#endif
#endif
