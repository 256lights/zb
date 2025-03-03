/* Copyright (C) 2020 Jeremiah Orians
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

#ifndef _UNISTD_C
#define _UNISTD_C
#include <sys/utsname.h>
#define NULL 0
#define __PATH_MAX 4096

void* malloc(unsigned size);

int access(char* pathname, int mode)
{
	asm("LOAD R0 R14 0"
	    "LOAD R1 R14 4"
	    "SYS_ACCESS");
}

int chdir(char* path)
{
	asm("LOAD R0 R14 0"
	    "SYS_CHDIR");
}

int fchdir(int fd)
{
	asm("LOAD R0 R14 0"
	    "SYS_FCHDIR");
}

void _exit(int value);

int fork()
{
	asm("SYS_FORK");
}


int waitpid (int pid, int* status_ptr, int options)
{
	/* Uses wait4 with struct rusage *ru set to NULL */
	asm("LOAD R0 R14 0"
	    "LOAD R1 R14 4"
	    "LOAD R2 R14 8"
	    "FALSE R3"
	    "SYS_WAIT4");
}


int execve(char* file_name, char** argv, char** envp)
{
	asm("LOAD R0 R14 0"
	    "LOAD R1 R14 4"
	    "LOAD R2 R14 8"
	    "SYS_EXECVE");
}

int read(int fd, char* buf, unsigned count)
{
	asm("LOAD R0 R14 0"
	    "LOAD R1 R14 4"
	    "LOAD R2 R14 8"
	    "SYS_READ");
}

int write(int fd, char* buf, unsigned count)
{
	asm("LOAD R0 R14 0"
	    "LOAD R1 R14 4"
	    "LOAD R2 R14 8"
	    "SYS_WRITE");
}

int lseek(int fd, int offset, int whence)
{
	asm("LOAD R0 R14 0"
	    "LOAD R1 R14 4"
	    "LOAD R2 R14 8"
	    "SYS_LSEEK");
}

int close(int fd)
{
	asm("LOAD R0 R14 0"
	    "SYS_CLOSE");
}


int unlink (char* filename)
{
	asm("LOAD R0 R14 0"
	    "SYS_UNLINK");
}


int _getcwd(char* buf, int size)
{
	asm("LOAD R0 R14 0"
	    "LOAD R1 R14 4"
	    "SYS_GETCWD");
}


char* getcwd(char* buf, unsigned size)
{
	int c = _getcwd(buf, size);
	if(0 == c) return NULL;
	return buf;
}


char* getwd(char* buf)
{
	return getcwd(buf, __PATH_MAX);
}


char* get_current_dir_name()
{
	return getcwd(malloc(__PATH_MAX), __PATH_MAX);
}

/********************************************************************************
 * All memory past the text segment and stack are always allocated to heap      *
 * purposes and thus no syscalls are needed for brk                             *
 ********************************************************************************/
int brk(void *addr)
{
	asm("LOAD R0 R14 0"
	    "ADDU R0 R12 R0"
	    "SWAP R0 R12");
}

int uname(struct utsname* unameData)
{
	asm("LOAD R0 R14 0"
	    "SYS_UNAME");
}

#endif
