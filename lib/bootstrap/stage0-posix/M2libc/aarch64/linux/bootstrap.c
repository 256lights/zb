/* Copyright (C) 2016 Jeremiah Orians
 * Copyright (C) 2020 deesix <deesix@tuta.io>
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

// CONSTANT stdin 0
// CONSTANT stdout 1
// CONSTANT stderr 2
// CONSTANT EOF 0xFFFFFFFF
// CONSTANT NULL 0
// CONSTANT EXIT_FAILURE 1
// CONSTANT EXIT_SUCCESS 0
// CONSTANT TRUE 1
// CONSTANT FALSE 0


int fgetc(FILE* f)
{
	asm("SET_X0_FROM_BP" "SUB_X0_8" "DEREF_X0"
	    "PUSH_X0"
	    "SET_X1_FROM_SP"
	    "SET_X2_TO_1"
	    "SET_X8_TO_SYS_READ"
	    "SYSCALL"
	    "SET_X1_TO_0"
	    "CMP_X1_X0"
	    "POP_X0"
	    "SKIP_INST_NE"
	    "SET_X0_TO_MINUS_1");
}

void fputc(char s, FILE* f)
{
	asm("SET_X0_FROM_BP" "SUB_X0_8"
	    "SET_X1_FROM_X0"
	    "SET_X0_FROM_BP" "SUB_X0_16" "DEREF_X0"
	    "SET_X2_TO_1"
	    "SET_X8_TO_SYS_WRITE"
	    "SYSCALL");
}

void fputs(char* s, FILE* f)
{
	while(0 != s[0])
	{
		fputc(s[0], f);
		s = s + 1;
	}
}

FILE* open(char* name, int flag, int mode)
{
	asm("SET_X0_FROM_BP" "SUB_X0_24" "DEREF_X0"
	    "SET_X3_FROM_X0"
	    "SET_X0_FROM_BP" "SUB_X0_16" "DEREF_X0"
	    "SET_X2_FROM_X0"
	    "SET_X0_FROM_BP" "SUB_X0_8" "DEREF_X0"
	    "SET_X1_FROM_X0"
	    "SET_X0_TO_FCNTL_H_AT_FDCWD"
	    "SET_X8_TO_SYS_OPENAT"
	    "SYSCALL");
}

FILE* fopen(char* filename, char* mode)
{
	FILE* f;
	if('w' == mode[0])
	{ /* 577 is O_WRONLY|O_CREAT|O_TRUNC, 384 is 600 in octal */
		f = open(filename, 577 , 384);
	}
	else
	{ /* Everything else is a read */
		f = open(filename, 0, 0);
	}

	/* Negative numbers are error codes */
	if(0 > f)
	{
		return 0;
	}
	return f;
}

int close(int fd)
{
	asm("SET_X0_FROM_BP" "SUB_X0_8" "DEREF_X0"
	    "SET_X8_TO_SYS_CLOSE"
	    "SYSCALL");
}

int fclose(FILE* stream)
{
	int error = close(stream);
	return error;
}

int brk(void *addr)
{
	asm("SET_X0_FROM_BP" "SUB_X0_8" "DEREF_X0"
	    "SET_X8_TO_SYS_BRK"
	    "SYSCALL");
}

long _malloc_ptr;
long _brk_ptr;

void* malloc(int size)
{
	if(NULL == _brk_ptr)
	{
		_brk_ptr = brk(0);
		_malloc_ptr = _brk_ptr;
	}

	if(_brk_ptr < _malloc_ptr + size)
	{
		_brk_ptr = brk(_malloc_ptr + size);
		if(-1 == _brk_ptr) return 0;
	}

	long old_malloc = _malloc_ptr;
	_malloc_ptr = _malloc_ptr + size;
	return old_malloc;
}

int strlen(char* str )
{
	int i = 0;
	while(0 != str[i]) i = i + 1;
	return i;
}

void* memset(void* ptr, int value, int num)
{
	char* s;
	for(s = ptr; 0 < num; num = num - 1)
	{
		s[0] = value;
		s = s + 1;
	}
}

void* calloc(int count, int size)
{
	void* ret = malloc(count * size);
	if(NULL == ret) return NULL;
	memset(ret, 0, (count * size));
	return ret;
}

void free(void* l)
{
	return;
}

void exit(int value)
{
	asm("SET_X0_FROM_BP" "SUB_X0_8" "DEREF_X0"
	    "SET_X8_TO_SYS_EXIT"
	    "SYSCALL");
}
