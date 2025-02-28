/*
 * Copyright (C) 2020 fosslinux
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
#include <string.h>
#include "kaem.h"

/* Prototypes from other files */
int array_length(char** array);
char* env_lookup(char* variable);

/*
 * VARIABLE HANDLING FUNCTIONS
 */

/* Substitute a variable into n->value */
int run_substitution(char* var_name, struct Token* n)
{
	char* value = env_lookup(var_name);
	/* If there is nothing to substitute, don't substitute anything! */
	if(value != NULL)
	{
		char* s = calloc(MAX_STRING, sizeof(char));
		s = strcat(s, n->value);
		s = strcat(s, value);
		n->value = s;
		return TRUE;
	}
	return FALSE;
}

/* Handle ${var:-text} format of variables - i.e. ifset format */
int variable_substitute_ifset(char* input, struct Token* n, int index)
{
	/*
	 * In ${var:-text} format, we evaluate like follows.
	 * If var is set as an envar, then we substitute the contents of that
	 * envar. If it is not set, we substitute alternative text.
	 *
	 * In this function, we assume that input is the raw token,
	 * n->value is everything already done in variable_substitute,
	 * index is where we are up to in input. offset is for n->value.
	 */

	/*
	 * Check if we should even be performing this function.
	 * We perform this function when we come across ${var:-text} syntax.
	 */
	int index_old = index;
	int perform = FALSE;
	int input_length = strlen(input);
	while(index < input_length)
	{ /* Loop over each character */
		if(input[index] == ':' && input[index + 1] == '-')
		{ /* Yes, this is (most likely) ${var:-text} format. */
			perform = TRUE;
			break;
		}
		index = index + 1;
	}

	/* Don't perform it if we shouldn't */
	if(perform == FALSE) return index_old;
	index = index_old;

	/*
	 * Get offset.
	 * offset is the difference between the index of the variable we write to
	 * in the following blocks and input.
	 * This stays relatively constant.
	 */
	int offset = index;

	/* Get the variable name */
	char* var_name = calloc(MAX_STRING, sizeof(char));
	require(var_name != NULL, "Memory initialization of var_name in variable_substitute_ifset failed\n");
	while(input[index] != ':')
	{ /* Copy into var_name until :- */
		var_name[index - offset] = input[index];
		index = index + 1;
	}

	/* Skip over :- */
	index = index + 2;
	offset = index;

	/* Get the alternative text */
	char* text = calloc(MAX_STRING, sizeof(char));
	require(text != NULL, "Memory initialization of text in variable_substitute_ifset failed\n");
	while(input[index] != '}')
	{ /* Copy into text until } */
		require(input_length > index, "IMPROPERLY TERMINATED VARIABLE\nABORTING HARD\n");
		text[index - offset] = input[index];
		index = index + 1;
	}

	/* Do the substitution */
	if(run_substitution(var_name, n) == FALSE)
	{ /* The variable was not found. Substitute the alternative text. */
		char* s = calloc(MAX_STRING, sizeof(char));
		s = strcat(s, n->value);
		s = strcat(s, text);
		n->value = s;
	}

	return index;
}

/* Controls substitution for ${variable} and derivatives */
int variable_substitute(char* input, struct Token* n, int index)
{
	/* NOTE: index is the pos of input */
	index = index + 1; /* We don't want the { */

	/*
	 * Check for "special" types
	 * If we do find a special type we delegate the substitution to it
	 * and return here; as we are done... there's nothing more do do in
	 * that case.
	 */
	int index_old = index;
	index = variable_substitute_ifset(input, n, index);
	if(index != index_old) return index;

	/* Reset index */
	index = index_old;

	/*
	 * If we reach here it is a normal substitution
	 * Let's do it!
	 */
	/* Initialize var_name and offset */
	char* var_name = calloc(MAX_STRING, sizeof(char));
	require(var_name != NULL, "Memory initialization of var_name in variable_substitute failed\n");
	int offset = index;

	/* Get the variable name */
	int substitute_done = FALSE;
	char c;
	while(substitute_done == FALSE)
	{
		c = input[index];
		require(MAX_STRING > index, "LINE IS TOO LONG\nABORTING HARD\n");
		if(EOF == c || '\n' == c || index > strlen(input))
		{ /* We never should hit EOF, EOL or run past the end of the line 
			 while collecting a variable */
			fputs("IMPROPERLY TERMINATED VARIABLE!\nABORTING HARD\n", stderr);
			exit(EXIT_FAILURE);
		}
		else if('\\' == c)
		{ /* Drop the \ - poor mans escaping. */
			index = index + 1;
		}
		else if('}' == c)
		{ /* End of variable name */
			substitute_done = TRUE;
		}
		else
		{
			var_name[index - offset] = c;
			index = index + 1;
		}
	}

	/* Substitute the variable */
	run_substitution(var_name, n);

	return index;
}

/* Function to concatenate all command line arguments */
void variable_all(char** argv, struct Token* n)
{
	fflush(stdout);
	/* index refernences the index of n->value, unlike other functions */
	int index = 0;
	int argv_length = array_length(argv);
	int i = 0;
	char* argv_element = calloc(MAX_STRING, sizeof(char));
	char* hold = argv[i];
	n->value = argv_element;
	/* Assuming the form kaem -f script or kaem -f script -- 123 we want matching results to bash, so skip the kaem, -f and script */
	while(!match("--", hold))
	{
		i = i + 1;
		hold = argv[i];
		if(argv_length == i) break;
	}

	/* put i = i + 1 in the for initialization to skip past the -- */
	for(; i < argv_length; i = i + 1)
	{
		/* Ends up with (n->value) (argv[i]) */
		/* If we don't do this we get jumbled results in M2-Planet */
		hold = argv[i];
		strcpy(argv_element + index, hold);
		index = index + strlen(hold);

		/* Add space on the end */
		n->value[index] = ' ';
		index = index + 1;
	}
	/* Remove trailing space */
	index = index - 1;
	n->value[index] = 0;
}

/* Function controlling substitution of variables */
void handle_variables(char** argv, struct Token* n)
{
	/* NOTE: index is the position of input */
	int index = 0;

	/* Create input */
	char* input = calloc(MAX_STRING, sizeof(char));
	require(input != NULL, "Memory initialization of input in collect_variable failed\n");
	strcpy(input, n->value);
	/* Reset n->value */
	n->value = calloc(MAX_STRING, sizeof(char));
	require(n->value != NULL, "Memory initialization of n->value in collect_variable failed\n");

	/* Copy everything up to the $ */
	/*
	 * TODO: Not need allocation of input before this check if there is no
	 * variable in it.
	 */
	while(input[index] != '$')
	{
		if(input[index] == 0)
		{ /* No variable in it */
			n->value = input;
			return; /* We don't need to do anything more */
		}
		n->value[index] = input[index];
		index = index + 1;
	}

	/* Must be outside the loop */
	int offset;

substitute:
	index = index + 1; /* We are uninterested in the $ */
	/* Run the substitution */
	if(input[index] == '{')
	{ /* Handle everything ${ related */
		index = variable_substitute(input, n, index);
		index = index + 1; /* We don't want the closing } */
	}
	else if(input[index] == '@')
	{ /* Handles $@ */
		index = index + 1; /* We don't want the @ */
		variable_all(argv, n);
	}
	else
	{ /* We don't know that */
		fputs("IMPROPERLY USED VARIABLE!\nOnly ${foo} and $@ format are accepted at this time.\nABORTING HARD\n", stderr);
		exit(EXIT_FAILURE);
	}

	offset = strlen(n->value) - index;
	/* Copy everything from the end of the variable to the end of the token */
	while(input[index] != 0)
	{
		if(input[index] == '$')
		{ /* We have found another variable */
			fflush(stdout);
			goto substitute;
		}
		n->value[index + offset] = input[index];
		index = index + 1;
	}
}
