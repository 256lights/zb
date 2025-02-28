/* Copyright (C) 2016 Jeremiah Orians
 * This file is part of stage0.
 *
 * stage0 is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * stage0 is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with stage0.  If not, see <http://www.gnu.org/licenses/>.
 */

#include "gcc_req.h"

//CONSTANT FREE 1
#define FREE 1
//CONSTANT MARKED 2
#define MARKED 2
//CONSTANT INT 4
#define INT 4
//CONSTANT SYM 8
#define SYM 8
//CONSTANT CONS 16
#define CONS 16
//CONSTANT PROC 32
#define PROC 32
//CONSTANT PRIMOP 64
#define PRIMOP 64
//CONSTANT CHAR 128
#define CHAR 128
//CONSTANT STRING 256
#define STRING 256

// CONSTANT FALSE 0
#define FALSE 0
// CONSTANT TRUE 1
#define TRUE 1

struct cell
{
	int type;
	union
	{
		struct cell* car;
		int value;
		char* string;
		FUNCTION* function;
	};
	struct cell* cdr;
	struct cell* env;
};

// CONSTANT MAX_STRING 4096
#define MAX_STRING 4096

/* Common functions */
struct cell* make_cons(struct cell* a, struct cell* b);
int strtoint(char *a);
char* int2str(int x, int base, int signed_p);
int match(char* a, char* b);

/* Global objects */
struct cell *all_symbols;
struct cell *top_env;
struct cell *nil;
struct cell *tee;
struct cell *quote;
struct cell *s_if;
struct cell *s_lambda;
struct cell *s_define;
struct cell *s_setb;
struct cell *s_cond;
struct cell *s_begin;
struct cell *s_let;
struct cell *s_while;
struct cell *current;
FILE* input;
FILE* file_output;
FILE* console_output;
int echo;
int left_to_take;
