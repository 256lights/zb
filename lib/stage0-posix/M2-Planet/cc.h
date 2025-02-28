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

#include <stdlib.h>
#include <stdio.h>
#include <string.h>

// CONSTANT FALSE 0
#define FALSE 0
// CONSTANT TRUE 1
#define TRUE 1

// CONSTANT KNIGHT_NATIVE 1
#define KNIGHT_NATIVE 1
// CONSTANT KNIGHT_POSIX 2
#define KNIGHT_POSIX 2
// CONSTANT X86 3
#define X86 3
// CONSTANT AMD64 4
#define AMD64 4
// CONSTANT ARMV7L 5
#define ARMV7L 5
// CONSTANT AARCH64 6
#define AARCH64 6
// CONSTANT RISCV32 7
#define RISCV32 7
// CONSTANT RISCV64 8
#define RISCV64 8


void copy_string(char* target, char* source, int max);
int in_set(int c, char* s);
int match(char* a, char* b);
void require(int bool, char* error);
void reset_hold_string();


struct type
{
	struct type* next;
	int size;
	int offset;
	int is_signed;
	struct type* indirect;
	struct type* members;
	struct type* type;
	char* name;
};

struct token_list
{
	struct token_list* next;
	union
	{
		struct token_list* locals;
		struct token_list* prev;
	};
	char* s;
	union
	{
		struct type* type;
		char* filename;
	};
	union
	{
		struct token_list* arguments;
		int depth;
		int linenumber;
	};
};

struct case_list
{
	struct case_list* next;
	char* value;
};

#include "cc_globals.h"
