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

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/stat.h>
#include "M2libc/bootstrappable.h"

#define max_string 4096
#define TRUE 1
#define FALSE 0

#define KNIGHT 0
#define X86 0x03
#define AMD64 0x3E
#define ARMV7L 0x28
#define AARM64 0xB7
#define PPC64LE 0x15
#define RISCV32 0xF3
#define RISCV64 0x100F3 /* Because RISC-V unlike all other architectures does get a seperate e_machine when changing from 32 to 64bit */

#define HEX 16
#define OCTAL 8
#define BINARY 2


struct input_files
{
	struct input_files* next;
	char* filename;
};

struct entry
{
	struct entry* next;
	unsigned target;
	char* name;
};

