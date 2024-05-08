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

/* Deal with the fact GCC converts the 1 to the size of the structs being iterated over */
#if __GCC__
#define CELL_SIZE 1
#else
//CONSTANT CELL_SIZE sizeof(struct cell)
#define CELL_SIZE sizeof(struct cell)
#endif

struct cell *free_cells;
struct cell *gc_block_start;
struct cell *top_allocated;

void update_remaining()
{
	int count = 0;
	struct cell* i = free_cells;
	while(NULL != i)
	{
		count = count + 1;
		i = i->cdr;
	}
	left_to_take = count;
}

struct cell* insert_ordered(struct cell* i, struct cell* list)
{
	if(NULL == list)
	{
		return i;
	}

	if(i < list)
	{
		i->cdr = list;
		return i;
	}

	list->cdr = insert_ordered(i, list->cdr);
	return list;
}

void reclaim_marked()
{
	struct cell* i;
	for(i= top_allocated; i >= gc_block_start ; i = i - CELL_SIZE)
	{
		if(i->type & MARKED)
		{
			i->type = FREE;
			i->car = NULL;
			i->cdr = NULL;
			i->env = NULL;
			free_cells = insert_ordered(i, free_cells);
		}
	}
}

void relocate_cell(struct cell* current, struct cell* target, struct cell* list)
{
	for(; NULL != list; list = list->cdr)
	{
		if(list->car == current)
		{
			list->car = target;
		}

		if(list->cdr == current)
		{
			list->cdr = target;
		}

		if(list->env == current)
		{
			list->env = target;
		}

		if((list->type & CONS)|| list->type & PROC )
		{
			relocate_cell(current, target, list->car);
		}
	}
}

struct cell* pop_cons();
void compact(struct cell* list)
{
	struct cell* temp;
	for(; NULL != list; list = list->cdr)
	{
		if((FREE != list->type) && (list > free_cells ))
		{
			temp = pop_cons();
			temp->type = list->type;
			temp->car = list->car;
			temp->cdr = list->cdr;
			temp->env = list->env;
			relocate_cell(list, temp, all_symbols);
			relocate_cell(list, temp, top_env);
		}

		if((list->type & CONS)|| list->type & PROC )
		{
			compact(list->car);
		}
	}
}


void mark_all_cells()
{
	struct cell* i;
	for(i= gc_block_start; i < top_allocated; i = i + CELL_SIZE)
	{
		/* if not in the free list */
		if(!(i->type & FREE))
		{
			/* Mark it */
			i->type = i->type | MARKED;
		}
	}
}

void unmark_cells(struct cell* list, struct cell* stop, int count)
{
	if(count > 1) return;

	for(; NULL != list; list = list->cdr)
	{
		if(list == stop) count = count + 1;
		list->type = list->type & ~MARKED;

		if(list->type & PROC)
		{
			unmark_cells(list->car, stop, count);
			if(NULL != list->env)
			{
				unmark_cells(list->env, stop, count);
			}
		}

		if(list->type & CONS)
		{
			unmark_cells(list->car, stop, count);
		}
	}
}

void garbage_collect()
{
	mark_all_cells();
	unmark_cells(current, current, 0);
	unmark_cells(all_symbols, all_symbols, 0);
	unmark_cells(top_env, top_env, 0);
	reclaim_marked();
	update_remaining();
	compact(all_symbols);
	compact(top_env);
	top_allocated = NULL;
}

void garbage_init(int number_of_cells)
{
	gc_block_start = calloc(number_of_cells + 1, sizeof(struct cell));
	top_allocated = gc_block_start + number_of_cells;
	free_cells = NULL;
	garbage_collect();
	top_allocated = NULL;
}

struct cell* pop_cons()
{
	if(NULL == free_cells)
	{
		fputs("OOOPS we ran out of cells", stderr);
		exit(EXIT_FAILURE);
	}
	struct cell* i;
	i = free_cells;
	free_cells = i->cdr;
	i->cdr = NULL;
	if(i > top_allocated)
	{
		top_allocated = i;
	}
	left_to_take = left_to_take - 1;
	return i;
}

struct cell* make_cell(int type, struct cell* a, struct cell* b, struct cell* env)
{
	struct cell* c = pop_cons();
	c->type = type;
	c->car = a;
	c->cdr = b;
	c->env = env;
	return c;
}

struct cell* make_int(int a)
{
	struct cell* c = pop_cons();
	c->type = INT;
	c->value = a;
	return c;
}

struct cell* make_char(int a)
{
	struct cell* c = pop_cons();
	c->type = CHAR;
	c->value = a;
	return c;
}

struct cell* make_string(char* a)
{
	struct cell* c = pop_cons();
	c->type = STRING;
	c->string = a;
	return c;
}

struct cell* make_sym(char* name)
{
	struct cell* c = pop_cons();
	c->type = SYM;
	c->string = name;
	return c;
}

struct cell* make_cons(struct cell* a, struct cell* b)
{
	return make_cell(CONS, a, b, nil);
}

struct cell* make_proc(struct cell* a, struct cell* b, struct cell* env)
{
	return make_cell(PROC, a, b, env);
}

struct cell* make_prim(void* fun)
{
	struct cell* c = pop_cons();
	c->type = PRIMOP;
	c->function = fun;
	return c;
}
