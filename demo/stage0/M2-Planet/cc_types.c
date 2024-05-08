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
#include "cc.h"

/* Imported functions */
int strtoint(char *a);
void line_error();
void require(int bool, char* error);

/* enable easy primitive extension */
struct type* add_primitive(struct type* a)
{
	if(NULL == prim_types) return a;
	struct type* i = prim_types;
	while(NULL != i->next)
	{
		i = i->next;
	}
	i->next = a;

	return prim_types;
}

/* enable easy primitive creation */
struct type* new_primitive(char* name0, char* name1, char* name2, int size, int sign)
{
	/* Create type** */
	struct type* a = calloc(1, sizeof(struct type));
	require(NULL != a, "Exhausted memory while declaring new primitive**\n");
	a->name = name2;
	a->size = register_size;
	a->indirect = a;
	a->is_signed = sign;

	/* Create type* */
	struct type* b = calloc(1, sizeof(struct type));
	require(NULL != b, "Exhausted memory while declaring new primitive*\n");
	b->name = name1;
	b->size = register_size;
	b->is_signed = sign;
	b->indirect = a;
	a->type = b;

	struct type* r = calloc(1, sizeof(struct type));
	require(NULL != r, "Exhausted memory while declaring new primitive\n");
	r->name = name0;
	r->size = size;
	r->is_signed = sign;
	r->indirect = b;
	r->type = r;
	b->type = r;

	return r;
}

/* Initialize default types */
void initialize_types()
{
	if(AMD64 == Architecture || AARCH64 == Architecture || RISCV64 == Architecture) register_size = 8;
	else register_size = 4;

	/* Define void */
	struct type* hold = new_primitive("void", "void*", "void**", register_size, FALSE);
	prim_types = add_primitive(hold);

	/* Define unsigned LONG */
	hold = new_primitive("SCM","SCM*", "SCM**", register_size, FALSE);
	prim_types = add_primitive(hold);

	/* Define LONG */
	hold = new_primitive("long", "long*", "long**", register_size, TRUE);
	prim_types = add_primitive(hold);

	/* Define UNSIGNED */
	hold = new_primitive("unsigned", "unsigned*", "unsigned**", register_size, FALSE);
	prim_types = add_primitive(hold);

	/* Define int */
	integer = new_primitive("int", "int*", "int**", register_size, TRUE);
	prim_types = add_primitive(integer);

	/* Define uint32_t */
	hold = new_primitive("uint32_t", "uint32_t*", "uint32_t**", 4, FALSE);
	prim_types = add_primitive(hold);

	/* Define int32_t */
	hold = new_primitive("int32_t", "int32_t*", "int32_t**", 4, TRUE);
	prim_types = add_primitive(hold);

	/* Define uint16_t */
	hold = new_primitive("uint16_t", "uint16_t*", "uint16_t**", 2, FALSE);
	prim_types = add_primitive(hold);

	/* Define int16_t */
	hold = new_primitive("int16_t", "int16_t*", "int16_t**", 2, TRUE);
	prim_types = add_primitive(hold);

	/* Define uint8_t */
	hold = new_primitive("uint8_t", "uint8_t*", "uint8_t**", 1, FALSE);
	prim_types = add_primitive(hold);

	/* Define int8_t */
	hold = new_primitive("int8_t", "int8_t*", "int8_t**", 1, TRUE);
	prim_types = add_primitive(hold);

	/* Define char */
	hold = new_primitive("char", "char*", "char**", 1, TRUE);
	prim_types = add_primitive(hold);

	/* Define FUNCTION */
	hold = new_primitive("FUNCTION", "FUNCTION*", "FUNCTION**", register_size, FALSE);
	prim_types = add_primitive(hold);

	if(BOOTSTRAP_MODE)
	{
		/* Define FILE */
		hold = new_primitive("FILE", "FILE*", "FILE**", register_size, TRUE);
		prim_types = add_primitive(hold);

		/* Primitives mes.c wanted */
		hold = new_primitive("size_t", "size_t*", "size_t**", register_size, FALSE);
		prim_types = add_primitive(hold);

		hold = new_primitive("ssize_t", "ssize_t*", "ssize_t**", register_size, FALSE);
		prim_types = add_primitive(hold);
	}

	global_types = prim_types;
}

struct type* lookup_type(char* s, struct type* start)
{
	struct type* i;
	for(i = start; NULL != i; i = i->next)
	{
		if(match(i->name, s))
		{
			return i;
		}
	}
	return NULL;
}

struct type* lookup_member(struct type* parent, char* name)
{
	struct type* i;
	require(NULL != parent, "Not a valid struct type\n");
	for(i = parent->members; NULL != i; i = i->members)
	{
		if(match(i->name, name)) return i;
	}

	fputs("ERROR in lookup_member ", stderr);
	fputs(parent->name, stderr);
	fputs("->", stderr);
	fputs(global_token->s, stderr);
	fputs(" does not exist\n", stderr);
	line_error();
	fputs("\n", stderr);
	exit(EXIT_FAILURE);
}

struct type* type_name();
void require_match(char* message, char* required);

int member_size;
struct type* build_member(struct type* last, int offset)
{
	struct type* i = calloc(1, sizeof(struct type));
	require(NULL != i, "Exhausted memory while building a struct member\n");
	i->members = last;
	i->offset = offset;

	struct type* member_type = type_name();
	require(NULL != member_type, "struct member type can not be invalid\n");
	i->type = member_type;
	i->name = global_token->s;
	global_token = global_token->next;
	require(NULL != global_token, "struct member can not be EOF terminated\n");

	/* Check to see if array */
	if(match( "[", global_token->s))
	{
		global_token = global_token->next;
		require(NULL != global_token, "struct member arrays can not be EOF sized\n");
		i->size = member_type->type->size * strtoint(global_token->s);
		if(0 == i->size)
		{
			fputs("Struct only supports [num] form\n", stderr);
			exit(EXIT_FAILURE);
		}
		global_token = global_token->next;
		require_match("Struct only supports [num] form\n", "]");
	}
	else
	{
		i->size = member_type->size;
	}
	member_size = i->size;

	return i;
}

struct type* build_union(struct type* last, int offset)
{
	int size = 0;
	global_token = global_token->next;
	require_match("ERROR in build_union\nMissing {\n", "{");
	while('}' != global_token->s[0])
	{
		last = build_member(last, offset);
		if(member_size > size)
		{
			size = member_size;
		}
		require_match("ERROR in build_union\nMissing ;\n", ";");
		require(NULL != global_token, "Unterminated union\n");
	}
	member_size = size;
	global_token = global_token->next;
	return last;
}

void create_struct()
{
	int offset = 0;
	member_size = 0;
	struct type* head = calloc(1, sizeof(struct type));
	require(NULL != head, "Exhausted memory while creating a struct\n");
	struct type* i = calloc(1, sizeof(struct type));
	require(NULL != i, "Exhausted memory while creating a struct indirection\n");
	struct type* ii = calloc(1, sizeof(struct type));
	require(NULL != ii, "Exhausted memory while creating a struct double indirection\n");
	head->name = global_token->s;
	head->type = head;
	head->indirect = i;
	head->next = global_types;
	i->name = global_token->s;
	i->type = head;
	i->indirect = ii;
	i->size = register_size;
	ii->name = global_token->s;
	ii->type = i;
	ii->indirect = ii;
	ii->size = register_size;
	global_types = head;
	global_token = global_token->next;
	require_match("ERROR in create_struct\n Missing {\n", "{");
	struct type* last = NULL;
	require(NULL != global_token, "Incomplete struct definition at end of file\n");
	while('}' != global_token->s[0])
	{
		if(match(global_token->s, "union"))
		{
			last = build_union(last, offset);
		}
		else
		{
			last = build_member(last, offset);
		}
		offset = offset + member_size;
		require_match("ERROR in create_struct\n Missing ;\n", ";");
		require(NULL != global_token, "Unterminated struct\n");
	}

	global_token = global_token->next;
	require_match("ERROR in create_struct\n Missing ;\n", ";");

	head->size = offset;
	head->members = last;
	i->members = last;
}


struct type* type_name()
{
	struct type* ret;

	require(NULL != global_token, "Received EOF instead of type name\n");

	if(match("extern", global_token->s))
	{
		global_token = global_token->next;
		require(NULL != global_token, "unfinished type definition in extern\n");
	}

	if(match("struct", global_token->s))
	{
		global_token = global_token->next;
		require(NULL != global_token, "structs can not have a EOF type name\n");
		ret = lookup_type(global_token->s, global_types);
		if(NULL == ret)
		{
			create_struct();
			return NULL;
		}
	}
	else
	{
		ret = lookup_type(global_token->s, global_types);
		if(NULL == ret)
		{
			fputs("Unknown type ", stderr);
			fputs(global_token->s, stderr);
			fputs("\n", stderr);
			line_error();
			fputs("\n", stderr);
			exit(EXIT_FAILURE);
		}
	}

	global_token = global_token->next;
	require(NULL != global_token, "unfinished type definition\n");

	if(match("const", global_token->s))
	{
		global_token = global_token->next;
		require(NULL != global_token, "unfinished type definition in const\n");
	}

	while(global_token->s[0] == '*')
	{
		ret = ret->indirect;
		global_token = global_token->next;
		require(NULL != global_token, "unfinished type definition in indirection\n");
	}

	return ret;
}

struct type* mirror_type(struct type* source, char* name)
{
	struct type* head = calloc(1, sizeof(struct type));
	require(NULL != head, "Exhausted memory while creating a struct\n");
	struct type* i = calloc(1, sizeof(struct type));
	require(NULL != i, "Exhausted memory while creating a struct indirection\n");

	head->name = name;
	i->name = name;
	head->size = source->size;
	i->size = source->indirect->size;
	head->offset = source->offset;
	i->offset = source->indirect->offset;
	head->is_signed = source->is_signed;
	i->is_signed = source->indirect->is_signed;
	head->indirect = i;
	i->indirect = head;
	head->members = source->members;
	i->members =  source->indirect->members;
	head->type = head;
	i->type = i;

	return head;
}
