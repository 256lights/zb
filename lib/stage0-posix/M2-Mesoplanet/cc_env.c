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

#include"cc.h"
#include <sys/utsname.h>

void init_macro_env(char* sym, char* value, char* source, int num);
char* env_lookup(char* variable);
void clear_string(char* s);

struct utsname* get_uname_data()
{
	struct utsname* unameData = calloc(1, sizeof(struct utsname));
	require(NULL != unameData, "unameData calloc failed\n");
	uname(unameData);
	if(4 <= DEBUG_LEVEL)
	{
		fputs("utsname details: ", stderr);
		fputs(unameData->sysname, stderr);
		fputc(' ', stderr);
		fputs(unameData->machine, stderr);
		fputc('\n', stderr);
	}
	return unameData;
}

void setup_env()
{
	if(2 <= DEBUG_LEVEL) fputs("Starting setup_env\n", stderr);
	char* ARCH;
	if(NULL != Architecture)
	{
		ARCH = Architecture;
	}
	else
	{
		ARCH = NULL;
		struct utsname* unameData = get_uname_data();

		if(match("i386", unameData->machine) ||
		   match("i486", unameData->machine) ||
		   match("i586", unameData->machine) ||
		   match("i686", unameData->machine) ||
		   match("i686-pae", unameData->machine)) ARCH = "x86";
		else if(match("x86_64", unameData->machine)) ARCH = "amd64";
		else ARCH = unameData->machine;
		if(3 <= DEBUG_LEVEL)
		{
			fputs("Architecture selected: ", stderr);
			fputs(ARCH, stderr);
			fputc('\n', stderr);
		}

		/* Check for override */
		char* hold = env_lookup("ARCHITECTURE_OVERRIDE");
		if(NULL != hold)
		{
			ARCH = hold;
			if(3 <= DEBUG_LEVEL)
			{
				fputs("environmental override for ARCH: ", stderr);
				fputs(ARCH, stderr);
				fputc('\n', stderr);
			}
		}
		free(unameData);
	}


	/* Set desired architecture */
	WORDSIZE = 32;
	ENDIAN = FALSE;
	BASEADDRESS = "0x0";
	if(match("knight-native", ARCH))
	{
		if(4 <= DEBUG_LEVEL) fputs("Using knight-native architecture\n", stderr);
		ENDIAN = TRUE;
		Architecture = "knight-native";
	}
	else if(match("knight-posix", ARCH))
	{
		if(4 <= DEBUG_LEVEL) fputs("Using knight-posix architecture\n", stderr);
		ENDIAN = TRUE;
		Architecture = "knight-posix";
	}
	else if(match("x86", ARCH))
	{
		if(4 <= DEBUG_LEVEL) fputs("Using x86 architecture\n", stderr);
		BASEADDRESS = "0x8048000";
		Architecture = "x86";
		init_macro_env("__i386__", "1", "--architecture", 0);
	}
	else if(match("amd64", ARCH))
	{
		if(4 <= DEBUG_LEVEL) fputs("Using amd64 architecture\n", stderr);
		BASEADDRESS = "0x00600000";
		Architecture = "amd64";
		WORDSIZE = 64;
		init_macro_env("__x86_64__", "1", "--architecture", 0);
	}
	else if(match("armv7l", ARCH))
	{
		if(4 <= DEBUG_LEVEL) fputs("Using armv7l architecture\n", stderr);
		BASEADDRESS = "0x10000";
		Architecture = "armv7l";
		init_macro_env("__arm__", "1", "--architecture", 0);
	}
	else if(match("aarch64", ARCH))
	{
		if(4 <= DEBUG_LEVEL) fputs("Using aarch64 architecture\n", stderr);
		BASEADDRESS = "0x400000";
		Architecture = "aarch64";
		WORDSIZE = 64;
		init_macro_env("__aarch64__", "1", "--architecture", 0);
	}
	else if(match("riscv32", ARCH))
	{
		if(4 <= DEBUG_LEVEL) fputs("Using riscv32 architecture\n", stderr);
		BASEADDRESS = "0x600000";
		Architecture = "riscv32";
		init_macro_env("__riscv", "1", "--architecture", 0);
		init_macro_env("__riscv_xlen", "32", "--architecture", 1);
	}
	else if(match("riscv64", ARCH))
	{
		if(4 <= DEBUG_LEVEL) fputs("Using riscv64 architecture\n", stderr);
		BASEADDRESS = "0x600000";
		Architecture = "riscv64";
		WORDSIZE = 64;
		init_macro_env("__riscv", "1", "--architecture", 0);
		init_macro_env("__riscv_xlen", "64", "--architecture", 1);
	}
	else
	{
		fputs("Unknown architecture: ", stderr);
		fputs(ARCH, stderr);
		fputs(" know values are: knight-native, knight-posix, x86, amd64, armv7l, aarch64, riscv32 and riscv64\n", stderr);
		exit(EXIT_FAILURE);
	}


	/* Setup Operating System */
	if(NULL == OperatingSystem)
	{
		OperatingSystem = "Linux";
		if(3 <= DEBUG_LEVEL)
		{
			fputs("Operating System selected: ", stderr);
			fputs(OperatingSystem, stderr);
			fputc('\n', stderr);
		}

		/* Check for override */
		char* hold = env_lookup("OS_OVERRIDE");
		if(NULL != hold)
		{
			OperatingSystem = hold;
			if(3 <= DEBUG_LEVEL)
			{
				fputs("environmental override for OS: ", stderr);
				fputs(OperatingSystem, stderr);
				fputc('\n', stderr);
			}
		}
	}

	if(match("UEFI", OperatingSystem))
	{
		if(4 <= DEBUG_LEVEL) fputs("Using UEFI\n", stderr);
		BASEADDRESS = "0x0";
		OperatingSystem = "UEFI";
		init_macro_env("__uefi__", "1", "--os", 0);
	}

	if(2 <= DEBUG_LEVEL) fputs("setup_env successful\n", stderr);
}

struct Token
{
	/*
	 * For the token linked-list, this stores the token; for the env linked-list
	 * this stores the value of the variable.
	 */
	char* value;
	/*
	 * Used only for the env linked-list. It holds a string containing the
	 * name of the var.
	 */
	char* var;
	/*
	 * This struct stores a node of a singly linked list, store the pointer to
	 * the next node.
	 */
	struct Token* next;
};

struct Token* env;

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
	if(6 <= DEBUG_LEVEL)
	{
		fputs("in token_lookup\nLooking for: ", stderr);
		fputs(variable, stderr);
		fputc('\n', stderr);
	}
	/* Start at the head */
	struct Token* n = token;

	/* Loop over the linked-list */
	while(n != NULL)
	{
		if(15 <= DEBUG_LEVEL)
		{
			fputs(n->var, stderr);
			fputc('\n', stderr);
		}
		if(match(variable, n->var))
		{
			if(6 <= DEBUG_LEVEL) fputs("match found in token_lookup\n", stderr);
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

char* envp_hold;
int envp_index;

void reset_envp_hold()
{
	clear_string(envp_hold);
	envp_index = 0;
}

void push_env_byte(int c)
{
	envp_hold[envp_index] = c;
	envp_index = envp_index + 1;
	require(4096 > envp_index, "Token exceeded 4096 char envp limit\n");
}

struct Token* process_env_variable(char* envp_line, struct Token* n)
{
	struct Token* node = calloc(1, sizeof(struct Token));
	require(node != NULL, "Memory initialization of node failed\n");
	reset_envp_hold();
	int i = 0;

	while(envp_line[i] != '=')
	{
		/* Copy over everything up to = to var */
		push_env_byte(envp_line[i]);
		i = i + 1;
	}

	node->var = calloc(i + 2, sizeof(char));
	require(node->var != NULL, "Memory initialization of n->var in population of env failed\n");
	strcpy(node->var, envp_hold);

	i = i + 1; /* Skip over = */

	reset_envp_hold();
	while(envp_line[i] != 0)
	{
		/* Copy everything else to value */
		push_env_byte(envp_line[i]);
		i = i + 1;
	}

	/* Sometimes, we get lines like VAR=, indicating nothing is in the variable */
	if(0 == strlen(envp_hold))
	{
		node->value = "";
	}
	else
	{
		/* but looks like we got something so, lets use it */
		node->value = calloc(strlen(envp_hold) + 2, sizeof(char));
		require(node->value != NULL, "Memory initialization of n->var in population of env failed\n");
		strcpy(node->value, envp_hold);
	}

	node->next = n;
	return node;
}

void populate_env(char** envp)
{
	if(2 <= DEBUG_LEVEL) fputs("populate_env started\n", stderr);
	/* You can't populate a NULL environment */
	if(NULL == envp)
	{
		if(3 <= DEBUG_LEVEL) fputs("NULL envp\n", stderr);
		return;
	}

	/* avoid empty arrays */
	int max = array_length(envp);

	if(0 == max)
	{
		if(3 <= DEBUG_LEVEL) fputs("Empty envp\n", stderr);
		return;
	}

	/* Initialize env and n */
	env = NULL;
	int i;
	envp_hold = calloc(4096, sizeof(char));
	require(envp_hold != NULL, "Memory initialization of envp_hold in population of env failed\n");
	char* envp_line = calloc(4096, sizeof(char));
	require(envp_line != NULL, "Memory initialization of envp_line in population of env failed\n");

	if(3 <= DEBUG_LEVEL) fputs("starting env loop\n", stderr);
	for(i = 0; i < max; i = i + 1)
	{
		/*
		 * envp is weird.
		 * When referencing envp[i]'s characters directly, they were all jumbled.
		 * So just copy envp[i] to envp_line, and work with that - that seems
		 * to fix it.
		 */
		clear_string(envp_line);
		require(4096 > strlen(envp[i]), "envp line exceeds 4096byte limit\n");
		strcpy(envp_line, envp[i]);

		if(9 <= DEBUG_LEVEL)
		{
			fputs("trying envp_line: ", stderr);
			fputs(envp_line, stderr);
			fputc('\n', stderr);
		}

		env = process_env_variable(envp_line, env);

		if(9 <= DEBUG_LEVEL)
		{
			fputs("got var of: ", stderr);
			fputs(env->var, stderr);
			fputs("\nAnd value of: ", stderr);
			fputs(env->value, stderr);
			fputc('\n', stderr);
		}
	}

	free(envp_line);
	free(envp_hold);
	if(3 <= DEBUG_LEVEL)
	{
		fputs("\n\nenv loop successful\n", stderr);
		fputs(int2str(i, 10, FALSE), stderr);
		fputs(" envp records processed\n\n", stderr);
	}

	require(NULL != env, "can't have an empty environment from the creation of a non-null environment\n");
	if(2 <= DEBUG_LEVEL) fputs("populate_env successful\n", stderr);
}
