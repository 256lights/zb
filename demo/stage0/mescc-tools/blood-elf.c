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

// CONSTANT max_string 4096
#define max_string 4096
int BITSIZE;
int BigEndian;
// CONSTANT HEX 16
#define HEX 16
// CONSTANT OCTAL 8
#define OCTAL 8
// CONSTANT BINARY 2
#define BINARY 2


/* Strings needed for constants */
char* zero_8;
char* zero_16;
char* zero_32;
char* one_16;
char* one_32;
char* two_8;
char* two_32;
char* three_32;
char* six_32;
char* sixteen_32;
char* twentyfour_32;

/* Imported from stringify.c */
int stringify(char* s, int digits, int divisor, int value, int shift);
void LittleEndian(char* start, int ByteMode);

struct entry
{
	struct entry* next;
	char* name;
};

FILE* output;
struct entry* jump_table;
int count;
char* entry;

void consume_token(FILE* source_file, char* s)
{
	int i = 0;
	int c = fgetc(source_file);
	require(EOF != c, "Can not have an EOF token\n");
	do
	{
		s[i] = c;
		i = i + 1;
		require(max_string > i, "Token exceeds token length restriction\n");
		c = fgetc(source_file);
		if(EOF == c) break;
	} while(!in_set(c, " \t\n>"));
}

void storeLabel(FILE* source_file)
{
	struct entry* entry = calloc(1, sizeof(struct entry));

	/* Prepend to list */
	entry->next = jump_table;
	jump_table = entry;

	/* Store string */
	entry->name = calloc((max_string + 1), sizeof(char));
	consume_token(source_file, entry->name);

	count = count + 1;
}

void line_Comment(FILE* source_file)
{
	int c = fgetc(source_file);
	while(!in_set(c, "\n\r"))
	{
		if(EOF == c) break;
		c = fgetc(source_file);
	}
}

void purge_string(FILE* source_file)
{
	int c = fgetc(source_file);
	while((EOF != c) && ('"' != c))
	{
		c = fgetc(source_file);
	}
}

void first_pass(struct entry* input)
{
	if(NULL == input) return;
	first_pass(input->next);

	FILE* source_file = fopen(input->name, "r");

	if(NULL == source_file)
	{
		fputs("The file: ", stderr);
		fputs(input->name, stderr);
		fputs(" can not be opened!\n", stderr);
		exit(EXIT_FAILURE);
	}

	int c;
	for(c = fgetc(source_file); EOF != c; c = fgetc(source_file))
	{
		/* Check for and deal with label */
		if(58 == c)
		{
			storeLabel(source_file);
		}
		/* Check for and deal with line comments */
		else if (c == '#' || c == ';')
		{
			line_Comment(source_file);
		}
		else if ('"' == c)
		{
			purge_string(source_file);
		}
	}
	fclose(source_file);
}

void output_string_table(struct entry* node)
{
	fputs("\n# Generated string table\n:ELF_str\n", output);
	fputs(zero_8, output);
	fputs("\t# NULL string\n", output);
	struct entry* i;
	for(i = node; NULL != i; i = i->next)
	{
		fputs(":ELF_str_", output);
		fputs(i->name, output);
		fputs("\t\"", output);
		fputs(i->name, output);
		fputs("\"\n", output);
	}
	fputs("# END Generated string table\n\n", output);
}

void output_symbol_table(struct entry* node)
{
	fputs("\n# Generated symbol table\n:ELF_sym\n# Required NULL symbol entry\n", output);
	if(64 == BITSIZE)
	{
		fputs(zero_32, output);
		fputs("\t# st_name\n", output);

		fputs(zero_8, output);
		fputs("\t# st_info\n", output);

		fputs(zero_8, output);
		fputs("\t# st_other\n", output);

		fputs(one_16, output);
		fputs("\t# st_shndx\n", output);

		fputs(zero_32, output);
		fputc(' ', output);
		fputs(zero_32, output);
		fputs("\t# st_value\n", output);

		fputs(zero_32, output);
		fputc(' ', output);
		fputs(zero_32, output);
		fputs("\t# st_size\n\n", output);
	}
	else
	{
		fputs(zero_32, output);
		fputs("\t# st_name\n", output);

		fputs(zero_32, output);
		fputs("\t# st_value\n", output);

		fputs(zero_32, output);
		fputs("\t# st_size\n", output);

		fputs(zero_8, output);
		fputs("\t# st_info\n", output);

		fputs(zero_8, output);
		fputs("\t# st_other\n", output);

		fputs(one_16, output);
		fputs("\t# st_shndx\n\n", output);
	}

	struct entry* i;
	for(i = node; NULL != i; i = i->next)
	{
		fputs("%ELF_str_", output);
		fputs(i->name, output);
		fputs(">ELF_str\t# st_name\n", output);

		if(64 == BITSIZE)
		{
			fputs(two_8, output);
			fputs("\t# st_info (FUNC)\n", output);

			if(('_' == i->name[0]) && !match(entry, i->name))
			{
				fputs(two_8, output);
				fputs("\t# st_other (hidden)\n", output);
			}
			else
			{
				fputs(zero_8, output);
				fputs("\t# st_other (other)\n", output);
			}

			fputs(one_16, output);
			fputs("\t# st_shndx\n", output);

			fputs("&", output);
			fputs(i->name, output);
			fputc(' ', output);
			fputs(zero_32, output);
			fputs("\t# st_value\n", output);

			fputs(zero_32, output);
			fputc(' ', output);
			fputs(zero_32, output);
			fputs("\t# st_size (unknown size)\n\n", output);
		}
		else
		{
			fputs("&", output);
			fputs(i->name, output);
			fputs("\t#st_value\n", output);

			fputs(zero_32, output);
			fputs("\t# st_size (unknown size)\n", output);

			fputs(two_8, output);
			fputs("\t# st_info (FUNC)\n", output);

			if(('_' == i->name[0]) && !match(entry, i->name))
			{
				fputs(two_8, output);
				fputs("\t# st_other (hidden)\n", output);
			}
			else
			{
				fputs(zero_8, output);
				fputs("\t# st_other (default)\n", output);
			}

			fputs(one_16, output);
			fputs("\t# st_shndx\n\n", output);
		}
	}

	fputs("# END Generated symbol table\n", output);
}

struct entry* reverse_list(struct entry* head)
{
	struct entry* root = NULL;
	struct entry* next;
	while(NULL != head)
	{
		next = head->next;
		head->next = root;
		root = head;
		head = next;
	}
	return root;
}

void write_int(char* field, char* label)
{
	fputs(field, output);
	fputs("\t#", output);
	fputs(label, output);
	fputc('\n', output);
}

void write_register(char* field, char* label)
{
	/* $field section in the section headers are different size for 32 and 64bits */
	/* The below is broken for BigEndian */
	fputs(field, output);
	if(64 == BITSIZE)
	{
		fputc(' ', output);
		fputs(zero_32, output);
	}

	fputs("\t#", output);
	fputs(label, output);
	fputc('\n', output);
}

void write_section(char* label, char* name, char* type, char* flags, char* address, char* offset, char* size, char* link, char* info, char* entry)
{
	/* Write label */
	fputc('\n', output);
	fputs(label, output);
	fputc('\n', output);

	write_int(name, "sh_name");
	write_int(type, "sh_type");
	write_register(flags, "sh_flags");
	write_register(address, "sh_addr");
	write_register(offset, "sh_offset");
	write_register(size, "sh_size");
	write_int(link, "sh_link");

	/* Deal with the ugly case of stubs */
	fputs(info, output);
	fputs("\t#sh_info\n", output);

	/* Alignment section in the section headers are different size for 32 and 64bits */
	/* The below is broken for BigEndian */
	if(64 == BITSIZE)
	{
		fputs(one_32, output);
		fputc(' ', output);
		fputs(zero_32, output);
		fputs("\t#sh_addralign\n", output);
	}
	else
	{
		fputs(one_32, output);
		fputs("\t#sh_addralign\n", output);
	}

	write_register(entry, "sh_entsize");
}

char* get_string(int value, int size, int ByteMode, int shift)
{
	char* ch = calloc(42, sizeof(char));
	require(NULL != ch, "Exhausted available memory\n");
	ch[0] = '\'';
	stringify(ch+1, size, ByteMode, value, shift);
	if(!BigEndian) LittleEndian(ch+1, ByteMode);
	int i = 0;
	while(0 != ch[i])
	{
		i = i + 1;
	}
	ch[i] = '\'';
	return ch;
}

char* setup_string(int value, int number_of_bytes, int ByteMode)
{
	int shift;
	int size;
	if(HEX == ByteMode)
	{
		size = 2;
		shift = 4;
	}
	else if(OCTAL == ByteMode)
	{
		size = 3;
		shift = 3;
	}
	else if(BINARY == ByteMode)
	{
		size = 8;
		shift = 1;
	}
	else
	{
		fputs("reached impossible mode\n", stderr);
		exit(EXIT_FAILURE);
	}

	return get_string(value, number_of_bytes *size, ByteMode, shift);
}

void setup_strings(int ByteMode)
{
	zero_8 = setup_string(0, 1, ByteMode);
	zero_16 = setup_string(0, 2, ByteMode);
	zero_32 = setup_string(0, 4, ByteMode);
	one_16 = setup_string(1, 2, ByteMode);
	one_32 = setup_string(1, 4, ByteMode);
	two_8 = setup_string(2, 1, ByteMode);
	two_32 = setup_string(2, 4, ByteMode);
	three_32 = setup_string(3, 4, ByteMode);
	six_32 = setup_string(6, 4, ByteMode);
	sixteen_32 = setup_string(16, 4, ByteMode);
	twentyfour_32 = setup_string(24, 4, ByteMode);
}

/* Standard C main program */
int main(int argc, char **argv)
{
	jump_table = NULL;
	struct entry* input = NULL;
	output = stdout;
	char* output_file = "";
	entry = "";
	BITSIZE = 32;
	count = 1;
	BigEndian = TRUE;
	int ByteMode = HEX;
	int set = FALSE;
	struct entry* temp;
	struct entry* head;

	int option_index = 1;
	while(option_index <= argc)
	{
		if(NULL == argv[option_index])
		{
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "-h") || match(argv[option_index], "--help"))
		{
			fputs("Usage: ", stderr);
			fputs(argv[0], stderr);
			fputs(" --file FILENAME1 {--file FILENAME2} --output FILENAME\n", stderr);
			exit(EXIT_SUCCESS);
		}
		else if(match(argv[option_index], "--64"))
		{
			BITSIZE = 64;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "-f") || match(argv[option_index], "--file"))
		{
			temp = calloc(1, sizeof(struct entry));
			temp->name = argv[option_index + 1];
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
				fputs(input->name, stderr);
				fputs(" can not be opened!\n", stderr);
				exit(EXIT_FAILURE);
			}
			option_index = option_index + 2;
		}
		else if(match(argv[option_index], "-b") || match(argv[option_index], "--binary"))
		{
			ByteMode = BINARY;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "-O") || match(argv[option_index], "--octal"))
		{
			ByteMode = OCTAL;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "-X") || match(argv[option_index], "--hex"))
		{
			ByteMode = HEX;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "--big-endian"))
		{
			BigEndian = TRUE;
			set = TRUE;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "--little-endian"))
		{
			BigEndian = FALSE;
			set = TRUE;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "-V") || match(argv[option_index], "--version"))
		{
			fputs("blood-elf 2.0.1\n(Basically Launches Odd Object Dump ExecutabLe Files\n", stdout);
			exit(EXIT_SUCCESS);
		}
		else if(match(argv[option_index], "--entry"))
		{
			head = calloc(1, sizeof(struct entry));
			/* Include _start or any other entry from your .hex2 */
			head->next = jump_table;
			jump_table = head;
			jump_table->name = argv[option_index + 1];
			/* However only the last one will be exempt from the _name hidden rule */
			entry = argv[option_index + 1];
			option_index = option_index + 2;
			count = count + 1;
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

	/* Force setting of endianness */
	if(!set)
	{
		fputs("either --little-endian or --big-endian MUST be set\n", stderr);
		return EXIT_FAILURE;
	}

	/* Setup the ugly formating because RISC-V sucks */
	setup_strings(ByteMode);

	/* Get all of the labels */
	first_pass(input);

	/* Reverse their order */
	jump_table = reverse_list(jump_table);

	/* Create sections */
	/* Create string names for sections */
	fputs("# Generated sections\n:ELF_shstr\n", output);
	fputs(zero_8, output);
	fputs("\t# NULL\n", output);
	fputs(":ELF_shstr__text\n\".text\"\n", output);
	fputs(":ELF_shstr__shstr\n\".shstrtab\"\n", output);
	fputs(":ELF_shstr__sym\n\".symtab\"\n", output);
	fputs(":ELF_shstr__str\n\".strtab\"\n", output);

	/* Create NULL section header as is required by the Spec. So dumb and waste of bytes*/
	write_section(":ELF_section_headers", zero_32, zero_32, zero_32, zero_32, zero_32, zero_32, zero_32, zero_32, zero_32);
	write_section(":ELF_section_header_text", "%ELF_shstr__text>ELF_shstr", one_32, six_32, "&ELF_text", "%ELF_text>ELF_base", "%ELF_data>ELF_text", zero_32, zero_32, zero_32);
	write_section(":ELF_section_header_shstr", "%ELF_shstr__shstr>ELF_shstr", three_32, zero_32, "&ELF_shstr", "%ELF_shstr>ELF_base", "%ELF_section_headers>ELF_shstr", zero_32, zero_32, zero_32);
	write_section(":ELF_section_header_str", "%ELF_shstr__str>ELF_shstr", three_32, zero_32, "&ELF_str", "%ELF_str>ELF_base", "%ELF_sym>ELF_str", zero_32, zero_32, zero_32);
	if(64 == BITSIZE) write_section(":ELF_section_header_sym", "%ELF_shstr__sym>ELF_shstr", two_32, zero_32, "&ELF_sym", "%ELF_sym>ELF_base", "%ELF_end>ELF_sym", three_32, setup_string(count, 4, ByteMode), twentyfour_32);
	else write_section(":ELF_section_header_sym", "%ELF_shstr__sym>ELF_shstr", two_32, zero_32, "&ELF_sym", "%ELF_sym>ELF_base", "%ELF_end>ELF_sym", three_32, setup_string(count, 4, ByteMode), sixteen_32);

	/* Create dwarf stubs needed for objdump -d to get function names */
	output_string_table(jump_table);
	output_symbol_table(jump_table);
	fputs("\n:ELF_end\n", output);

	/* Close output file */
	fclose(output);

	return EXIT_SUCCESS;
}
