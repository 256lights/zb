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

#include "hex2_globals.h"

/* Globals */
FILE* output;
struct entry** jump_tables;
int BigEndian;
int Base_Address;
int Architecture;
int ByteMode;
int exec_enable;
int ip;
char* scratch;
char* filename;
int linenumber;
int ALIGNED;

/* For processing bytes */
int hold;
int toggle;

void line_error()
{
	fputs(filename, stderr);
	fputs(":", stderr);
	fputs(int2str(linenumber, 10, FALSE), stderr);
	fputs(" :", stderr);
}

int consume_token(FILE* source_file)
{
	int i = 0;
	int c = fgetc(source_file);
	while(!in_set(c, " \t\n>"))
	{
		scratch[i] = c;
		i = i + 1;
		c = fgetc(source_file);
		require(max_string > i, "Consumed token exceeds length restriction\n");
		if(EOF == c) break;
	}

	return c;
}

int Throwaway_token(FILE* source_file)
{
	int c;
	do
	{
		c = fgetc(source_file);
		if(EOF == c) break;
	} while(!in_set(c, " \t\n>"));

	return c;
}

int length(char* s)
{
	int i = 0;
	while(0 != s[i]) i = i + 1;
	return i;
}

void Clear_Scratch(char* s)
{
	do
	{
		s[0] = 0;
		s = s + 1;
	} while(0 != s[0]);
}

void Copy_String(char* a, char* b)
{
	while(0 != a[0])
	{
		b[0] = a[0];
		a = a + 1;
		b = b + 1;
	}
}

int GetHash(char* s)
{
	int i = 5381;
	while(0 != s[0])
	{
		i = i * 31 + s[0];
		s = s + 1;
	}
	return (i & 0xFFFF);
}

unsigned GetTarget(char* c)
{
	struct entry* i;
	for(i = jump_tables[GetHash(c)]; NULL != i; i = i->next)
	{
		if(match(c, i->name))
		{
			return i->target;
		}
	}
	fputs("Target label ", stderr);
	fputs(c, stderr);
	fputs(" is not valid\n", stderr);
	exit(EXIT_FAILURE);
}

int storeLabel(FILE* source_file, int ip)
{
	struct entry* entry = calloc(1, sizeof(struct entry));
	require(NULL != entry, "failed to allocate entry\n");

	/* Ensure we have target address */
	entry->target = ip;

	/* Store string */
	int c = consume_token(source_file);
	entry->name = calloc(length(scratch) + 1, sizeof(char));
	require(NULL != entry->name, "failed to allocate entry->name\n");
	Copy_String(scratch, entry->name);
	Clear_Scratch(scratch);

	/* Prepend to list */
	int h = GetHash(entry->name);
	entry->next = jump_tables[h];
	jump_tables[h] = entry;

	return c;
}

void range_check(int displacement, int number_of_bytes, int absolute)
{
	if(4 == number_of_bytes) return;
	else if (absolute && (3 == number_of_bytes))
	{
		/* Deal with unsigned */
		if((16777215 < displacement) || (displacement < 0))
		{
			fputs("An absolute displacement of ", stderr);
			fputs(int2str(displacement, 10, TRUE), stderr);
			fputs(" does not fit in 3 bytes\n", stderr);
			exit(EXIT_FAILURE);
		}
		return;
	}
	else if (3 == number_of_bytes)
	{
		/* Deal with signed */
		if((8388607 < displacement) || (displacement < -8388608))
		{
			fputs("A relative displacement of ", stderr);
			fputs(int2str(displacement, 10, TRUE), stderr);
			fputs(" does not fit in 3 bytes\n", stderr);
			exit(EXIT_FAILURE);
		}
		return;
	}
	else if (absolute && (2 == number_of_bytes))
	{
		/* Deal with unsigned */
		if((65535 < displacement) || (displacement < 0))
		{
			fputs("An absolute displacement of ", stderr);
			fputs(int2str(displacement, 10, TRUE), stderr);
			fputs(" does not fit in 2 bytes\n", stderr);
			exit(EXIT_FAILURE);
		}
		return;
	}
	else if (2 == number_of_bytes)
	{
		/* Deal with signed */
		if((32767 < displacement) || (displacement < -32768))
		{
			fputs("A relative displacement of ", stderr);
			fputs(int2str(displacement, 10, TRUE), stderr);
			fputs(" does not fit in 2 bytes\n", stderr);
			exit(EXIT_FAILURE);
		}
		return;
	}
	else if (absolute && (1 == number_of_bytes))
	{
		/* Deal with unsigned */
		if((255 < displacement) || (displacement < 0))
		{
			fputs("An absolute displacement of ", stderr);
			fputs(int2str(displacement, 10, TRUE), stderr);
			fputs(" does not fit in 1 byte\n", stderr);
			exit(EXIT_FAILURE);
		}
		return;
	}
	else if (1 == number_of_bytes)
	{
		/* Deal with signed */
		if((127 < displacement) || (displacement < -128))
		{
			fputs("A relative displacement of ", stderr);
			fputs(int2str(displacement, 10, TRUE), stderr);
			fputs(" does not fit in 1 byte\n", stderr);
			exit(EXIT_FAILURE);
		}
		return;
	}

	fputs("Invalid number of bytes given\n", stderr);
	exit(EXIT_FAILURE);
}

void outputPointer(int displacement, int number_of_bytes, int absolute)
{
	unsigned value = displacement;

	/* HALT HARD if we are going to do something BAD*/
	range_check(displacement, number_of_bytes, absolute);

	if(BigEndian)
	{ /* Deal with BigEndian */
		if(4 == number_of_bytes) fputc((value >> 24), output);
		if(3 <= number_of_bytes) fputc(((value >> 16)%256), output);
		if(2 <= number_of_bytes) fputc(((value >> 8)%256), output);
		if(1 <= number_of_bytes) fputc((value % 256), output);
	}
	else
	{ /* Deal with LittleEndian */
		unsigned byte;
		while(number_of_bytes > 0)
		{
			byte = value % 256;
			value = value / 256;
			fputc(byte, output);
			number_of_bytes = number_of_bytes - 1;
		}
	}
}

int Architectural_displacement(int target, int base)
{
	if(KNIGHT == Architecture) return (target - base);
	else if(X86 == Architecture) return (target - base);
	else if(AMD64 == Architecture) return (target - base);
	else if(ALIGNED && (ARMV7L == Architecture))
	{
		ALIGNED = FALSE;
		/* Note: Branch displacements on ARM are in number of instructions to skip, basically. */
		if (target & 3)
		{
			line_error();
			fputs("error: Unaligned branch target: ", stderr);
			fputs(scratch, stderr);
			fputs(", aborting\n", stderr);
			exit(EXIT_FAILURE);
		}
		/*
		 * The "fetch" stage already moved forward by 8 from the
		 * beginning of the instruction because it is already
		 * prefetching the next instruction.
		 * Compensate for it by subtracting the space for
		 * two instructions (including the branch instruction).
		 * and the size of the aligned immediate.
		 */
		return (((target - base + (base & 3)) >> 2) - 2);
	}
	else if(ARMV7L == Architecture)
	{
		/*
		 * The size of the offset is 8 according to the spec but that value is
		 * based on the end of the immediate, which the documentation gets wrong
		 * and needs to be adjusted to the size of the immediate.
		 * Eg 1byte immediate => -8 + 1 = -7
		 */
		return ((target - base) - 8 + (3 & base));
	}
	else if(ALIGNED && (AARM64 == Architecture))
	{
			ALIGNED = FALSE;
			return (target - (~3 & base)) >> 2;
	}
	else if (AARM64 == Architecture)
	{
		return ((target - base) - 8 + (3 & base));
	}
	else if(ALIGNED && (PPC64LE == Architecture))
	{
		ALIGNED = FALSE;
		/* set Link register with branch */
		return (target - (base & 0xFFFFFFFC )) | 1;
	}
	else if(PPC64LE == Architecture)
	{
		/* DO *NOT* set link register with branch */
		return (target - (base & 0xFFFFFFFC));
	}
	else if(RISCV32 == Architecture || RISCV64 == Architecture) return (target - base);

	fputs("Unknown Architecture, aborting before harm is done\n", stderr);
	exit(EXIT_FAILURE);
}

void Update_Pointer(char ch)
{
	/* Calculate pointer size*/
	if(in_set(ch, "%&")) ip = ip + 4; /* Deal with % and & */
	else if(in_set(ch, "@$")) ip = ip + 2; /* Deal with @ and $ */
	else if('~' == ch) ip = ip + 3; /* Deal with ~ */
	else if('!' == ch) ip = ip + 1; /* Deal with ! */
	else
	{
		line_error();
		fputs("storePointer given unknown\n", stderr);
		exit(EXIT_FAILURE);
	}
}

void storePointer(char ch, FILE* source_file)
{
	/* Get string of pointer */
	Clear_Scratch(scratch);
	Update_Pointer(ch);
	int base_sep_p = consume_token(source_file);

	/* Lookup token */
	int target = GetTarget(scratch);
	int displacement;

	int base = ip;

	/* Change relative base address to :<base> */
	if ('>' == base_sep_p)
	{
		Clear_Scratch(scratch);
		consume_token (source_file);
		base = GetTarget (scratch);

		/* Force universality of behavior */
		displacement = (target - base);
	}
	else
	{
		displacement = Architectural_displacement(target, base);
	}

	/* output calculated difference */
	if('!' == ch) outputPointer(displacement, 1, FALSE); /* Deal with ! */
	else if('$' == ch) outputPointer(target, 2, TRUE); /* Deal with $ */
	else if('@' == ch) outputPointer(displacement, 2, FALSE); /* Deal with @ */
	else if('~' == ch) outputPointer(displacement, 3, FALSE); /* Deal with ~ */
	else if('&' == ch) outputPointer(target, 4, TRUE); /* Deal with & */
	else if('%' == ch) outputPointer(displacement, 4, FALSE);  /* Deal with % */
	else
	{
		line_error();
		fputs("error: storePointer reached impossible case: ch=", stderr);
		fputc(ch, stderr);
		fputs("\n", stderr);
		exit(EXIT_FAILURE);
	}
}

void line_Comment(FILE* source_file)
{
	int c = fgetc(source_file);
	while(!in_set(c, "\n\r"))
	{
		if(EOF == c) break;
		c = fgetc(source_file);
	}
	linenumber = linenumber + 1;
}

int hex(int c, FILE* source_file)
{
	if (in_set(c, "0123456789")) return (c - 48);
	else if (in_set(c, "abcdef")) return (c - 87);
	else if (in_set(c, "ABCDEF")) return (c - 55);
	else if (in_set(c, "#;")) line_Comment(source_file);
	else if ('\n' == c) linenumber = linenumber + 1;
	return -1;
}

int octal(int c, FILE* source_file)
{
	if (in_set(c, "01234567")) return (c - 48);
	else if (in_set(c, "#;")) line_Comment(source_file);
	else if ('\n' == c) linenumber = linenumber + 1;
	return -1;
}

int binary(int c, FILE* source_file)
{
	if (in_set(c, "01")) return (c - 48);
	else if (in_set(c, "#;")) line_Comment(source_file);
	else if ('\n' == c) linenumber = linenumber + 1;
	return -1;
}

void process_byte(char c, FILE* source_file, int write)
{
	if(HEX == ByteMode)
	{
		if(0 <= hex(c, source_file))
		{
			if(toggle)
			{
				if(write) fputc(((hold * 16)) + hex(c, source_file), output);
				ip = ip + 1;
				hold = 0;
			}
			else
			{
				hold = hex(c, source_file);
			}
			toggle = !toggle;
		}
	}
	else if(OCTAL ==ByteMode)
	{
		if(0 <= octal(c, source_file))
		{
			if(2 == toggle)
			{
				if(write) fputc(((hold * 8)) + octal(c, source_file), output);
				ip = ip + 1;
				hold = 0;
				toggle = 0;
			}
			else if(1 == toggle)
			{
				hold = ((hold * 8) + octal(c, source_file));
				toggle = 2;
			}
			else
			{
				hold = octal(c, source_file);
				toggle = 1;
			}
		}
	}
	else if(BINARY == ByteMode)
	{
		if(0 <= binary(c, source_file))
		{
			if(7 == toggle)
			{
				if(write) fputc((hold * 2) + binary(c, source_file), output);
				ip = ip + 1;
				hold = 0;
				toggle = 0;
			}
			else
			{
				hold = ((hold * 2) + binary(c, source_file));
				toggle = toggle + 1;
			}
		}
	}
}

void pad_to_align(int write)
{
	if((ARMV7L == Architecture) || (AARM64 == Architecture) || (RISCV32 == Architecture) || (RISCV64 == Architecture))
	{
		if(1 == (ip & 0x1))
		{
			ip = ip + 1;
			if(write) fputc('\0', output);
		}
		if(2 == (ip & 0x2))
		{
			ip = ip + 2;
			if(write)
			{
				fputc('\0', output);
				fputc('\0', output);
			}
		}
	}
}

void first_pass(struct input_files* input)
{
	if(NULL == input) return;
	first_pass(input->next);
	filename = input->filename;
	linenumber = 1;
	FILE* source_file = fopen(filename, "r");

	if(NULL == source_file)
	{
		fputs("The file: ", stderr);
		fputs(input->filename, stderr);
		fputs(" can not be opened!\n", stderr);
		exit(EXIT_FAILURE);
	}

	toggle = FALSE;
	int c;
	for(c = fgetc(source_file); EOF != c; c = fgetc(source_file))
	{
		/* Check for and deal with label */
		if(':' == c)
		{
			c = storeLabel(source_file, ip);
		}

		/* check for and deal with relative/absolute pointers to labels */
		if(in_set(c, "!@$~%&"))
		{ /* deal with 1byte pointer !; 2byte pointers (@ and $); 3byte pointers ~; 4byte pointers (% and &) */
			Update_Pointer(c);
			c = Throwaway_token(source_file);
			if ('>' == c)
			{ /* deal with label>base */
				c = Throwaway_token(source_file);
			}
		}
		else if('<' == c)
		{
			pad_to_align(FALSE);
		}
		else if('^' == c)
		{
			/* Just ignore */
			continue;
		}
		else process_byte(c, source_file, FALSE);
	}
	fclose(source_file);
}

void second_pass(struct input_files* input)
{
	if(NULL == input) return;
	second_pass(input->next);
	filename = input->filename;
	linenumber = 1;
	FILE* source_file = fopen(filename, "r");

	/* Something that should never happen */
	if(NULL == source_file)
	{
		fputs("The file: ", stderr);
		fputs(input->filename, stderr);
		fputs(" can not be opened!\nWTF-pass2\n", stderr);
		exit(EXIT_FAILURE);
	}

	toggle = FALSE;
	hold = 0;

	int c;
	for(c = fgetc(source_file); EOF != c; c = fgetc(source_file))
	{
		if(':' == c) c = Throwaway_token(source_file); /* Deal with : */
		else if(in_set(c, "!@$~%&")) storePointer(c, source_file);  /* Deal with !, @, $, ~, % and & */
		else if('<' == c) pad_to_align(TRUE);
		else if('^' == c) ALIGNED = TRUE;
		else process_byte(c, source_file, TRUE);
	}

	fclose(source_file);
}
