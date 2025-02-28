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

#include "kaem.h"

int command_done;
int VERBOSE;
int VERBOSE_EXIT;
int STRICT;
int INIT_MODE;
int FUZZING;
int WARNINGS;
char* KAEM_BINARY;
char* PATH;

/* Token linked-list; stores the tokens of each line */
struct Token* token;
/* Env linked-list; stores the environment variables */
struct Token* env;
/* Alias linked-list; stores the aliases */
struct Token* alias;
