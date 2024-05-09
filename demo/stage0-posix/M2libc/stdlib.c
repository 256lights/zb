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

#include <unistd.h>
#include <sys/stat.h>
#include <fcntl.h>

#define EXIT_FAILURE 1
#define EXIT_SUCCESS 0

#define _IN_USE 1
#define _NOT_IN_USE 0

typedef char wchar_t;

void exit(int value);

struct _malloc_node
{
	struct _malloc_node *next;
	void* block;
	size_t size;
	int used;
};

struct _malloc_node* _allocated_list;
struct _malloc_node* _free_list;

/********************************
 * The core POSIX malloc        *
 ********************************/
long _malloc_ptr;
long _brk_ptr;
void* _malloc_brk(unsigned size)
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

void __init_malloc()
{
	_free_list = NULL;
	_allocated_list = NULL;
	return;
}

/************************************************************************
 * Handle with the tricky insert behaviors for our nodes                *
 * As free lists must be sorted from smallest to biggest to enable      *
 * cheap first fit logic                                                *
 * The free function however is rarely called, so it can kick sand and  *
 * do things the hard way                                               *
 ************************************************************************/
void _malloc_insert_block(struct _malloc_node* n, int used)
{
	/* Allocated block doesn't care about order */
	if(_IN_USE == used)
	{
		/* Literally just be done as fast as possible */
		n->next = _allocated_list;
		_allocated_list = n;
		return;
	}

	/* sanity check garbage */
	if(_NOT_IN_USE != used) exit(EXIT_FAILURE);
	if(_NOT_IN_USE != n->used) exit(EXIT_FAILURE);
	if(NULL != n->next) exit(EXIT_FAILURE);

	/* Free block really does care about order */
	struct _malloc_node* i = _free_list;
	struct _malloc_node* last = NULL;
	while(NULL != i)
	{
		/* sort smallest to largest */
		if(n->size <= i->size)
		{
			/* Connect */
			n->next = i;
			/* If smallest yet */
			if(NULL == last) _free_list = n;
			/* or just another average block */
			else last->next = n;
			return;
		}

		/* iterate */
		last = i;
		i = i->next;
	}

	/* looks like we are the only one */
	if(NULL == last) _free_list = n;
	/* or we are the biggest yet */
	else last->next = n;
}

/************************************************************************
 * We only mark a block as unused, we don't actually deallocate it here *
 * But rather shove it into our _free_list                              *
 ************************************************************************/
void free(void* ptr)
{
/* just in case someone needs to quickly turn it off */
#ifndef _MALLOC_DISABLE_FREE
	struct _malloc_node* i = _allocated_list;
	struct _malloc_node* last = NULL;

	/* walk the whole freaking list if needed to do so */
	while(NULL != i)
	{
		/* did we find it? */
		if(i->block == ptr)
		{
			/* detach the block */
			if(NULL == last) _allocated_list = i->next;
			/* in a way that doesn't break the allocated list */
			else last->next = i->next;

			/* insert into free'd list */
			i->used = _NOT_IN_USE;
			i->next = NULL;
			_malloc_insert_block(i, _NOT_IN_USE);
			return;
		}

		/* iterate */
		last = i;
		i = i->next;
	}

	/* we received a pointer to a block that wasn't allocated */
	/* Bail *HARD* because I don't want to cover this edge case */
	exit(EXIT_FAILURE);
#endif
	/* if free is disabled, there is nothing to do */
	return;
}

/************************************************************************
 * find if there is any "FREED" blocks big enough to sit on our memory  *
 * budget's face and ruin its life. Respectfully of course              *
 ************************************************************************/
void* _malloc_find_free(unsigned size)
{
	struct _malloc_node* i = _free_list;
	struct _malloc_node* last = NULL;
	/* Walk the whole list if need be */
	while(NULL != i)
	{
		/* see if anything in it is equal or bigger than what I need */
		if((_NOT_IN_USE == i->used) && (i->size > size))
		{
			/* disconnect from list ensuring we don't break free doing so */
			if(NULL == last) _free_list = i->next;
			else last->next = i->next;

			/* insert into allocated list */
			i->used = _IN_USE;
			i->next = NULL;
			_malloc_insert_block(i, _IN_USE);
			return i->block;
		}

		/* iterate (will loop forever if you get this wrong) */
		last = i;
		i = i->next;
	}

	/* Couldn't find anything big enough */
	return NULL;
}

/************************************************************************
 * Well we couldn't find any memory good enough to satisfy our needs so *
 * we are going to have to go beg for some memory on the street corner  *
 ************************************************************************/
void* _malloc_add_new(unsigned size)
{
	struct _malloc_node* n;
#ifdef __uefi__
	n = _malloc_uefi(sizeof(struct _malloc_node));
	/* Check if we were beaten */
	if(NULL == n) return NULL;
	n->block = _malloc_uefi(size);
#else
	n = _malloc_brk(sizeof(struct _malloc_node));
	/* Check if we were beaten */
	if(NULL == n) return NULL;
	n->block = _malloc_brk(size);
#endif
	/* check if we were robbed */
	if(NULL == n->block) return NULL;

	/* Looks like we made it home safely */
	n->size = size;
	n->next = NULL;
	n->used = _IN_USE;
	/* lets pop the cork and party */
	_malloc_insert_block(n, _IN_USE);
	return n->block;
}

/************************************************************************
 * Safely iterates over all malloc nodes and frees them                 *
 ************************************************************************/
void __malloc_node_iter(struct _malloc_node* node, FUNCTION _free)
{
	struct _malloc_node* current;
	while(node != NULL)
	{
		current = node;
		node = node->next;
		_free(current->block);
		_free(current);
	}
}

/************************************************************************
 * Runs a callback with all previously allocated nodes.                 *
 * This can be useful if operating system does not do any clean up.     *
 ************************************************************************/
void* _malloc_release_all(FUNCTION _free)
{
	__malloc_node_iter(_allocated_list, _free);
	__malloc_node_iter(_free_list, _free);
}

/************************************************************************
 * Provide a POSIX standardish malloc function to keep things working   *
 ************************************************************************/
void* malloc(unsigned size)
{
	/* skip allocating nothing */
	if(0 == size) return NULL;

	/* use one of the standard block sizes */
	size_t max = 1 << 30;
	size_t used = 256;
	while(used < size)
	{
		used = used << 1;

		/* fail big allocations */
		if(used > max) return NULL;
	}

	/* try the cabinets around the house */
	void* ptr = _malloc_find_free(used);

	/* looks like we need to get some more from the street corner */
	if(NULL == ptr)
	{
		ptr = _malloc_add_new(used);
	}

	/* hopefully you can handle NULL pointers, good luck */
	return ptr;
}

/************************************************************************
 * Provide a POSIX standardish memset function to keep things working   *
 ************************************************************************/
void* memset(void* ptr, int value, int num)
{
	char* s;
	/* basically walk the block 1 byte at a time and set it to any value you want */
	for(s = ptr; 0 < num; num = num - 1)
	{
		s[0] = value;
		s = s + 1;
	}

	return ptr;
}

/************************************************************************
 * Provide a POSIX standardish calloc function to keep things working   *
 ************************************************************************/
void* calloc(int count, int size)
{
	/* if things get allocated, we are good*/
	void* ret = malloc(count * size);
	/* otherwise good luck */
	if(NULL == ret) return NULL;
	memset(ret, 0, (count * size));
	return ret;
}


/* USED EXCLUSIVELY BY MKSTEMP */
void __set_name(char* s, int i)
{
	s[5] = '0' + (i % 10);
	i = i / 10;
	s[4] = '0' + (i % 10);
	i = i / 10;
	s[3] = '0' + (i % 10);
	i = i / 10;
	s[2] = '0' + (i % 10);
	i = i / 10;
	s[1] = '0' + (i % 10);
	i = i / 10;
	s[0] = '0' + i;
}

/************************************************************************
 * Provide a POSIX standardish mkstemp function to keep things working  *
 ************************************************************************/
int mkstemp(char *template)
{
	/* get length of template */
	int i = 0;
	while(0 != template[i]) i = i + 1;
	i = i - 1;

	/* String MUST be more than 6 characters in length */
	if(i < 6) return -1;

	/* Sanity check the string matches the template requirements */
	int count = 6;
	int c;
	while(count > 0)
	{
		c = template[i];
		/* last 6 chars must be X */
		if('X' != c) return -1;
		template[i] = '0';
		i = i - 1;
		count = count - 1;
	}

	int fd = -1;
	count = -1;
	/* open will return -17 or other values */
	while(0 > fd)
	{
		/* Just give up after the planet has blown up */
		if(9000 < count) return -1;

		/* Try up to 9000 unique filenames before stopping */
		count = count + 1;
		__set_name(template+i+1, count);

		/* Pray we can */
		fd = open(template, O_RDWR | O_CREAT | O_EXCL, 00600);
	}

	/* well that only took count many tries */
	return fd;
}

/************************************************************************
 * wcstombs - convert a wide-character string to a multibyte string     *
 * because seriously UEFI??? UTF-16 is a bad design choice but I guess  *
 * they were drinking pretty hard when they designed UEFI; it is DOS    *
 * but somehow they magically found ways of making it worse             *
 ************************************************************************/
size_t wcstombs(char* dest, char* src, size_t n)
{
	int i = 0;

	do
	{
		/* UTF-16 is 2bytes per char and that first byte maps good enough to ASCII */
		dest[i] = src[2 * i];
		if(dest[i] == 0)
		{
			break;
		}
		i = i + 1;
		n = n - 1;
	} while (n != 0);

	return i;
}

/************************************************************************
 * getenv - get an environmental variable                               *
 ************************************************************************/
size_t _strlen(char const* str)
{
	size_t i = 0;
	while(0 != str[i]) i = i + 1;
	return i;
}
int _strncmp(char const* lhs, char const* rhs, size_t count)
{
	size_t i = 0;
	while(count > i)
	{
		if(0 == lhs[i]) break;
		if(lhs[i] != rhs[i]) return lhs[i] - rhs[i];
		i = i + 1;
	}

	return 0;
}
char** _envp;
char* getenv (char const* name)
{
	char** p = _envp;
	char* q;
	int length = _strlen(name);

	while (p[0] != 0)
	{
		if(_strncmp(name, p[0], length) == 0)
		{
			q = p[0] + length;
			if(q[0] == '=')
				return q + 1;
		}
		p += sizeof(char**); /* M2 pointer arithemtic */
	}

	return 0;
}

/************************************************************************
 * setenv - set an environmental variable                               *
 ************************************************************************/
char* _strcpy(char* dest, char const* src)
{
	int i = 0;

	while (0 != src[i])
	{
		dest[i] = src[i];
		i = i + 1;
	}
	dest[i] = 0;

	return dest;
}

int setenv(char const *s, char const *v, int overwrite_p)
{
	char** p = _envp;
	int length = _strlen(s);
	char* q;

	while (p[0] != 0)
	{
		if (_strncmp (s, p[0], length) == 0)
		{
			q = p[0] + length;
			if (q[0] == '=')
				break;
		}
		p += sizeof(char**); /* M2 pointer arithemtic */
	}
	char *entry = malloc (length + _strlen(v) + 2);
	int end_p = p[0] == 0;
	p[0] = entry;
	_strcpy(entry, s);
	_strcpy(entry + length, "=");
	_strcpy(entry + length + 1, v);
	entry[length + _strlen(v) + 2] = 0;
	if (end_p != 0)
		p[1] = 0;

	return 0;
}
