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
#include <unistd.h>
#include <sys/wait.h>
#define MAX_ARRAY 256

char* env_lookup(char* variable);

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

/* Find the full path to an executable */
char* find_executable(char* name)
{
	char* PATH = env_lookup("PATH");
	require(NULL != PATH, "No PATH found\nAborting\n");
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
		trial_length = strlen(trial);
		require(trial_length < MAX_STRING, "COMMAND TOO LONG!\nABORTING HARD\n");
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

void sanity_command_check(char** array)
{
	int i = 0;
	char* s = array[0];
	while(NULL != s)
	{
		fputs(s, stderr);
		fputc(' ', stderr);
		i = i + 1;
		s = array[i];
	}
	fputc('\n', stderr);
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

	if(DEBUG_LEVEL > 6)
	{
		fputs("in what_exit with char* program of: ", stderr);
		fputs(program, stderr);
		fputs("\nAnd int status of: 0x", stderr);
		fputs(int2str(status, 16, FALSE), stderr);
		fputc('\n', stderr);
	}

	int WIFEXITED = !(status & 0x7F);
	int WEXITSTATUS = (status & 0xFF00) >> 8;
	int WTERMSIG = status & 0x7F;
	int WCOREDUMP = status & 0x80;
	int WIFSIGNALED = !((0x7F == WTERMSIG) || (0 == WTERMSIG));
	int WIFSTOPPED = ((0x7F == WTERMSIG) && (0 == WCOREDUMP));

	if(WIFEXITED)
	{
		if(DEBUG_LEVEL > 2)
		{
			fputc('\n', stderr);
			fputs(program, stderr);
			fputs(" normal termination, exit status = ", stderr);
			fputs(int2str(WEXITSTATUS, 10, TRUE), stderr);
			fputc('\n', stderr);
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


void _execute(char* name, char** array, char** envp)
{
	int status; /* i.e. return code */
	/* Get the full path to the executable */
	char* program = find_executable(name);

	/* Check we can find the executable */
	if(NULL == program)
	{
		fputs("WHILE EXECUTING ", stderr);
		fputs(name, stderr);
		fputs(" NOT FOUND!\nABORTING HARD\n", stderr);
		exit(EXIT_FAILURE);
	}

	sanity_command_check(array);

	int result;
#ifdef __uefi__
	result = spawn(program, array, envp);
#else
	int f = fork();

	/* Ensure fork succeeded */
	if(f == -1)
	{
		fputs("WHILE EXECUTING ", stderr);
		fputs(name, stderr);
		fputs("fork() FAILED\nABORTING HARD\n", stderr);
		exit(EXIT_FAILURE);
	}
	else if(f == 0)
	{
		/* Child */
		/**************************************************************
		 * Fuzzing produces random stuff; we don't want it running    *
		 * dangerous commands. So we just don't execve.               *
		 **************************************************************/
		if(FALSE == FUZZING)
		{
			/* We are not fuzzing */
			/* execve() returns only on error */
			execve(program, array, envp);
			fputs("Unable to execute: ", stderr);
			fputs(program, stderr);
			fputs("\nPlease check file permissions and that it is a valid binary\n", stderr);
		}

		/* Prevent infinite loops */
		_exit(EXIT_FAILURE);
	}

	/* Otherwise we are the parent */
	/* And we should wait for it to complete */
	waitpid(f, &status, 0);

	result = what_exit(program, status);
#endif
	if(0 != result)
	{
		fputs("Subprocess: ", stderr);
		fputs(program, stderr);
		fputs(" error\nAborting for safety\n", stderr);
		exit(result);
	}
}

void insert_array(char** array, int index, char* string)
{
	int size = strlen(string);
	array[index] = calloc(size+2, sizeof(char));
	strcpy(array[index], string);
}


void spawn_hex2(char* input, char* output, char* architecture, char** envp, int debug)
{
	char* hex2;
#ifdef __uefi__
	hex2 = "hex2.efi";
#else
	hex2 = "hex2";
#endif

	char* elf_header = calloc(MAX_STRING, sizeof(char));
	elf_header = strcat(elf_header, M2LIBC_PATH);
	elf_header = strcat(elf_header, "/");
	elf_header = strcat(elf_header, architecture);
	if(match("UEFI", OperatingSystem)) elf_header = strcat(elf_header, "/uefi/PE32-");
	else elf_header = strcat(elf_header, "/ELF-");
	elf_header = strcat(elf_header, architecture);
	if(debug)
	{
		elf_header = strcat(elf_header, "-debug.hex2");
	}
	else
	{
		elf_header = strcat(elf_header, ".hex2");
	}

	fputs("# starting hex2 linking\n", stdout);
	char** array = calloc(MAX_ARRAY, sizeof(char*));
	insert_array(array, 0, hex2);
	insert_array(array, 1, "--file");
	insert_array(array, 2, elf_header);
	insert_array(array, 3, "--file");
	insert_array(array, 4, input);
	insert_array(array, 5, "--output");
	insert_array(array, 6, output);
	insert_array(array, 7, "--architecture");
	insert_array(array, 8, architecture);
	insert_array(array, 9, "--base-address");
	insert_array(array, 10, BASEADDRESS);
	if(ENDIAN)
	{
		insert_array(array, 11, "--big-endian");
	}
	else
	{
		insert_array(array, 11, "--little-endian");
	}
	_execute(hex2, array, envp);
}


void spawn_M1(char* input, char* debug_file, char* output, char* architecture, char** envp, int debug_flag)
{
	char* M1;
#ifdef __uefi__
	M1 = "M1.efi";
#else
	M1 = "M1";
#endif

	fputs("# starting M1 assembly\n", stdout);
	char* definitions = calloc(MAX_STRING, sizeof(char));
	definitions = strcat(definitions, M2LIBC_PATH);
	definitions = strcat(definitions, "/");
	definitions = strcat(definitions, architecture);
	definitions = strcat(definitions, "/");
	definitions = strcat(definitions, architecture);
	definitions = strcat(definitions, "_defs.M1");

	char* libc = calloc(MAX_STRING, sizeof(char));
	libc = strcat(libc, M2LIBC_PATH);
	libc = strcat(libc, "/");
	libc = strcat(libc, architecture);

	if(match("UEFI", OperatingSystem))
	{
		libc = strcat(libc, "/uefi/libc-full.M1");
	}
	else if(STDIO_USED)
	{
		libc = strcat(libc, "/libc-full.M1");
	}
	else
	{
		libc = strcat(libc, "/libc-core.M1");
	}

	char** array = calloc(MAX_ARRAY, sizeof(char*));
	insert_array(array, 0, M1);
	insert_array(array, 1, "--file");
	insert_array(array, 2, definitions);
	insert_array(array, 3, "--file");
	insert_array(array, 4, libc);
	insert_array(array, 5, "--file");
	insert_array(array, 6, input);
	if(ENDIAN)
	{
		insert_array(array, 7, "--big-endian");
	}
	else
	{
		insert_array(array, 7, "--little-endian");
	}
	insert_array(array, 8, "--architecture");
	insert_array(array, 9, architecture);

	if(debug_flag)
	{
		insert_array(array, 10, "--file");
		insert_array(array, 11, debug_file);
		insert_array(array, 12, "--output");
		insert_array(array, 13, output);
	}
	else
	{
		insert_array(array, 10, "--output");
		insert_array(array, 11, output);
	}
	_execute(M1, array, envp);
}


void spawn_blood_elf(char* input, char* output, char** envp, int large_flag)
{
	char* blood_elf;
#ifdef __uefi__
	blood_elf = "blood-elf.efi";
#else
	blood_elf = "blood-elf";
#endif

	fputs("# starting Blood-elf stub generation\n", stdout);
	char** array = calloc(MAX_ARRAY, sizeof(char*));
	insert_array(array, 0,blood_elf);
	insert_array(array, 1, "--file");
	insert_array(array, 2, input);
	if(ENDIAN)
	{
		insert_array(array, 3, "--big-endian");
	}
	else
	{
		insert_array(array, 3, "--little-endian");
	}
	insert_array(array, 4, "--output");
	insert_array(array, 5, output);
	if(large_flag) insert_array(array, 6, "--64");
	_execute(blood_elf, array, envp);
}

void spawn_M2(char* input, char* output, char* architecture, char** envp, int debug_flag)
{
	char* M2_Planet;
#ifdef __uefi__
	M2_Planet = "M2-Planet.efi";
#else
	M2_Planet = "M2-Planet";
#endif

	fputs("# starting M2-Planet build\n", stdout);
	char** array = calloc(MAX_ARRAY, sizeof(char*));
	insert_array(array, 0, M2_Planet);
	insert_array(array, 1, "--file");
	insert_array(array, 2, input);
	insert_array(array, 3, "--output");
	insert_array(array, 4, output);
	insert_array(array, 5, "--architecture");
	insert_array(array, 6, architecture);
	if(debug_flag) insert_array(array, 7, "--debug");
	_execute(M2_Planet, array, envp);
}

void spawn_processes(int debug_flag, char* prefix, char* preprocessed_file, char* destination, char** envp)
{
	int large_flag = FALSE;
	if(WORDSIZE > 32) large_flag = TRUE;

	if(match("UEFI", OperatingSystem))
	{
		debug_flag = FALSE;
	}

	char* M2_output = calloc(100, sizeof(char));
	strcpy(M2_output, prefix);
	strcat(M2_output, "/M2-Planet-XXXXXX");
	int i = mkstemp(M2_output);
	if(-1 != i)
	{
		close(i);
		spawn_M2(preprocessed_file, M2_output, Architecture, envp, debug_flag);
	}
	else
	{
		fputs("unable to get a tempfile for M2-Planet output\n", stderr);
		exit(EXIT_FAILURE);
	}

	char* blood_output = "";
	if(debug_flag)
	{
		blood_output = calloc(100, sizeof(char));
		strcpy(blood_output, prefix);
		strcat(blood_output, "/blood-elf-XXXXXX");
		i = mkstemp(blood_output);
		if(-1 != i)
		{
			close(i);
			spawn_blood_elf(M2_output, blood_output, envp, large_flag);
		}
		else
		{
			fputs("unable to get a tempfile for blood-elf output\n", stderr);
			exit(EXIT_FAILURE);
		}
	}

	char* M1_output = calloc(100, sizeof(char));
	strcpy(M1_output, prefix);
	strcat(M1_output, "/M1-macro-XXXXXX");
	i = mkstemp(M1_output);
	if(-1 != i)
	{
		close(i);
		spawn_M1(M2_output, blood_output, M1_output, Architecture, envp, debug_flag);
	}
	else
	{
		fputs("unable to get a tempfile for M1 output\n", stderr);
		exit(EXIT_FAILURE);
	}

	/* We no longer need the M2-Planet tempfile output */
	if(!DIRTY_MODE) remove(M2_output);
	/* Nor the blood-elf output anymore if it exists */
	if(!match("", blood_output))
	{
		if(!DIRTY_MODE) remove(blood_output);
	}

	/* Build the final binary */
	spawn_hex2(M1_output, destination, Architecture, envp, debug_flag);

	/* clean up after ourselves*/
	if(!DIRTY_MODE) remove(M1_output);
}
