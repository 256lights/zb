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

/* What types we have */
extern struct type* global_types;
extern struct type* prim_types;

/* What we are currently working on */
extern struct token_list* global_token;

/* Output reorder collections*/
extern struct token_list* output_list;
extern struct token_list* strings_list;
extern struct token_list* globals_list;

/* Make our string collection more efficient */
extern char* hold_string;
extern int string_index;

/* Allow us to have a single settable max string */
extern long MAX_STRING;

/* enable preprocessor-only mode */
extern int PREPROCESSOR_MODE;

/* enable spawn behavior to be effective */
extern char* M2LIBC_PATH;
extern char* Architecture;
extern char* OperatingSystem;
extern int WORDSIZE;
extern int ENDIAN;
extern char* BASEADDRESS;
extern int STDIO_USED;
extern char* TEMPDIR;

/* So we don't shoot ourself in the face */
extern int FUZZING;
extern int DIRTY_MODE;
extern int DEBUG_LEVEL;
