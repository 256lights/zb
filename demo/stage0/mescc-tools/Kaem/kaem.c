/* Copyright (C) 2016-2020 Jeremiah Orians
 * Copyright (C) 2020 fosslinux
 * Copyright (C) 2021 Andrius Štikonas
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
#include <string.h>
#include "kaem.h"

/* Prototypes from other files */
void handle_variables(char** argv, struct Token* n);

/*
 * UTILITY FUNCTIONS
 */

/* Function to find a character in a string */
char* find_char(char* string, char a)
{
	if(0 == string[0])
	{
		return NULL;
	}

	while(a != string[0])
	{
		string = string + 1;

		if(0 == string[0])
		{
			return string;
		}
	}

	return string;
}

/* Function to find the length of a char**; an array of strings */
int array_length(char** array)
{
	int length = 0;

	while(array[length] != NULL)
	{
		length = length + 1;
	}

	return length;
}

/* Search for a variable in the token linked-list */
char* token_lookup(char* variable, struct Token* token)
{
	/* Start at the head */
	struct Token* n = token;

	/* Loop over the linked-list */
	while(n != NULL)
	{
		if(match(variable, n->var))
		{
			/* We have found the correct node */
			return n->value; /* Done */
		}

		/* Nope, try the next */
		n = n->next;
	}

	/* We didn't find anything! */
	return NULL;
}

/* Search for a variable in the env linked-list */
char* env_lookup(char* variable)
{
	return token_lookup(variable, env);
}

/* Search for a variable in the alias linked-list */
char* alias_lookup(char* variable)
{
	return token_lookup(variable, alias);
}

/* Find the full path to an executable */
char* find_executable(char* name)
{
	if(match("", name))
	{
		return NULL;
	}

	if(('.' == name[0]) || ('/' == name[0]))
	{
		/* assume names that start with . or / are relative or absolute */
		return name;
	}

	char* trial = calloc(MAX_STRING, sizeof(char));
	char* MPATH = calloc(MAX_STRING, sizeof(char)); /* Modified PATH */
	require(MPATH != NULL, "Memory initialization of MPATH in find_executable failed\n");
	strcpy(MPATH, PATH);
	FILE* t;
	char* next = find_char(MPATH, ':');
	int index;
	int offset;
	int mpath_length;
	int name_length;
	int trial_length;

	while(NULL != next)
	{
		/* Reset trial */
		trial_length = strlen(trial);

		for(index = 0; index < trial_length; index = index + 1)
		{
			trial[index] = 0;
		}

		next[0] = 0;
		/* prepend_string(MPATH, prepend_string("/", name)) */
		mpath_length = strlen(MPATH);

		for(index = 0; index < mpath_length; index = index + 1)
		{
			require(MAX_STRING > index, "Element of PATH is too long\n");
			trial[index] = MPATH[index];
		}

		trial[index] = '/';
		offset = strlen(trial);
		name_length = strlen(name);

		for(index = 0; index < name_length; index = index + 1)
		{
			require(MAX_STRING > index, "Element of PATH is too long\n");
			trial[index + offset] = name[index];
		}

		/* Try the trial */
		require(strlen(trial) < MAX_STRING, "COMMAND TOO LONG!\nABORTING HARD\n");
		t = fopen(trial, "r");

		if(NULL != t)
		{
			fclose(t);
			return trial;
		}

		MPATH = next + 1;
		next = find_char(MPATH, ':');
	}

	return NULL;
}

/* Function to convert a Token linked-list into an array of strings */
char** list_to_array(struct Token* s)
{
	struct Token* n;
	n = s;
	char** array = calloc(MAX_ARRAY, sizeof(char*));
	require(array != NULL, "Memory initialization of array in conversion of list to array failed\n");
	char* element = calloc(MAX_STRING, sizeof(char));
	require(element != NULL, "Memory initialization of element in conversion of list to array failed\n");
	int index = 0;
	int i;
	int value_length;
	int var_length;
	int offset;

	while(n != NULL)
	{
		/* Loop through each node and assign it to an array index */
		array[index] = calloc(MAX_STRING, sizeof(char));
		require(array[index] != NULL, "Memory initialization of array[index] in conversion of list to array failed\n");
		/* Bounds checking */
		/* No easy way to tell which it is, output generic message */
		require(index < MAX_ARRAY, "SCRIPT TOO LONG or TOO MANY ENVARS\nABORTING HARD\n");

		if(n->var == NULL)
		{
			/* It is a line */
			array[index] = n->value;
		}
		else
		{
			/* It is a var */
			/* prepend_string(n->var, prepend_string("=", n->value)) */
			var_length = strlen(n->var);

			for(i = 0; i < var_length; i = i + 1)
			{
				element[i] = n->var[i];
			}

			element[i] = '=';
			i = i + 1;
			offset = i;
			value_length = strlen(n->value);

			for(i = 0; i < value_length; i = i + 1)
			{
				element[i + offset] = n->value[i];
			}
		}

		/* Insert elements if not empty */
		if(!match("", element))
		{
			strcpy(array[index], element);
		}

		n = n->next;
		index = index + 1;

		/* Reset element */
		for(i = 0; i < MAX_STRING; i = i + 1)
		{
			element[i] = 0;
		}
	}

	return array;
}

/* Function to handle the correct options for escapes */
int handle_escape(int c)
{
	if(c == '\n')
	{
		/* Do nothing - eat up the newline */
		return -1;
	}
	else if('n' == c)
	{
		/* Add a newline to the token */
		return '\n';
	}
	else if('r' == c)
	{
		/* Add a return to the token */
		return '\r';
	}
	else if('\\' == c)
	{
		/* Add a real backslash to the token */
		return '\\';
	}
	else
	{
		/* Just add it to the token (eg, quotes) */
		return c;
	}
}

/*
 * TOKEN COLLECTION FUNCTIONS
 */

/* Function for skipping over line comments */
void collect_comment(FILE* input)
{
	int c;

	/* Eat up the comment, one character at a time */
	/*
	 * Sanity check that the comment ends with \n.
	 * Remove the comment from the FILE*
	 */
	do
	{
		c = fgetc(input);
		/* We reached an EOF!! */
		require(EOF != c, "IMPROPERLY TERMINATED LINE COMMENT!\nABORTING HARD\n");
	} while('\n' != c); /* We can now be sure it ended with \n -- and have purged the comment */
}

/* Function for collecting strings and removing the "" pair that goes with them */
int collect_string(FILE* input, char* n, int index)
{
	int string_done = FALSE;
	int c;

	do
	{
		/* Bounds check */
		require(MAX_STRING > index, "LINE IS TOO LONG\nABORTING HARD\n");
		c = fgetc(input);
		require(EOF != c, "IMPROPERLY TERMINATED STRING!\nABORTING HARD\n");

		if('\\' == c)
		{
			/* We are escaping the next character */
			/* This correctly handles escaped quotes as it just returns the quote */
			c = fgetc(input);
			c = handle_escape(c);
			n[index] = c;
			index = index + 1;
		}
		else if('"' == c)
		{
			/* End of string */
			string_done = TRUE;
		}
		else
		{
			n[index] = c;
			index = index + 1;
		}
	} while(string_done == FALSE);

	return index;
}

/* Function to parse and assign token->value */
int collect_token(FILE* input, char* n, int last_index)
{
	int c;
	int cc;
	int token_done = FALSE;
	int index = 0;

	do
	{
		/* Loop over each character in the token */
		c = fgetc(input);
		/* Bounds checking */
		require(MAX_STRING > index, "LINE IS TOO LONG\nABORTING HARD\n");

		if(EOF == c)
		{
			/* End of file -- this means script complete */
			/* We don't actually exit here. This logically makes more sense;
			 * let the code follow its natural path of execution and exit
			 * sucessfuly at the end of main().
			 */
			token_done = TRUE;
			command_done = TRUE;
			return -1;
		}
		else if((' ' == c) || ('\t' == c))
		{
			/* Space and tab are token separators */
			token_done = TRUE;
		}
		else if(('\n' == c) || (';' == c))
		{
			/* Command terminates at the end of a line or at semicolon */
			command_done = TRUE;
			token_done = TRUE;

			if(0 == index)
			{
				index = last_index;
			}
		}
		else if('"' == c)
		{
			/* Handle strings -- everything between a pair of "" */
			index = collect_string(input, n, index);
			token_done = TRUE;
		}
		else if('#' == c)
		{
			/* Handle line comments */
			collect_comment(input);
			command_done = TRUE;
			token_done = TRUE;

			if(0 == index)
			{
				index = last_index;
			}
		}
		else if('\\' == c)
		{
			/* Support for escapes */
			c = fgetc(input); /* Skips over \, gets the next char */
			cc = handle_escape(c);

			if(-1 != cc)
			{
				/* We need to put it into the token */
				n[index] = cc;
			}

			index = index + 1;
		}
		else if(0 == c)
		{
			/* We have come to the end of the token */
			token_done = TRUE;
		}
		else
		{
			/* It's a character to assign */
			n[index] = c;
			index = index + 1;
		}
	} while(token_done == FALSE);

	return index;
}

/* Function to parse string and assign token->value */
int collect_alias_token(char* input, char* n, int index)
{
	int c;
	int cc;
	int token_done = FALSE;
	int output_index = 0;

	do
	{
		/* Loop over each character in the token */
		c = input[index];
		index = index + 1;

		if((' ' == c) || ('\t' == c))
		{
			/* Space and tab are token separators */
			token_done = TRUE;
		}
		else if('\\' == c)
		{
			/* Support for escapes */
			c = input[index];
			index = index + 1;
			cc = handle_escape(c);

			/* We need to put it into the token */
			n[output_index] = cc;
			output_index = output_index + 1;
		}
		else if(0 == c)
		{
			/* We have come to the end of the token */
			token_done = TRUE;
			index = 0;
		}
		else
		{
			/* It's a character to assign */
			n[output_index] = c;
			output_index = output_index + 1;
		}
	} while(token_done == FALSE);

        /* Terminate the output with a NULL */
        n[output_index] = 0;

	return index;
}

/*
 * EXECUTION FUNCTIONS
 * Note: All of the builtins return SUCCESS (0) when they exit successfully
 * and FAILURE (1) when they fail.
 */

/* Function to check if the token is an envar */
int is_envar(char* token)
{
	int i = 0;
	int token_length = strlen(token);

	while(i < token_length)
	{
		if(token[i] == '=')
		{
			return FAILURE;
		}

		i = i + 1;
	}

	return SUCCESS;
}

/* Add an envar */
void add_envar()
{
	/* Pointers to strings we want */
	char* name = calloc(strlen(token->value) + 4, sizeof(char));
	char* value = token->value;
	char* newvalue;
	int i = 0;

	/* Isolate the name */
	while('=' != value[i])
	{
		name[i] = value[i];
		i = i + 1;
	}

	/* Isolate the value */
	newvalue = name + i + 2;
	value = value + i + 1;
	i = 0;
	require(0 != value[i], "add_envar received improper variable\n");

	while(0 != value[i])
	{
		newvalue[i] = value[i];
		i = i + 1;
	}

	/* If we are in init-mode and this is the first var env == NULL, rectify */
	if(env == NULL)
	{
		env = calloc(1, sizeof(struct Token));
		require(env != NULL, "Memory initialization of env failed\n");
		env->var = name; /* Add our first variable */
	}

	/*
	 * If the name of the envar is PATH, then we need to set our (internal)
	 * global PATH value.
	 */
	if(match(name, "PATH"))
	{
		strcpy(PATH, newvalue);
	}

	struct Token* n = env;

	/* Find match if possible */
	while(!match(name, n->var))
	{
		if(NULL == n->next)
		{
			n->next = calloc(1, sizeof(struct Token));
			require(n->next != NULL, "Memory initialization of next env node in add_envar failed\n");
			n->next->var = name;
		} /* Loop will match and exit */

		n = n->next;
	}

	/* Since we found the variable we need only to set it to its new value */
	n->value = newvalue;
}

/* Add an alias */
void add_alias()
{
	token = token->next; /* Skip the actual alias */
	if(token->next == NULL)
	{
		/* No arguments */
		char** array = list_to_array(alias);
		int index = 0;
		while(array[index] != NULL) {
			fputs(array[index], stdout);
			fputc('\n', stdout);
			index = index + 1;
		}
		fflush(stdout);
		return;
	}
	if(!is_envar(token->value)) {
		char** array = list_to_array(token);
		int index = 0;
		while(array[index] != NULL) {
			fputs(array[index], stdout);
			fputc(' ', stdout);
			index = index + 1;
		}
		fputc('\n', stdout);
		fflush(stdout);
		return;
	}

	/* Pointers to strings we want */
	char* name = calloc(strlen(token->value) + 4, sizeof(char));
	char* value = token->value;
	char* newvalue;
	int i = 0;

	/* Isolate the name */
	while('=' != value[i])
	{
		name[i] = value[i];
		i = i + 1;
	}

	/* Isolate the value */
	newvalue = name + i + 2;
	value = value + i + 1;
	i = 0;
	require(0 != value[i], "add_alias received improper variable\n");

	while(0 != value[i])
	{
		newvalue[i] = value[i];
		i = i + 1;
	}

	/* If this is the first alias, rectify */
	if(alias == NULL)
	{
		alias = calloc(1, sizeof(struct Token));
		require(alias != NULL, "Memory initialization of alias failed\n");
		alias->var = name; /* Add our first variable */
	}

	struct Token* n = alias;

	/* Find match if possible */
	while(!match(name, n->var))
	{
		if(NULL == n->next)
		{
			n->next = calloc(1, sizeof(struct Token));
			require(n->next != NULL, "Memory initialization of next alias node in alias failed\n");
			n->next->var = name;
		} /* Loop will match and exit */

		n = n->next;
	}

	/* Since we found the variable we need only to set it to its new value */
	n->value = newvalue;
}

/* cd builtin */
int cd()
{
	if(NULL == token->next)
	{
		return FAILURE;
	}

	token = token->next;

	if(NULL == token->value)
	{
		return FAILURE;
	}

	int ret = chdir(token->value);

	if(0 > ret)
	{
		return FAILURE;
	}

	return SUCCESS;
}

/* pwd builtin */
int pwd()
{
	char* path = calloc(MAX_STRING, sizeof(char));
	require(path != NULL, "Memory initialization of path in pwd failed\n");
	getcwd(path, MAX_STRING);
	require(!match("", path), "getcwd() failed\n");
	fputs(path, stdout);
	fputs("\n", stdout);
	return SUCCESS;
}

/* set builtin */
int set()
{
	/* Get the options */
	int i;

	if(NULL == token->next)
	{
		goto cleanup_set;
	}

	token = token->next;

	if(NULL == token->value)
	{
		goto cleanup_set;
	}

	char* options = calloc(MAX_STRING, sizeof(char));
	require(options != NULL, "Memory initialization of options in set failed\n");
	int last_position = strlen(token->value) - 1;

	for(i = 0; i < last_position; i = i + 1)
	{
		options[i] = token->value[i + 1];
	}

	/* Parse the options */
	int options_length = strlen(options);

	for(i = 0; i < options_length; i = i + 1)
	{
		if(options[i] == 'a')
		{
			/* set -a is on by default and cannot be disabled at this time */
			if(WARNINGS)
			{
				fputs("set -a is on by default and cannot be disabled\n", stdout);
			}

			continue;
		}
		else if(options[i] == 'e')
		{
			/* Fail on failure */
			STRICT = TRUE;
		}
		else if(options[i] == 'x')
		{
			/* Show commands as executed */
			/* TODO: this currently behaves like -v. Make it do what it should */
			VERBOSE = TRUE;
			/*
			 * Output the set -x because VERBOSE didn't catch it before.
			 * We don't do just -x because we support multiple options in one command,
			 * eg set -ex.
			 */
			fputs(" +> set -", stdout);
			fputs(options, stdout);
			fputs("\n", stdout);
			fflush(stdout);
		}
		else
		{
			/* Invalid */
			fputc(options[i], stderr);
			fputs(" is an invalid set option!\n", stderr);
			exit(EXIT_FAILURE);
		}
	}

	return SUCCESS;
cleanup_set:
	return FAILURE;
}

/* echo builtin */
void echo()
{
	if(token->next == NULL)
	{
		/* No arguments */
		fputs("\n", stdout);
		return;
	}

	if(token->next->value == NULL)
	{
		/* No arguments */
		fputs("\n", stdout);
		return;
	}

	token = token->next; /* Skip the actual echo */

	while(token != NULL)
	{
		/* Output each argument to echo to stdout */
		if(token->value == NULL)
		{
			break;
		}

		fputs(token->value, stdout);
		if(NULL != token->next)
		{
			/* M2-Planet doesn't short circuit */
			if(NULL != token->next->value) fputc(' ', stdout);
		}
		token = token->next;
	}

	fputs("\n", stdout);
}

/* unset builtin */
void unset()
{
	struct Token* e;
	/* We support multiple variables on the same line */
	struct Token* t;

	for(t = token->next; t != NULL; t = t->next)
	{
		if(NULL == t->value)
		{
			continue;
		}

		e = env;

		/* Look for the variable; we operate on ->next because we need to remove ->next */
		while(e->next != NULL)
		{
			if(match(e->next->var, t->value))
			{
				break;
			}

			e = e->next;
		}

		if(e->next != NULL)
		{
			/* There is something to unset */
			e->next = e->next->next;
		}

	}
}

void execute(FILE* script, char** argv);
int _execute(FILE* script, char** argv);
int collect_command(FILE* script, char** argv);

/* if builtin */
void if_cmd(FILE* script, char** argv)
{
	int index;
	int old_VERBOSE;
	token = token->next; /* Skip the actual if */
	/* Do not check for successful exit status */
	int if_status = _execute(script, argv);
	old_VERBOSE = VERBOSE;
	VERBOSE = VERBOSE && !if_status;

	do
	{
		index = collect_command(script, argv);
		require(index != -1, "Unexpected EOF, improperly terminated if statement.\n");

		if(0 == index)
		{
			continue;
		}

		if(0 == if_status)
		{
			/* Stuff to exec */
			execute(script, argv);
		}

		if(match(token->value, "else"))
		{
			if_status = !if_status;
		}
	} while(!match(token->value, "fi"));

	VERBOSE = old_VERBOSE;
}

int what_exit(char* program, int status)
{
	/***********************************************************************************
	 * If the low-order 8 bits of w_status are equal to 0x7F or zero, the child        *
	 * process has stopped. If the low-order 8 bits of w_status are non-zero and are   *
	 * not equal to 0x7F, the child process terminated due to a signal otherwise, the  *
	 * child process terminated due to an exit() call.                                 *
	 *                                                                                 *
	 * In the event it was a signal that stopped the process the top 8 bits of         *
	 * w_status contain the signal that caused the process to stop.                    *
	 *                                                                                 *
	 * In the event it was terminated the bottom 7 bits of w_status contain the        *
	 * terminating error number for the process.                                       *
	 *                                                                                 *
	 * If bit 0x80 of w_status is set, a core dump was produced.                       *
	 ***********************************************************************************/

	int WIFEXITED = !(status & 0x7F);
	int WEXITSTATUS = (status & 0xFF00) >> 8;
	int WTERMSIG = status & 0x7F;
	int WCOREDUMP = status & 0x80;
	int WIFSIGNALED = !((0x7F == WTERMSIG) || (0 == WTERMSIG));
	int WIFSTOPPED = ((0x7F == WTERMSIG) && (0 == WCOREDUMP));

	if(WIFEXITED)
	{
		if(VERBOSE_EXIT)
		{
			fputc('\n', stderr);
			fputs(program, stderr);
			fputs(" normal termination, exit status = ", stderr);
			fputs(int2str(WEXITSTATUS, 10, TRUE), stderr);
			fputs("\n\n\n", stderr);
		}
		return WEXITSTATUS;
	}
	else if (WIFSIGNALED)
	{
		fputc('\n', stderr);
		fputs(program, stderr);
		fputs(" abnormal termination, signal number = ", stderr);
		fputs(int2str(WTERMSIG, 10, TRUE), stderr);
		fputc('\n', stderr);
		if(WCOREDUMP) fputs("core dumped\n", stderr);
		return WTERMSIG;
	}
	else if(WIFSTOPPED)
	{
		fputc('\n', stderr);
		fputs(program, stderr);
		fputs(" child stopped, signal number = ", stderr);
		fputs(int2str(WEXITSTATUS, 10, TRUE), stderr);
		fputc('\n', stderr);
		return WEXITSTATUS;
	}

	fputc('\n', stderr);
	fputs(program, stderr);
	fputs(" :: something crazy happened with execve\nI'm just gonna get the hell out of here\n", stderr);
	exit(EXIT_FAILURE);
}

/* Execute program and check for error */
void execute(FILE* script, char** argv)
{
	int status = _execute(script, argv);

	if(STRICT == TRUE && (0 != status))
	{
		/* Clearly the script hit an issue that should never have happened */
		fputs("Subprocess error ", stderr);
		fputs(int2str(status, 10, TRUE), stderr);
		fputs("\nABORTING HARD\n", stderr);
		exit(EXIT_FAILURE);
	}
}

/* Execute program */
int _execute(FILE* script, char** argv)
{
	/* Run the command */
	/* rc = return code */
	int rc;
	/* exec without forking */
	int exec = FALSE;

	/* Actually do the execution */
	if(is_envar(token->value) == TRUE)
	{
		add_envar();
		return 0;
	}
	else if(match(token->value, "cd"))
	{
		rc = cd();

		if(STRICT)
		{
			require(rc == SUCCESS, "cd failed!\n");
		}

		return 0;
	}
	else if(match(token->value, "set"))
	{
		rc = set();

		if(STRICT)
		{
			require(rc == SUCCESS, "set failed!\n");
		}

		return 0;
	}
	else if(match(token->value, "alias"))
	{
		add_alias();
		return 0;
	}
	else if(match(token->value, "pwd"))
	{
		rc = pwd();

		if(STRICT)
		{
			require(rc == SUCCESS, "pwd failed!\n");
		}

		return 0;
	}
	else if(match(token->value, "echo"))
	{
		echo();
		return 0;
	}
	else if(match(token->value, "unset"))
	{
		unset();
		return 0;
	}
	else if(match(token->value, "exec"))
	{
		token = token->next; /* Skip the actual exec */
		exec = TRUE;
	}
	else if(match(token->value, "if"))
	{
		if_cmd(script, argv);
		return 0;
	}
	else if(match(token->value, "then"))
	{
		/* ignore */
		return 0;
	}
	else if(match(token->value, "else"))
	{
		/* ignore */
		return 0;
	}
	else if(match(token->value, "fi"))
	{
		/* ignore */
		return 0;
	}

	/* If it is not a builtin, run it as an executable */
	int status; /* i.e. return code */
	char** array;
	char** envp;
	/* Get the full path to the executable */
	char* program = find_executable(token->value);

	/* Check we can find the executable */
	if(NULL == program)
	{
		if(STRICT == TRUE)
		{
			fputs("WHILE EXECUTING ", stderr);
			fputs(token->value, stderr);
			fputs(" NOT FOUND!\nABORTING HARD\n", stderr);
			exit(EXIT_FAILURE);
		}

		/* If we are not strict simply return */
		return 0;
	}

	int f = 0;

#ifdef __uefi__
	array = list_to_array(token);
	envp = list_to_array(env);
	return spawn(program, array, envp);
#else
	if(!exec)
	{
		f = fork();
	}

	/* Ensure fork succeeded */
	if(f == -1)
	{
		fputs("WHILE EXECUTING ", stderr);
		fputs(token->value, stderr);
		fputs(" fork() FAILED\nABORTING HARD\n", stderr);
		exit(EXIT_FAILURE);
	}
	else if(f == 0)
	{
		/* Child */
		/**************************************************************
		 * Fuzzing produces random stuff; we don't want it running    *
		 * dangerous commands. So we just don't execve.               *
		 * But, we still do the list_to_array calls to check for      *
		 * segfaults.                                                 *
		 **************************************************************/
		array = list_to_array(token);
		envp = list_to_array(env);

		if(FALSE == FUZZING)
		{
			/* We are not fuzzing */
			/* execve() returns only on error */
			execve(program, array, envp);
		}

		/* Prevent infinite loops */
		_exit(EXIT_FAILURE);
	}

	/* Otherwise we are the parent */
	/* And we should wait for it to complete */
	waitpid(f, &status, 0);
	return what_exit(program, status);
#endif
}

int collect_command(FILE* script, char** argv)
{
	command_done = FALSE;
	/* Initialize token */
	struct Token* n;
	n = calloc(1, sizeof(struct Token));
	require(n != NULL, "Memory initialization of token in collect_command failed\n");
	char* s = calloc(MAX_STRING, sizeof(char));
	require(s != NULL, "Memory initialization of token in collect_command failed\n");
	token = n;
	int index = 0;
	int alias_index;
	char* alias_string;

	/* Get the tokens */
	while(command_done == FALSE)
	{
		index = collect_token(script, s, index);
		/* Don't allocate another node if the current one yielded nothing, OR
		 * if we are done.
		 */

		if(match(s, ""))
		{
			continue;
		}

		alias_string = alias_lookup(s);
		alias_index = 0;
		do
		{
			if(alias_string != NULL)
			{
				alias_index = collect_alias_token(alias_string, s, alias_index);
			}

			/* add to token */
			n->value = s;
			s = calloc(MAX_STRING, sizeof(char));
			require(s != NULL, "Memory initialization of next token node in collect_command failed\n");
			/* Deal with variables */
			handle_variables(argv, n);

			/* If the variable expands into nothing */
			if(match(n->value, " "))
			{
				n->value = NULL;
				continue;
			}

			/* Prepare for next loop */
			n->next = calloc(1, sizeof(struct Token));
			require(n->next != NULL, "Memory initialization of next token node in collect_command failed\n");
			n = n->next;
		}
		while(alias_index != 0);
	}

	/* -1 means the script is done */
	if(EOF == index)
	{
		return index;
	}

	/* Output the command if verbose is set */
	/* Also if there is nothing in the command skip over */
	if(VERBOSE && !match(token->value, "") && !match(token->value, NULL))
	{
		n = token;
		fputs(" +>", stdout);

		while(n != NULL)
		{
			/* Print out each token token */
			fputs(" ", stdout);

			/* M2-Planet doesn't let us do this in the while */
			if(n->value != NULL)
			{
				if(!match(n->value, ""))
				{
					fputs(n->value, stdout);
				}
			}

			n = n->next;
		}

		fputc('\n', stdout);
		fflush(stdout);
	}

	return index;
}

/* Function for executing our programs with desired arguments */
void run_script(FILE* script, char** argv)
{
	int index;

	while(TRUE)
	{
		/*
		 * Tokens has to be reset each time, as we need a new linked-list for
		 * each line.
		 * See, the program flows like this as a high level overview:
		 * Get line -> Sanitize line and perform variable replacement etc ->
		 * Execute line -> Next.
		 * We don't need the previous lines once they are done with, so tokens
		 * are hence for each line.
		 */
		index = collect_command(script, argv);

		/* -1 means the script is done */
		if(EOF == index)
		{
			break;
		}

		if(0 == index)
		{
			continue;
		}

		/* Stuff to exec */
		execute(script, argv);
	}
}

/* Function to populate env */
void populate_env(char** envp)
{
	/* You can't populate a NULL environment */
	if(NULL == envp)
	{
		return;
	}

	/* avoid empty arrays */
	int max = array_length(envp);

	if(0 == max)
	{
		return;
	}

	/* Initialize env and n */
	env = calloc(1, sizeof(struct Token));
	require(env != NULL, "Memory initialization of env failed\n");
	struct Token* n;
	n = env;
	int i;
	int j;
	int k;
	char* envp_line;

	for(i = 0; i < max; i = i + 1)
	{
		n->var = calloc(MAX_STRING, sizeof(char));
		require(n->var != NULL, "Memory initialization of n->var in population of env failed\n");
		n->value = calloc(MAX_STRING, sizeof(char));
		require(n->value != NULL, "Memory initialization of n->var in population of env failed\n");
		j = 0;
		/*
		 * envp is weird.
		 * When referencing envp[i]'s characters directly, they were all jumbled.
		 * So just copy envp[i] to envp_line, and work with that - that seems
		 * to fix it.
		 */
		envp_line = calloc(MAX_STRING, sizeof(char));
		require(envp_line != NULL, "Memory initialization of envp_line in population of env failed\n");
		require(strlen(envp[i]) < MAX_STRING, "Environment variable exceeds length restriction\n");
		strcpy(envp_line, envp[i]);

		while(envp_line[j] != '=')
		{
			/* Copy over everything up to = to var */
			n->var[j] = envp_line[j];
			j = j + 1;
		}

		/* If we get strange input, we need to ignore it */
		if(n->var == NULL)
		{
			continue;
		}

		j = j + 1; /* Skip over = */
		k = 0; /* As envp[i] will continue as j but n->value begins at 0 */

		while(envp_line[j] != 0)
		{
			/* Copy everything else to value */
			n->value[k] = envp_line[j];
			j = j + 1;
			k = k + 1;
		}

		/* Sometimes, we get lines like VAR=, indicating nothing is in the variable */
		if(n->value == NULL)
		{
			n->value = "";
		}

		/* Advance to next part of linked list */
		n->next = calloc(1, sizeof(struct Token));
		require(n->next != NULL, "Memory initialization of n->next in population of env failed\n");
		n = n->next;
	}

	/* Get rid of node on the end */
	n = NULL;
	/* Also destroy the n->next reference */
	n = env;

	while(n->next->var != NULL)
	{
		n = n->next;
	}

	n->next = NULL;
}

int main(int argc, char** argv, char** envp)
{
	VERBOSE = FALSE;
	VERBOSE_EXIT = FALSE;
	STRICT = TRUE;
	FUZZING = FALSE;
	WARNINGS = FALSE;
	char* filename = "kaem.run";
	FILE* script = NULL;
	/* Initalize structs */
	token = calloc(1, sizeof(struct Token));
	require(token != NULL, "Memory initialization of token failed\n");
	if(NULL != argv[0]) KAEM_BINARY = argv[0];
	else KAEM_BINARY = "./bin/kaem";
	int i = 1;

	/* Loop over arguments */
	while(i <= argc)
	{
		if(NULL == argv[i])
		{
			/* Ignore the argument */
			i = i + 1;
		}
		else if(match(argv[i], "-h") || match(argv[i], "--help"))
		{
			/* Help information */
			fputs("Usage: ", stdout);
			fputs(argv[0], stdout);
			fputs(" [-h | --help] [-V | --version] [--file filename | -f filename] [-i | --init-mode] [-v | --verbose] [--non-strict] [--warn] [--fuzz]\n", stdout);
			exit(EXIT_SUCCESS);
		}
		else if(match(argv[i], "-f") || match(argv[i], "--file"))
		{
			/* Set the filename */
			if(argv[i + 1] != NULL)
			{
				filename = argv[i + 1];
			}

			i = i + 2;
		}
		else if(match(argv[i], "-i") || match(argv[i], "--init-mode"))
		{
			/* init mode does not populate env */
			INIT_MODE = TRUE;
			i = i + 1;
		}
		else if(match(argv[i], "-V") || match(argv[i], "--version"))
		{
			/* Output version */
			fputs("kaem version 1.5.0\n", stdout);
			exit(EXIT_SUCCESS);
		}
		else if(match(argv[i], "-v") || match(argv[i], "--verbose"))
		{
			/* Set verbose */
			VERBOSE = TRUE;
			i = i + 1;
		}
		else if(match(argv[i], "--strict"))
		{
			/* it is a NOP */
			STRICT = TRUE;
			i = i + 1;
		}
		else if(match(argv[i], "--non-strict"))
		{
			/* Set strict */
			STRICT = FALSE;
			i = i + 1;
		}
		else if(match(argv[i], "--warn"))
		{
			/* Set warnings */
			WARNINGS = TRUE;
			i = i + 1;
		}
		else if(match(argv[i], "--fuzz"))
		{
			/* Set fuzzing */
			FUZZING = TRUE;
			i = i + 1;
		}
		else if(match(argv[i], "--show-exit-codes"))
		{
			/* show exit codes */
			VERBOSE_EXIT = TRUE;
			i = i + 1;
		}
		else if(match(argv[i], "--"))
		{
			/* Nothing more after this */
			break;
		}
		else
		{
			/* We don't know this argument */
			fputs("UNKNOWN ARGUMENT\n", stdout);
			exit(EXIT_FAILURE);
		}
	}

	/* Populate env */
	if(INIT_MODE == FALSE)
	{
		populate_env(envp);
	}

	/* make sure SHELL is set */
	if(NULL == env_lookup("SHELL"))
	{
		struct Token* shell = calloc(1, sizeof(struct Token));
		require(NULL != shell, "unable to create SHELL environment variable\n");
		shell->next = env;
		shell->var = "SHELL";
		shell->value= KAEM_BINARY;
		env = shell;
	}

	/* Populate PATH variable
	 * We don't need to calloc() because env_lookup() does this for us.
	 */
	PATH = env_lookup("PATH");
	/* Populate USERNAME variable */
	char* USERNAME = env_lookup("LOGNAME");

	/* Handle edge cases */
	if((NULL == PATH) && (NULL == USERNAME))
	{
		/* We didn't find either of PATH or USERNAME -- use a generic PATH */
		PATH = calloc(MAX_STRING, sizeof(char));
		require(PATH != NULL, "Memory initialization of PATH failed\n");
		strcpy(PATH, "/root/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin");
	}
	else if(NULL == PATH)
	{
		/* We did find a username but not a PATH -- use a generic PATH but with /home/USERNAME */
		PATH = calloc(MAX_STRING, sizeof(char));
		PATH = strcat(PATH, "/home/");
		PATH = strcat(PATH, USERNAME);
		PATH = strcat(PATH, "/bin:/usr/local/bin:/usr/bin:/bin:/usr/local/games:/usr/games");
	}

	/* Open the script */
	script = fopen(filename, "r");

	if(NULL == script)
	{
		fputs("The file: ", stderr);
		fputs(filename, stderr);
		fputs(" can not be opened!\n", stderr);
		exit(EXIT_FAILURE);
	}

	/* Run the commands */
	run_script(script, argv);
	/* Cleanup */
	fclose(script);
	return EXIT_SUCCESS;
}
