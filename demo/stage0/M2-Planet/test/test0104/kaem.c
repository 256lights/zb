/* Copyright (C) 2016 Jeremiah Orians
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

#include <stdlib.h>
#include <stdio.h>
#include <unistd.h>
#include <sys/wait.h>

#define FALSE 0
//CONSTANT FALSE 0
#define TRUE 1
//CONSTANT TRUE 1
#define max_string 4096
//CONSTANT max_string 4096
#define max_args 256
//CONSTANT max_args 256

char* int2str(int x, int base, int signed_p);
int match(char* a, char* b);

char** tokens;
int command_done;
int VERBOSE;
int STRICT;
int envp_length;


/* Function for purging line comments */
void collect_comment(FILE* input)
{
	int c;
	do
	{
		c = fgetc(input);
		if(-1 == c)
		{
			fputs("IMPROPERLY TERMINATED LINE COMMENT!\nABORTING HARD\n", stderr);
			exit(EXIT_FAILURE);
		}
	} while('\n' != c);
}

/* Function for collecting RAW strings and removing the " that goes with them */
int collect_string(FILE* input, int index, char* target)
{
	int c;
	do
	{
		c = fgetc(input);
		if(-1 == c)
		{ /* We never should hit EOF while collecting a RAW string */
			fputs("IMPROPERLY TERMINATED RAW string!\nABORTING HARD\n", stderr);
			exit(EXIT_FAILURE);
		}
		else if('"' == c)
		{ /* Made it to the end */
			c = 0;
		}
		target[index] = c;
		index = index + 1;
	} while(0 != c);
	return index;
}

/* Function to collect an individual argument or purge a comment */
char* collect_token(FILE* input)
{
	char* token = calloc(max_string, sizeof(char));
	char c;
	int i = 0;
	do
	{
		c = fgetc(input);
		if(-1 == c)
		{ /* Deal with end of file */
			fputs("execution complete\n", stderr);
			exit(EXIT_SUCCESS);
		}
		else if((' ' == c) || ('\t' == c))
		{ /* space and tab are token seperators */
			c = 0;
		}
		else if('\n' == c)
		{ /* Command terminates at end of line */
			c = 0;
			command_done = 1;
		}
		else if('"' == c)
		{ /* RAW strings are everything between a pair of "" */
			i = collect_string(input, i, token);
			c = 0;
		}
		else if('#' == c)
		{ /* Line comments to aid the humans */
			collect_comment(input);
			c = 0;
			command_done = 1;
		}
		else if('\\' == c)
		{ /* Support for end of line escapes, drops the char after */
			fgetc(input);
			c = 0;
		}
		token[i] = c;
		i = i + 1;
	} while (0 != c);

	if(1 == i)
	{ /* Nothing worth returning */
		free(token);
		return NULL;
	}
	return token;
}

char* copy_string(char* target, char* source)
{
	while(0 != source[0])
	{
		target[0] = source[0];
		target = target + 1;
		source = source + 1;
	}
	return target;
}

int string_length(char* a)
{
	int i = 0;
	while(0 != a[i]) i = i + 1;
	return i;
}

char* prepend_string(char* add, char* base)
{
	char* ret = calloc(max_string, sizeof(char));
	copy_string(copy_string(ret, add), base);
	return ret;
}

char* find_char(char* string, char a)
{
	if(0 == string[0]) return NULL;
	while(a != string[0])
	{
		string = string + 1;
		if(0 == string[0]) return string;
	}
	return string;
}

char* prematch(char* search, char* field)
{
	do
	{
		if(search[0] != field[0]) return NULL;
		search = search + 1;
		field = field + 1;
	} while(0 != search[0]);
	return field;
}

char* env_lookup(char* token, char** envp)
{
	if(NULL == envp) return NULL;
	int i = 0;
	char* ret = NULL;
	do
	{
		ret = prematch(token, envp[i]);
		if(NULL != ret) return ret;
		i = i + 1;
	} while(NULL != envp[i]);
	return NULL;
}

char* find_executable(char* name, char* PATH)
{
	if(('.' == name[0]) || ('/' == name[0]))
	{ /* assume names that start with . or / are relative or absolute */
		return name;
	}

	char* next = find_char(PATH, ':');
	char* trial;
	FILE* t;
	while(NULL != next)
	{
		next[0] = 0;
		trial = prepend_string(PATH, prepend_string("/", name));

		t = fopen(trial, "r");
		if(NULL != t)
		{
			fclose(t);
			return trial;
		}
		PATH = next + 1;
		next = find_char(PATH, ':');
		free(trial);
	}
	return NULL;
}

/* Function to check if the token is an envar and if it is get the pos of = */
int check_envar(char* token)
{
	int j;
	int equal_found;
	equal_found = 0;
	int found;
	char c;

	for(j = 0; j < string_length(token); j = j + 1)
	{
		if(token[j] == '=')
		{ /* After = can be anything */
			equal_found = 1;
			break;
		}
		else
		{ /* Should be A-z */
			found = 0;
			/* Represented numerically; 0 = 48 through 9 = 57 */
			for(c = 48; c <= 57; c = c + 1)
			{
				if(token[j] == c)
				{
					found = 1;
				}
			}
			/* Represented numerically; A = 65 through z = 122 */
			for(c = 65; c <= 122; c = c + 1)
			{
				if(token[j] == c)
				{
					found = 1;
				}
			}
			if(found == 0)
			{ /* In all likelihood this isn't actually an environment variable */
				return 1;
			}
		}
	}
	if(equal_found == 0)
	{ /* Not an envar */
		return 1;
	}
	return 0;
}

/* Function for executing our programs with desired arguments */
void execute_commands(FILE* script, char** envp, int envp_length)
{
	char* PATH;
	char* USERNAME;
	int i;
	int status;
	char* result;
	int j;
	int is_envar;
	char* program;
	int f;

	while(1)
	{
		tokens = calloc(max_args, sizeof(char*));
		PATH = env_lookup("PATH=", envp);
		if(NULL != PATH)
		{
			PATH = calloc(max_string, sizeof(char));
			copy_string(PATH, env_lookup("PATH=", envp));
		}

		USERNAME = env_lookup("LOGNAME=", envp);
		if((NULL == PATH) && (NULL == USERNAME))
		{
			PATH = calloc(max_string, sizeof(char));
			copy_string(PATH, "/root/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin");
		}
		else if(NULL == PATH)
		{
			PATH = prepend_string("/home/", prepend_string(USERNAME,"/bin:/usr/local/bin:/usr/bin:/bin:/usr/local/games:/usr/games"));
		}

		i = 0;
		status = 0;
		command_done = 0;
		do
		{
			result = collect_token(script);
			if(0 != result)
			{ /* Not a comment string but an actual argument */
				tokens[i] = result;
				i = i + 1;
			}
		} while(0 == command_done);

		if(VERBOSE && (0 < i))
		{
			fputs(" +> ", stdout);
			for(j = 0; j < i; j = j + 1)
			{
				fputs(tokens[j], stdout);
				fputc(' ', stdout);
			}
			fputs("\n", stdout);
		}

		if(0 < i)
		{ /* Not a line comment */
			is_envar = 0;
			if(check_envar(tokens[0]) == 0)
			{ /* It's an envar! */
				is_envar = 1;
				envp[envp_length] = tokens[0]; /* Since arrays are 0 indexed */
				envp_length = envp_length + 1;
			}

			if(is_envar == 0)
			{ /* Stuff to exec */
				program = find_executable(tokens[0], PATH);
				if(NULL == program)
				{
					fputs(tokens[0], stderr);
					fputs("Some weird shit went down with: ", stderr);
					fputs("\n", stderr);
					exit(EXIT_FAILURE);
				}

				f = fork();
				if (f == -1)
				{
					fputs("fork() failure", stderr);
					exit(EXIT_FAILURE);
				}
				else if (f == 0)
				{ /* child */
					/* execve() returns only on error */
					execve(program, tokens, envp);
					/* Prevent infinite loops */
					_exit(EXIT_SUCCESS);
				}

				/* Otherwise we are the parent */
				/* And we should wait for it to complete */
				waitpid(f, &status, 0);

				if(STRICT && (0 != status))
				{ /* Clearly the script hit an issue that should never have happened */
					fputs("Subprocess error ", stderr);
					fputs(int2str(status,10, FALSE), stderr);
					fputs("\nABORTING HARD\n", stderr);
					/* stop to prevent damage */
					exit(EXIT_FAILURE);
				}
			}
			/* Then go again */
		}
	}
}


int main(int argc, char** argv, char** envp)
{
	VERBOSE = FALSE;
	STRICT = FALSE;
	char* filename = "kaem.run";
	FILE* script = NULL;

	/* Get envp_length */
	envp_length = 1;
	while(envp[envp_length] != NULL)
	{
		envp_length = envp_length + 1;
	}
	char** nenvp = calloc(envp_length + max_args + 1, sizeof(char*));
	int i;
	for(i = 0; i < envp_length; i = i + 1)
	{
		nenvp[i] = envp[i];
	}

	for(i = envp_length; i < (envp_length + max_args); i = i + 1)
	{
		nenvp[i] = "";
	}

	i = 1;
	while(i <= argc)
	{
		if(NULL == argv[i])
		{
			i = i + 1;
		}
		else if(match(argv[i], "-h") || match(argv[i], "--help"))
		{
			fputs("kaem only accepts --help, --version, --file, --verbose, --nightmare-mode or no arguments\n", stdout);
			exit(EXIT_SUCCESS);
		}
		else if(match(argv[i], "-f") || match(argv[i], "--file"))
		{
			filename = argv[i + 1];
			i = i + 2;
		}
		else if(match(argv[i], "n") || match(argv[i], "--nightmare-mode"))
		{
			fputs("Begin nightmare", stdout);
			envp = NULL;
			i = i + 1;
		}
		else if(match(argv[i], "-V") || match(argv[i], "--version"))
		{
			fputs("kaem version 0.6.0\n", stdout);
			exit(EXIT_SUCCESS);
		}
		else if(match(argv[i], "--verbose"))
		{
			VERBOSE = TRUE;
			i = i + 1;
		}
		else if(match(argv[i], "--strict"))
		{
			STRICT = TRUE;
			i = i + 1;
		}
		else
		{
			fputs("UNKNOWN ARGUMENT\n", stdout);
			exit(EXIT_FAILURE);
		}
	}

	script = fopen(filename, "r");

	if(NULL == script)
	{
		fputs("The file: ", stderr);
		fputs(filename, stderr);
		fputs(" can not be opened!\n", stderr);
		exit(EXIT_FAILURE);
	}

	execute_commands(script, nenvp, envp_length);
	fclose(script);
	return EXIT_SUCCESS;
}
