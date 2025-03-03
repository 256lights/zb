/* Copyright (C) 2016-2020 Jeremiah Orians
 * Copyright (C) 2020 fosslinux
 * This file is part of mescc-tools.
 *
 * mescc-tools is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * mescc-tools is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with mescc-tools.  If not, see <http://www.gnu.org/licenses/>.
 */

#include <stdio.h>
#include "../M2libc/bootstrappable.h"

/*
 * DEFINES
 */

#define FALSE 0
#define TRUE 1
// CONSTANT SUCCESS 0
#define SUCCESS 0
// CONSTANT FAILURE 1
#define FAILURE 1
#define MAX_STRING 4096
#define MAX_ARRAY 512


/*
 * Here is the token struct. It is used for both the token linked-list and
 * env linked-list.
 */
struct Token
{
	/*
	 * For the token linked-list, this stores the token; for the env linked-list
	 * this stores the value of the variable.
	 */
	char* value;
	/*
	 * Used only for the env linked-list. It holds a string containing the
	 * name of the var.
	 */
	char* var;
	/*
	 * This struct stores a node of a singly linked list, store the pointer to
	 * the next node.
	 */
	struct Token* next;
};

#include "kaem_globals.h"
