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

#include "lisp.h"
#include <stdint.h>
#include <string.h>

FILE* source_file;
int Reached_EOF;

struct cell* token_stack;
struct cell* make_sym(char* name);
struct cell* intern(char *name);
struct cell* findsym(char *name);

/****************************************************************
 *      "Convert a string into a list of tokens."               *
 ****************************************************************/
struct cell* tokenize(struct cell* head, char* fullstring, int size)
{
	int i = 0;
	int done = FALSE;
	if((0 >= size) || (0 == fullstring[0]))
	{
		return head;
	}

	char *store = calloc(MAX_STRING + 1, sizeof(char));
	int c;

	do
	{
		c = fullstring[i];
		if((i > size) || (MAX_STRING <= i))
		{
			done = TRUE;
		}
		else if(34 == c)
		{
			store[i] = c;
			i = i + 1;
			while(34 != fullstring[i])
			{
				store[i] = fullstring[i];
				i = i + 1;
			}
			i = i + 1;
			done = TRUE;
		}
		else
		{
			if((' ' == c) || ('\t' == c) || ('\n' == c) | ('\r' == c))
			{
				i = i + 1;
				done = TRUE;
			}
			else
			{
				store[i] = c;
				i = i + 1;
			}
		}
	} while(!done);

	if(i > 1)
	{
		struct cell* temp = make_sym(store);
		temp->cdr = head;
		head = temp;
	}
	else
	{
		free(store);
	}
	head = tokenize(head, (fullstring+i), (size - i));
	return head;
}


int is_integer(char* a)
{
	if(('0' <= a[0]) && ('9' >= a[0]))
	{
		return TRUE;
	}

	if('-' == a[0])
	{
		if(('0' <= a[1]) && ('9' >= a[1]))
		{
			return TRUE;
		}
	}

	return FALSE;
}


/********************************************************************
 *     Numbers become numbers                                       *
 *     Strings become strings                                       *
 *     Functions become functions                                   *
 *     quoted things become quoted                                  *
 *     Everything is treated like a symbol                          *
 ********************************************************************/
struct cell* atom(struct cell* a)
{
	/* Check for quotes */
	if('\'' == a->string[0])
	{
		a->string = a->string + 1;
		return make_cons(quote, make_cons(a, nil));
	}

	/* Check for strings */
	if(34 == a->string[0])
	{
		a->type = STRING;
		a->string = a->string + 1;
		return a;
	}

	/* Check for integer */
	if(is_integer(a->string))
	{
		a->type = INT;
		a->value = strtoint(a->string);
		return a;
	}

	/* Check for functions */
	struct cell* op = findsym(a->string);
	if(nil != op)
	{
		return op->car;
	}

	/* Assume new symbol */
	all_symbols = make_cons(a, all_symbols);
	return a;
}

/****************************************************************
 *     "Read an expression from a sequence of tokens."          *
 ****************************************************************/
struct cell* readlist();
struct cell* readobj()
{
	struct cell* head = token_stack;
	token_stack = head->cdr;
	head->cdr = NULL;
	if (match("(", head->string))
	{
		return readlist();
	}

	return atom(head);
}

struct cell* readlist()
{
	struct cell* head = token_stack;
	if (match(")", head->string))
	{
		token_stack = head->cdr;
		return nil;
	}

	struct cell* tmp = readobj();
/*	token_stack = head->cdr; */
	return make_cons(tmp,readlist());
}

/****************************************************
 *     Put list of tokens in correct order          *
 ****************************************************/
struct cell* reverse_list(struct cell* head)
{
	struct cell* root = NULL;
	struct cell* next;
	while(NULL != head)
	{
		next = head->cdr;
		head->cdr = root;
		root = head;
		head = next;
	}
	return root;
}

/****************************************************
 *     "Read a Scheme expression from a string."    *
 ****************************************************/
struct cell* parse(char* program, int size)
{
	token_stack = tokenize(NULL, program, size);
	if(NULL == token_stack)
	{
		return nil;
	}
	token_stack = reverse_list(token_stack);
	return readobj();
}

/****************************************************
 * Do the heavy lifting of reading an s-expreesion  *
 ****************************************************/
unsigned Readline(FILE* source_file, char* temp)
{
	int c;
	unsigned i;
	unsigned depth = 0;

	for(i = 0; i < MAX_STRING; i = i + 1)
	{
restart_comment:
		c = fgetc(source_file);
		if((-1 == c) || (4 == c))
		{
			return i;
		}
		else if(';' == c)
		{
			/* drop everything until we hit newline */
			while('\n' != c)
			{
				c = fgetc(source_file);
			}
			goto restart_comment;
		}
		else if('"' == c)
		{ /* Deal with strings */
			temp[i] = c;
			i = i + 1;
			c = fgetc(source_file);
			while('"' != c)
			{
				temp[i] = c;
				i = i + 1;
				c = fgetc(source_file);
			}
			temp[i] = c;
		}
		else if((0 == depth) && (('\n' == c) || ('\r' == c) || (' ' == c) || ('\t' == c)))
		{
			goto Line_complete;
		}
		else if(('(' == c) || (')' == c))
		{
			if('(' == c)
			{
				depth = depth + 1;
			}

			if(')' == c)
			{
				depth = depth - 1;
			}

			temp[i] = ' ';
			temp[i+1] = c;
			temp[i+2] = ' ';
			i = i + 2;
		}
		else
		{
			temp[i] = c;
		}
	}

Line_complete:
	if(1 > i)
	{
		return Readline(source_file, temp);
	}

	return i;
}
