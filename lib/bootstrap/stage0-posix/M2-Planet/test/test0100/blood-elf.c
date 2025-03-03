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
#include <getopt.h>
#include <unistd.h>
#include <sys/stat.h>


#define max_string 4096
//CONSTANT max_string 4096
#define TRUE 1
//CONSTANT TRUE 1
#define FALSE 0
//CONSTANT FALSE 0

#define FALSE 0
// CONSTANT FALSE 0
#define TRUE 1
// CONSTANT TRUE 1

int match(char* a, char* b)
{
	int i = -1;
	do
	{
		i = i + 1;
		if(a[i] != b[i])
		{
			return FALSE;
		}
	} while((0 != a[i]) && (0 !=b[i]));
	return TRUE;
}

struct entry
{
	struct entry* next;
	char* name;
};

FILE* output;
struct entry* jump_table;

void consume_token(FILE* source_file, char* s)
{
	int i = 0;
	int c = fgetc(source_file);
	do
	{
		s[i] = c;
		i = i + 1;
		c = fgetc(source_file);
	} while((' ' != c) && ('\t' != c) && ('\n' != c) && '>' != c);
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

	/* Remove all entries that start with the forbidden char pattern :_ */
	if('_' == entry->name[0])
	{
		jump_table = jump_table->next;
	}
}

void line_Comment(FILE* source_file)
{
	int c = fgetc(source_file);
	while((10 != c) && (13 != c))
	{
		c = fgetc(source_file);
	}
}

void purge_string(FILE* source_file)
{
	int c = fgetc(source_file);
	while((EOF != c) && (34 != c))
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
		else if (34 == c)
		{
			purge_string(source_file);
		}
	}
	fclose(source_file);
}

void output_debug(struct entry* node, int stage)
{
	struct entry* i;
	for(i = node; NULL != i; i = i->next)
	{
		if(stage)
		{
			fputs(":ELF_str_", output);
			fputs(i->name, output);
			fputs("\n\x22", output);
			fputs(i->name, output);
			fputs("\x22\n", output);
		}
		else
		{
			fputs("%ELF_str_", output);
			fputs(i->name, output);
			fputs(">ELF_str\n&", output);
			fputs(i->name, output);
			fputs("\n%10000\n!2\n!0\n@1\n", output);
		}
	}
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

/* Standard C main program */
int main(int argc, char **argv)
{
	jump_table = NULL;
	struct entry* input = NULL;
	output = stdout;
	char* output_file = "";
	struct entry* temp;

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
			fputs(" -f FILENAME1 {-f FILENAME2}\n", stderr);
			exit(EXIT_SUCCESS);
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
		else if(match(argv[option_index], "-V") || match(argv[option_index], "--version"))
		{
			fputs("blood-elf 0.1\n(Basically Launches Odd Object Dump ExecutabLe Files\n", stdout);
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
	first_pass(input);

	/* Reverse their order */
	jump_table = reverse_list(jump_table);

	fputs(":ELF_str\n!0\n", output);
	output_debug(jump_table, TRUE);
	fputs("%0\n:ELF_sym\n%0\n%0\n%0\n!0\n!0\n@1\n", output);
	output_debug(jump_table, FALSE);
	fputs("\n:ELF_end\n", output);

	if (output != stdout) {
		fclose(output);
	}

	return EXIT_SUCCESS;
}
