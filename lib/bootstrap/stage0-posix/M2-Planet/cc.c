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
#include<stdlib.h>
#include<stdio.h>
#include<string.h>
#include"cc.h"

/* The core functions */
void initialize_types();
struct token_list* read_all_tokens(FILE* a, struct token_list* current, char* filename);
struct token_list* reverse_list(struct token_list* head);

struct token_list* remove_line_comments(struct token_list* head);
struct token_list* remove_line_comment_tokens(struct token_list* head);
struct token_list* remove_preprocessor_directives(struct token_list* head);

void eat_newline_tokens();
void init_macro_env(char* sym, char* value, char* source, int num);
void preprocess();
void program();
void recursive_output(struct token_list* i, FILE* out);
void output_tokens(struct token_list *i, FILE* out);
int strtoint(char *a);

int main(int argc, char** argv)
{
	MAX_STRING = 4096;
	BOOTSTRAP_MODE = FALSE;
	PREPROCESSOR_MODE = FALSE;
	int DEBUG = FALSE;
	FILE* in = stdin;
	FILE* destination_file = stdout;
	Architecture = 0; /* catch unset */
	init_macro_env("__M2__", "42", "__INTERNAL_M2__", 0); /* Setup __M2__ */
	char* arch;
	char* name;
	char* hold;
	int env=0;
	char* val;

	int i = 1;
	while(i <= argc)
	{
		if(NULL == argv[i])
		{
			i = i + 1;
		}
		else if(match(argv[i], "-f") || match(argv[i], "--file"))
		{
			if(NULL == hold_string)
			{
				hold_string = calloc(MAX_STRING + 4, sizeof(char));
				require(NULL != hold_string, "Impossible Exhaustion has occurred\n");
			}

			name = argv[i + 1];
			if(NULL == name)
			{
				fputs("did not receive a file name\n", stderr);
				exit(EXIT_FAILURE);
			}

			in = fopen(name, "r");
			if(NULL == in)
			{
				fputs("Unable to open for reading file: ", stderr);
				fputs(name, stderr);
				fputs("\n Aborting to avoid problems\n", stderr);
				exit(EXIT_FAILURE);
			}
			global_token = read_all_tokens(in, global_token, name);
			fclose(in);
			i = i + 2;
		}
		else if(match(argv[i], "-o") || match(argv[i], "--output"))
		{
			destination_file = fopen(argv[i + 1], "w");
			if(NULL == destination_file)
			{
				fputs("Unable to open for writing file: ", stderr);
				fputs(argv[i + 1], stderr);
				fputs("\n Aborting to avoid problems\n", stderr);
				exit(EXIT_FAILURE);
			}
			i = i + 2;
		}
		else if(match(argv[i], "-A") || match(argv[i], "--architecture"))
		{
			arch = argv[i + 1];
			if(match("knight-native", arch)) {
				Architecture = KNIGHT_NATIVE;
				init_macro_env("__knight__", "1", "--architecture", env);
				env = env + 1;
			}
			else if(match("knight-posix", arch)) {
				Architecture = KNIGHT_POSIX;
				init_macro_env("__knight_posix__", "1", "--architecture", env);
				env = env + 1;
			}
			else if(match("x86", arch))
			{
				Architecture = X86;
				init_macro_env("__i386__", "1", "--architecture", env);
				env = env + 1;
			}
			else if(match("amd64", arch))
			{
				Architecture = AMD64;
				init_macro_env("__x86_64__", "1", "--architecture", env);
				env = env + 1;
			}
			else if(match("armv7l", arch))
			{
				Architecture = ARMV7L;
				init_macro_env("__arm__", "1", "--architecture", env);
				env = env + 1;
			}
			else if(match("aarch64", arch))
			{
				Architecture = AARCH64;
				init_macro_env("__aarch64__", "1", "--architecture", env);
				env = env + 1;
			}
			else if(match("riscv32", arch))
			{
				Architecture = RISCV32;
				init_macro_env("__riscv", "1", "--architecture", env);
				init_macro_env("__riscv_xlen", "32", "--architecture", env + 1);
				env = env + 2;
			}
			else if(match("riscv64", arch))
			{
				Architecture = RISCV64;
				init_macro_env("__riscv", "1", "--architecture", env);
				init_macro_env("__riscv_xlen", "64", "--architecture", env + 1);
				env = env + 2;
			}
			else
			{
				fputs("Unknown architecture: ", stderr);
				fputs(arch, stderr);
				fputs(" know values are: knight-native, knight-posix, x86, amd64, armv7l, aarch64, riscv32 and riscv64\n", stderr);
				exit(EXIT_FAILURE);
			}
			i = i + 2;
		}
		else if(match(argv[i], "--max-string"))
		{
			hold = argv[i+1];
			if(NULL == hold)
			{
				fputs("--max-string requires a numeric argument\n", stderr);
				exit(EXIT_FAILURE);
			}
			MAX_STRING = strtoint(hold);
			require(0 < MAX_STRING, "Not a valid string size\nAbort and fix your --max-string\n");
			i = i + 2;
		}
		else if(match(argv[i], "--bootstrap-mode"))
		{
			BOOTSTRAP_MODE = TRUE;
			i = i + 1;
		}
		else if(match(argv[i], "-g") || match(argv[i], "--debug"))
		{
			DEBUG = TRUE;
			i = i + 1;
		}
		else if(match(argv[i], "-h") || match(argv[i], "--help"))
		{
			fputs(" -f input file\n -o output file\n --help for this message\n --version for file version\n", stdout);
			exit(EXIT_SUCCESS);
		}
		else if(match(argv[i], "-E"))
		{
			PREPROCESSOR_MODE = TRUE;
			i = i + 1;
		}
		else if(match(argv[i], "-D"))
		{
			val = argv[i+1];
			if(NULL == val)
			{
				fputs("-D requires an argument", stderr);
				exit(EXIT_FAILURE);
			}
			while(0 != val[0])
			{
				if('=' == val[0])
				{
					val[0] = 0;
					val = val + 1;
					break;
				}
				val = val + 1;
			}
			init_macro_env(argv[i+1], val, "__ARGV__", env);
			env = env + 1;
			i = i + 2;
		}
		else if(match(argv[i], "-V") || match(argv[i], "--version"))
		{
			fputs("M2-Planet v1.11.0\n", stderr);
			exit(EXIT_SUCCESS);
		}
		else
		{
			fputs("UNKNOWN ARGUMENT\n", stdout);
			exit(EXIT_FAILURE);
		}
	}

	/* Deal with special case of architecture not being set */
	if(0 == Architecture)
	{
		Architecture = KNIGHT_NATIVE;
		init_macro_env("__knight__", "1", "--architecture", env);
	}

	/* Deal with special case of wanting to read from standard input */
	if(stdin == in)
	{
		hold_string = calloc(MAX_STRING + 4, sizeof(char));
		require(NULL != hold_string, "Impossible Exhaustion has occurred\n");
		global_token = read_all_tokens(in, global_token, "STDIN");
	}

	if(NULL == global_token)
	{
		fputs("Either no input files were given or they were empty\n", stderr);
		exit(EXIT_FAILURE);
	}
	global_token = reverse_list(global_token);

	if (BOOTSTRAP_MODE)
	{
		global_token = remove_line_comment_tokens(global_token);
		global_token = remove_preprocessor_directives(global_token);
	}
	else
	{
		global_token = remove_line_comments(global_token);
		preprocess();
	}

	if (PREPROCESSOR_MODE)
	{
		fputs("\n/* Preprocessed source */\n", destination_file);
		output_tokens(global_token, destination_file);
		goto exit_success;
	}

	/* the main parser doesn't know how to handle newline tokens */
	eat_newline_tokens();

	initialize_types();
	reset_hold_string();
	output_list = NULL;
	program();

	/* Output the program we have compiled */
	fputs("\n# Core program\n", destination_file);
	recursive_output(output_list, destination_file);
	if(KNIGHT_NATIVE == Architecture) fputs("\n", destination_file);
	else if(DEBUG) fputs("\n:ELF_data\n", destination_file);
	fputs("\n# Program global variables\n", destination_file);
	recursive_output(globals_list, destination_file);
	fputs("\n# Program strings\n", destination_file);
	recursive_output(strings_list, destination_file);
	if(KNIGHT_NATIVE == Architecture) fputs("\n:STACK\n", destination_file);
	else if(!DEBUG) fputs("\n:ELF_end\n", destination_file);

exit_success:
	if (destination_file != stdout)
	{
		fclose(destination_file);
	}
	return EXIT_SUCCESS;
}
