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

#ifndef _STDLIB_H
#define _STDLIB_H
#include <unistd.h>

#ifdef __M2__
#include <stdlib.c>
#else


#define EXIT_FAILURE 1
#define EXIT_SUCCESS 0

extern void exit(int value);

extern long _malloc_ptr;
extern long _brk_ptr;

extern void free(void* l);
extern void* malloc(unsigned size);
extern void* memset(void* ptr, int value, int num);
extern void* calloc(int count, int size);
extern char *getenv(const char *name);

size_t wcstombs(char* dest, const wchar_t* src, size_t n);

#endif
#endif
