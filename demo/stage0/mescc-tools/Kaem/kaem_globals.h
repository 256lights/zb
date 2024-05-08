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

extern int command_done;
extern int VERBOSE;
extern int VERBOSE_EXIT;
extern int STRICT;
extern int INIT_MODE;
extern int FUZZING;
extern int WARNINGS;
extern char* KAEM_BINARY;
extern char* PATH;

/* Token linked-list; stores the tokens of each line */
extern struct Token* token;
/* Env linked-list; stores the environment variables */
extern struct Token* env;
/* Alias linked-list; stores the aliases */
extern struct Token* alias;
