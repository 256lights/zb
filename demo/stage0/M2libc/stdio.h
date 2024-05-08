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

#ifndef _STDIO_H
#define _STDIO_H

#ifdef __M2__

/* Actual format of FILE */
struct __IO_FILE
{
	int fd;
	int bufmode; /* O_RDONLY = 0, O_WRONLY = 1 */
	int bufpos;
	int file_pos;
	int buflen;
	char* buffer;
	struct __IO_FILE* next;
	struct __IO_FILE* prev;
};

/* Now give us the FILE we all love */
typedef struct __IO_FILE FILE;

#include <stdio.c>
#else

#include <sys/types.h>
#include <sys/stat.h>
#include <fcntl.h>
#include <unistd.h>
#include <stdlib.h>

/* Required constants */
/* For file I/O*/
#define EOF -1
#define BUFSIZ 4096

/* For lseek */
#define SEEK_SET 0
#define SEEK_CUR 1
#define SEEK_END 2

/* Actual format of FILE */
struct __IO_FILE
{
	int fd;
	int bufmode; /* 0 = no buffer, 1 = read, 2 = write */
	int bufpos;
	int buflen;
	char* buffer;
};

/* Now give us the FILE we all love */
typedef struct __IO_FILE FILE;

/* Required variables */
extern FILE* stdin;
extern FILE* stdout;
extern FILE* stderr;

/* Standard C functions */
/* Getting */
extern int fgetc(FILE* f);
extern int getchar();
extern char* fgets(char* str, int count, FILE* stream);
extern size_t fread( void* buffer, size_t size, size_t count, FILE* stream );

/* Putting */
extern void fputc(char s, FILE* f);
extern void putchar(char s);
extern int fputs(char const* str, FILE* stream);
extern int puts(char const* str);
extern size_t fwrite(void const* buffer, size_t size, size_t count, FILE* stream );

/* File management */
extern FILE* fopen(char const* filename, char const* mode);
extern int fclose(FILE* stream);
extern int fflush(FILE* stream);

/* File Positioning */
extern int ungetc(int ch, FILE* stream);
extern long ftell(FILE* stream);
extern int fseek(FILE* f, long offset, int whence);
extern void rewind(FILE* f);
#endif
#endif
