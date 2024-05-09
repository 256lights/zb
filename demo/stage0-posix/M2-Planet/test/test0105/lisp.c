/* Copyright (C) 2016 Jeremiah Orians
 * This file is part of stage0.
 *
 * stage0 is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * stage0 is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with stage0.  If not, see <http://www.gnu.org/licenses/>.
 */

#include "lisp.h"

struct file_list
{
	struct file_list* next;
	FILE* file;
};

/* Prototypes */
struct cell* eval(struct cell* exp, struct cell* env);
void init_sl3();
int Readline(FILE* source_file, char* temp);
struct cell* parse(char* program, int size);
void writeobj(FILE *ofp, struct cell* op);
void garbage_init(int number_of_cells);
void garbage_collect();

/* Read Eval Print Loop*/
int REPL(FILE* in, FILE *out)
{
	int read;
	input = in;
	char* message = calloc(MAX_STRING + 2, sizeof(char));
	read = Readline(in, message);
	if(0 == read)
	{
		return TRUE;
	}
	struct cell* temp = parse(message, read);
	current = temp;
	temp = eval(temp, top_env);
	writeobj(out, temp);
	current = nil;
	if(echo) fputc('\n', out);
	return FALSE;
}

void recursively_evaluate(struct file_list* a)
{
	if(NULL == a) return;
	recursively_evaluate(a->next);
	int Reached_EOF = FALSE;
	while(!Reached_EOF)
	{
		garbage_collect();
		Reached_EOF = REPL(a->file, console_output);
	}
}

/*** Main Driver ***/
int main(int argc, char **argv)
{
	int number_of_cells = 1000000;
	file_output = fopen("/dev/null", "w");
	console_output = stdout;
	struct file_list* essential = NULL;
	struct file_list* new;

	int i = 1;
	while(i <= argc)
	{
		if(NULL == argv[i])
		{
			i = i + 1;
		}
		else if(match(argv[i], "-c") || match(argv[i], "--console"))
		{
			console_output = fopen(argv[i + 1], "w");
			if(NULL == console_output)
			{
				fputs("The file: ", stderr);
				fputs(argv[i + 1], stderr);
				fputs(" does not appear writable\n", stderr);
				exit(EXIT_FAILURE);
			}
			i = i + 2;
		}
		else if(match(argv[i], "-f") || match(argv[i], "--file"))
		{
			new = calloc(1, sizeof(struct file_list));
			new->file = fopen(argv[i + 1], "r");
			if(NULL == new->file)
			{
				fputs("The file: ", stderr);
				fputs(argv[i + 1], stderr);
				fputs(" does not appear readable\n", stderr);
				exit(EXIT_FAILURE);
			}
			new->next = essential;
			essential = new;
			i = i + 2;
		}
		else if(match(argv[i], "-h") || match(argv[i], "--help"))
		{
			fputs("Usage: ", stdout);
			fputs(argv[0], stdout);
			fputs(" -f FILENAME1 {-f FILENAME2}\n", stdout);
			exit(EXIT_SUCCESS);
		}
		else if(match(argv[i], "-m") || match(argv[i], "--memory"))
		{
			number_of_cells = strtoint(argv[i + 1]);
			i = i + 2;
		}
		else if(match(argv[i], "-o") || match(argv[i], "--output"))
		{
			file_output =  fopen(argv[i + 1], "w");
			if(NULL == file_output)
			{
				fputs("The file: ", stderr);
				fputs(argv[i + 1], stderr);
				fputs(" does not appear writable\n", stderr);
				exit(EXIT_FAILURE);
			}
			i = i + 2;
		}
		else if(match(argv[i], "-v") || match(argv[i], "--version"))
		{
			fputs("Slow_Lisp 0.1\n", stdout);
			exit(EXIT_SUCCESS);
		}
		else
		{
			fputs("Unknown option\n", stderr);
			exit(EXIT_FAILURE);
		}
	}

	/* Our most important initializations */
	garbage_init(number_of_cells);
	init_sl3();
	int Reached_EOF;
	echo = TRUE;

	recursively_evaluate(essential);

	Reached_EOF = FALSE;
	while(!Reached_EOF)
	{
		garbage_collect();
		Reached_EOF = REPL(stdin, stdout);
	}
	fclose(file_output);
	if (console_output != stdout)
	{
		fclose(console_output);
	}
	while(NULL != essential)
	{
		fclose(essential->file);
		essential = essential -> next;
	}
	return 0;
}
