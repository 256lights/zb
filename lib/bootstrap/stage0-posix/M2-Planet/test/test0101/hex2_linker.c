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

#define max_string 4096
//CONSTANT max_string 4096
#define TRUE 1
//CONSTANT TRUE 1
#define FALSE 0
//CONSTANT FALSE 0

int match(char* a, char* b);
char* int2str(int x, int base, int signed_p);
int strtoint(char *a);
int in_set(int c, char* s);

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

FILE* output;
struct entry* jump_table;
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

void line_error()
{
	fputs(filename, stderr);
	fputs(":", stderr);
	fputs(int2str(linenumber, 10, TRUE), stderr);
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
	}

	return c;
}

int Throwaway_token(FILE* source_file)
{
	int c;
	do
	{
		c = fgetc(source_file);
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

unsigned GetTarget(char* c)
{
	struct entry* i;
	for(i = jump_table; NULL != i; i = i->next)
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

	/* Ensure we have target address */
	entry->target = ip;

	/* Prepend to list */
	entry->next = jump_table;
	jump_table = entry;

	/* Store string */
	int c = consume_token(source_file);
	entry->name = calloc(length(scratch) + 1, sizeof(char));
	Copy_String(scratch, entry->name);
	Clear_Scratch(scratch);

	return c;
}

void range_check(int displacement, int number_of_bytes)
{
	if(4 == number_of_bytes) return;
	else if (3 == number_of_bytes)
	{
		if((8388607 < displacement) || (displacement < -8388608))
		{
			fputs("A displacement of ", stderr);
			fputs(int2str(displacement, 10, TRUE), stderr);
			fputs(" does not fit in 3 bytes\n", stderr);
			exit(EXIT_FAILURE);
		}
		return;
	}
	else if (2 == number_of_bytes)
	{
		if((32767 < displacement) || (displacement < -32768))
		{
			fputs("A displacement of ", stderr);
			fputs(int2str(displacement, 10, TRUE), stderr);
			fputs(" does not fit in 2 bytes\n", stderr);
			exit(EXIT_FAILURE);
		}
		return;
	}
	else if (1 == number_of_bytes)
	{
		if((127 < displacement) || (displacement < -128))
		{
			fputs("A displacement of ", stderr);
			fputs(int2str(displacement, 10, TRUE), stderr);
			fputs(" does not fit in 1 byte\n", stderr);
			exit(EXIT_FAILURE);
		}
		return;
	}

	fputs("Invalid number of bytes given\n", stderr);
	exit(EXIT_FAILURE);
}

void outputPointer(int displacement, int number_of_bytes)
{
	unsigned value = displacement;

	/* HALT HARD if we are going to do something BAD*/
	range_check(displacement, number_of_bytes);

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
	if(0 == Architecture) return (target - base);
	else if(1 == Architecture) return (target - base);
	else if(2 == Architecture) return (target - base);
	else if(ALIGNED && (40 == Architecture))
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
	else if(40 == Architecture)
	{
		/*
		 * The size of the offset is 8 according to the spec but that value is
		 * based on the end of the immediate, which the documentation gets wrong
		 * and needs to be adjusted to the size of the immediate.
		 * Eg 1byte immediate => -8 + 1 = -7
		 */
		return ((target - base) - 8 + (3 & base));
	}

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
	if('!' == ch) outputPointer(displacement, 1); /* Deal with ! */
	else if('$' == ch) outputPointer(target, 2); /* Deal with $ */
	else if('@' == ch) outputPointer(displacement, 2); /* Deal with @ */
	else if('~' == ch) outputPointer(displacement, 3); /* Deal with ~ */
	else if('&' == ch) outputPointer(target, 4); /* Deal with & */
	else if('%' == ch) outputPointer(displacement, 4);  /* Deal with % */
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


int hold;
int toggle;
void process_byte(char c, FILE* source_file, int write)
{
	if(16 == ByteMode)
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
	else if(8 ==ByteMode)
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
	else if(2 == ByteMode)
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
	if(40 == Architecture)
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

/* Standard C main program */
int main(int argc, char **argv)
{
	ALIGNED = FALSE;
	BigEndian = TRUE;
	jump_table = NULL;
	Architecture = 0;
	Base_Address = 0;
	struct input_files* input = NULL;
	output = stdout;
	char* output_file = "";
	exec_enable = FALSE;
	ByteMode = 16;
	scratch = calloc(max_string + 1, sizeof(char));
	char* arch;
	struct input_files* temp;

	int option_index = 1;
	while(option_index <= argc)
	{
		if(NULL == argv[option_index])
		{
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "--BigEndian"))
		{
			BigEndian = TRUE;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "--LittleEndian"))
		{
			BigEndian = FALSE;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "--exec_enable"))
		{
			exec_enable = TRUE;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "-A") || match(argv[option_index], "--architecture"))
		{
			arch = argv[option_index + 1];
			if(match("knight-native", arch) || match("knight-posix", arch)) Architecture = 0;
			else if(match("x86", arch)) Architecture = 1;
			else if(match("amd64", arch)) Architecture = 2;
			else if(match("armv7l", arch)) Architecture = 40;
			else
			{
				fputs("Unknown architecture: ", stderr);
				fputs(arch, stderr);
				fputs(" know values are: knight-native, knight-posix, x86, amd64 and armv7l", stderr);
			}
			option_index = option_index + 2;
		}
		else if(match(argv[option_index], "-b") || match(argv[option_index], "--binary"))
		{
			ByteMode = 2;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "-B") || match(argv[option_index], "--BaseAddress"))
		{
			Base_Address = strtoint(argv[option_index + 1]);
			option_index = option_index + 2;
		}
		else if(match(argv[option_index], "-h") || match(argv[option_index], "--help"))
		{
			fputs("Usage: ", stderr);
			fputs(argv[0], stderr);
			fputs(" -f FILENAME1 {-f FILENAME2} (--BigEndian|--LittleEndian)", stderr);
			fputs(" [--BaseAddress 12345] [--architecture name]\nArchitecture", stderr);
			fputs(" knight-native, knight-posix, x86, amd64 and armv7\n", stderr);
			fputs("To leverage octal or binary input: --octal, --binary\n", stderr);
			exit(EXIT_SUCCESS);
		}
		else if(match(argv[option_index], "-f") || match(argv[option_index], "--file"))
		{
			temp = calloc(1, sizeof(struct input_files));
			temp->filename = argv[option_index + 1];
			temp->next = input;
			input = temp;
			option_index = option_index + 2;
		}
		else if(match(argv[option_index], "-o") || match(argv[option_index], "--output"))
		{
			output_file = argv[option_index + 1];
			output = fopen(output_file, "w");

			if(NULL == output)
			{
				fputs("The file: ", stderr);
				fputs(argv[option_index + 1], stderr);
				fputs(" can not be opened!\n", stderr);
				exit(EXIT_FAILURE);
			}
			option_index = option_index + 2;
		}
		else if(match(argv[option_index], "-O") || match(argv[option_index], "--octal"))
		{
			ByteMode = 8;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "-V") || match(argv[option_index], "--version"))
		{
			fputs("hex2 0.3\n", stdout);
			exit(EXIT_SUCCESS);
		}
		else
		{
			fputs("Unknown option\n", stderr);
			exit(EXIT_FAILURE);
		}
	}

	/* Make sure we have a program tape to run */
	if (NULL == input)
	{
		return EXIT_FAILURE;
	}

	/* Get all of the labels */
	ip = Base_Address;
	first_pass(input);

	/* Fix all the references*/
	ip = Base_Address;
	second_pass(input);

	/* Close output file */
	if (output != stdout) {
		fclose(output);
	}

	/* Set file as executable */
	if(exec_enable)
	{
		/* 488 = 750 in octal */
		if(0 != chmod(output_file, 488))
		{
			fputs("Unable to change permissions\n", stderr);
			exit(EXIT_FAILURE);
		}
	}

	return EXIT_SUCCESS;
}
