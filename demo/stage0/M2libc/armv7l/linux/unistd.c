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
	asm("!4 R0 SUB R12 ARITH_ALWAYS"
	    "!0 R0 LOAD32 R0 MEMORY"
	    "!8 R1 SUB R12 ARITH_ALWAYS"
	    "!0 R1 LOAD32 R1 MEMORY"
	    "!33 R7 LOADI8_ALWAYS"
	    "SYSCALL_ALWAYS");
}

int chdir(char* path)
{
	asm("!4 R0 SUB R12 ARITH_ALWAYS"
	    "!0 R0 LOAD32 R0 MEMORY"
	    "!12 R7 LOADI8_ALWAYS"
	    "SYSCALL_ALWAYS");
}

int fchdir(int fd)
{
	asm("!4 R0 SUB R12 ARITH_ALWAYS"
	    "!0 R0 LOAD32 R0 MEMORY"
	    "!133 R7 LOADI8_ALWAYS"
	    "SYSCALL_ALWAYS");
}

void _exit(int value);

int fork()
{
	asm("!2 R7 LOADI8_ALWAYS"
	    "SYSCALL_ALWAYS");
}


int waitpid (int pid, int* status_ptr, int options)
{
	asm("!114 R7 LOADI8_ALWAYS"
	    "!12 R2 SUB R12 ARITH_ALWAYS"
	    "!0 R2 LOAD32 R2 MEMORY"
	    "!8 R1 SUB R12 ARITH_ALWAYS"
	    "!0 R1 LOAD32 R1 MEMORY"
	    "!4 R0 SUB R12 ARITH_ALWAYS"
	    "!0 R0 LOAD32 R0 MEMORY"
	    "SYSCALL_ALWAYS");
}


int execve(char* file_name, char** argv, char** envp)
{
	asm("!11 R7 LOADI8_ALWAYS"
	    "!12 R2 SUB R12 ARITH_ALWAYS"
	    "!0 R2 LOAD32 R2 MEMORY"
	    "!8 R1 SUB R12 ARITH_ALWAYS"
	    "!0 R1 LOAD32 R1 MEMORY"
	    "!4 R0 SUB R12 ARITH_ALWAYS"
	    "!0 R0 LOAD32 R0 MEMORY"
	    "SYSCALL_ALWAYS");
}

int read(int fd, char* buf, unsigned count)
{
	asm("!3 R7 LOADI8_ALWAYS"
	    "!12 R2 SUB R12 ARITH_ALWAYS"
	    "!0 R2 LOAD32 R2 MEMORY"
	    "!8 R1 SUB R12 ARITH_ALWAYS"
	    "!0 R1 LOAD32 R1 MEMORY"
	    "!4 R0 SUB R12 ARITH_ALWAYS"
	    "!0 R0 LOAD32 R0 MEMORY"
	    "SYSCALL_ALWAYS");
}

int write(int fd, char* buf, unsigned count)
{
	asm("!4 R7 LOADI8_ALWAYS"
	    "!12 R2 SUB R12 ARITH_ALWAYS"
	    "!0 R2 LOAD32 R2 MEMORY"
	    "!8 R1 SUB R12 ARITH_ALWAYS"
	    "!0 R1 LOAD32 R1 MEMORY"
	    "!4 R0 SUB R12 ARITH_ALWAYS"
	    "!0 R0 LOAD32 R0 MEMORY"
	    "SYSCALL_ALWAYS");
}

int lseek(int fd, int offset, int whence)
{
	asm("!19 R7 LOADI8_ALWAYS"
	    "!12 R2 SUB R12 ARITH_ALWAYS"
	    "!0 R2 LOAD32 R2 MEMORY"
	    "!8 R1 SUB R12 ARITH_ALWAYS"
	    "!0 R1 LOAD32 R1 MEMORY"
	    "!4 R0 SUB R12 ARITH_ALWAYS"
	    "!0 R0 LOAD32 R0 MEMORY"
	    "SYSCALL_ALWAYS");
}


int close(int fd)
{
	asm("!4 R0 SUB R12 ARITH_ALWAYS"
	    "!6 R7 LOADI8_ALWAYS"
	    "SYSCALL_ALWAYS");
}


int unlink (char* filename)
{
	asm("!4 R0 SUB R12 ARITH_ALWAYS"
	    "!10 R7 LOADI8_ALWAYS"
	    "SYSCALL_ALWAYS");
}


int _getcwd(char* buf, int size)
{
	asm("!4 R0 SUB R12 ARITH_ALWAYS"
	    "!0 R0 LOAD32 R0 MEMORY"
	    "!8 R1 SUB R12 ARITH_ALWAYS"
	    "!0 R1 LOAD32 R1 MEMORY"
	    "!183 R7 LOADI8_ALWAYS"
	    "SYSCALL_ALWAYS");
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


int brk(void *addr)
{
	asm("!4 R0 SUB R12 ARITH_ALWAYS"
	    "!0 R0 LOAD32 R0 MEMORY"
	    "!45 R7 LOADI8_ALWAYS"
	    "SYSCALL_ALWAYS");
}

int uname(struct utsname* unameData)
{
	asm("!122 R7 LOADI8_ALWAYS"
	    "!4 R0 SUB R12 ARITH_ALWAYS"
	    "!0 R0 LOAD32 R0 MEMORY"
	    "SYSCALL_ALWAYS");
}

int unshare(int flags)
{
	asm("!4 R0 SUB R12 ARITH_ALWAYS"
	    "!0 R0 LOAD32 R0 MEMORY"
	    "!337 R7 LOADI8_ALWAYS"
	    "SYSCALL_ALWAYS");
}

int geteuid()
{
	asm("!201 R7 LOADI8_ALWAYS"
	    "SYSCALL_ALWAYS");
}

int getegid()
{
	asm("!202 R7 LOADI8_ALWAYS"
	   "SYSCALL_ALWAYS");
}

int chroot(char const *path)
{
	asm("!4 R0 SUB R12 ARITH_ALWAYS"
	   "!61 R7 LOADI8_ALWAYS"
	   "SYSCALL_ALWAYS");
}

int mount(char const *source, char const *target, char const *filesystemtype, SCM mountflags, void const *data)
{
	asm("!4 R0 SUB R12 ARITH_ALWAYS"
	    "!0 R0 LOAD32 R0 MEMORY"
	    "!8 R1 SUB R12 ARITH_ALWAYS"
	    "!0 R1 LOAD32 R1 MEMORY"
	    "!12 R2 SUB R12 ARITH_ALWAYS"
	    "!0 R2 LOAD32 R2 MEMORY"
	    "!16 R3 SUB R12 ARITH_ALWAYS"
	    "!0 R3 LOAD32 R3 MEMORY"
	    "!20 R4 SUB R12 ARITH_ALWAYS"
	    "!0 R4 LOAD32 R4 MEMORY"
	    "!31 R7 LOADI8_ALWAYS"
	    "SYSCALL_ALWAYS");
}

#endif
