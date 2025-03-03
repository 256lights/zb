/* -*- c-file-style: "linux";indent-tabs-mode:t -*- */
/* Copyright (C) 2017 Jeremiah Orians
 * Copyright (C) 2017 Jan Nieuwenhuizen <janneke@gnu.org>
 * This file is part of mescc-tools
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

#include "hex2.h"

/* Global variables */
extern FILE* output;
extern char* filename;
extern char* scratch;
extern int ALIGNED;
extern int Architecture;
extern int Base_Address;
extern int BigEndian;
extern int ByteMode;
extern int exec_enable;
extern int hold;
extern int ip;
extern int linenumber;
extern int toggle;
extern struct entry** jump_tables;

/* Function prototypes */
int Architectural_displacement(int target, int base);
int Throwaway_token(FILE* source_file);
int consume_token(FILE* source_file);
int storeLabel(FILE* source_file, int ip);
unsigned GetTarget(char* c);
void Clear_Scratch(char* s);
void line_error();
void outputPointer(int displacement, int number_of_bytes, int absolute);
void pad_to_align(int write);
int hex(int c, FILE* source_file);
int octal(int c, FILE* source_file);
int binary(int c, FILE* source_file);
