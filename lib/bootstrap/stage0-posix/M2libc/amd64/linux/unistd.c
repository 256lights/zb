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
	asm("lea_rdi,[rsp+DWORD] %16"
	    "mov_rdi,[rdi]"
	    "lea_rsi,[rsp+DWORD] %8"
	    "mov_rsi,[rsi]"
	    "mov_rax, %21"
	    "syscall");
}

int chdir(char* path)
{
	asm("lea_rdi,[rsp+DWORD] %8"
	    "mov_rdi,[rdi]"
	    "mov_rax, %80"
	    "syscall");
}

int fchdir(int fd)
{
	asm("lea_rdi,[rsp+DWORD] %8"
	    "mov_rdi,[rdi]"
	    "mov_rax, %81"
	    "syscall");
}

void _exit(int value);

int fork()
{
	asm("mov_rax, %57"
	    "mov_rdi, %0"
	    "syscall");
}


int waitpid (int pid, int* status_ptr, int options)
{
	/* Uses wait4 with struct rusage *ru set to NULL */
	asm("lea_rdi,[rsp+DWORD] %24"
	    "mov_rdi,[rdi]"
	    "lea_rsi,[rsp+DWORD] %16"
	    "mov_rsi,[rsi]"
	    "lea_rdx,[rsp+DWORD] %8"
	    "mov_rdx,[rdx]"
	    "mov_r10, %0"
	    "mov_rax, %61"
	    "syscall");
}


int execve(char* file_name, char** argv, char** envp)
{
	asm("lea_rdi,[rsp+DWORD] %24"
	    "mov_rdi,[rdi]"
	    "lea_rsi,[rsp+DWORD] %16"
	    "mov_rsi,[rsi]"
	    "lea_rdx,[rsp+DWORD] %8"
	    "mov_rdx,[rdx]"
	    "mov_rax, %59"
	    "syscall");
}

int read(int fd, char* buf, unsigned count)
{ /*maybe*/
	asm("lea_rdi,[rsp+DWORD] %24"
	    "mov_rdi,[rdi]"
	    "lea_rsi,[rsp+DWORD] %16"
	    "mov_rsi,[rsi]"
	    "lea_rdx,[rsp+DWORD] %8"
	    "mov_rdx,[rdx]"
	    "mov_rax, %0"
	    "syscall");
}

int write(int fd, char* buf, unsigned count)
{/*maybe*/
	asm("lea_rdi,[rsp+DWORD] %24"
	    "mov_rdi,[rdi]"
	    "lea_rsi,[rsp+DWORD] %16"
	    "mov_rsi,[rsi]"
	    "lea_rdx,[rsp+DWORD] %8"
	    "mov_rdx,[rdx]"
	    "mov_rax, %1"
	    "syscall");
}

int lseek(int fd, int offset, int whence)
{
	asm("lea_rdi,[rsp+DWORD] %24"
	    "mov_rdi,[rdi]"
	    "lea_rsi,[rsp+DWORD] %16"
	    "mov_rsi,[rsi]"
	    "lea_rdx,[rsp+DWORD] %8"
	    "mov_rdx,[rdx]"
	    "mov_rax, %8"
	    "syscall");
}


int close(int fd)
{
	asm("lea_rdi,[rsp+DWORD] %8"
	    "mov_rdi,[rdi]"
	    "mov_rax, %3"
	    "syscall");
}


int unlink (char* filename)
{
	asm("lea_rdi,[rsp+DWORD] %8"
	    "mov_rdi,[rdi]"
	    "mov_rax, %87"
	    "syscall");
}


int _getcwd(char* buf, int size)
{
	asm("lea_rdi,[rsp+DWORD] %16"
	    "mov_rdi,[rdi]"
	    "lea_rsi,[rsp+DWORD] %8"
	    "mov_rsi,[rsi]"
	    "mov_rax, %79"
	    "syscall");
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
	asm("mov_rax,[rsp+DWORD] %8"
	    "push_rax"
	    "mov_rax, %12"
	    "pop_rbx"
	    "mov_rdi,rbx"
	    "syscall");
}

int uname(struct utsname* unameData)
{
	asm("lea_rdi,[rsp+DWORD] %8"
	    "mov_rdi,[rdi]"
	    "mov_rax, %63"
	    "syscall");
}

int unshare(int flags)
{
	asm("lea_rdi,[rsp+DWORD] %8"
	    "mov_rdi,[rdi]"
	    "mov_rax, %272"
	    "syscall");
}

int geteuid()
{
	asm("mov_rax, %107"
	    "syscall");
}

int getegid()
{
	asm("mov_rax, %108"
	    "syscall");
}

int mount(char *source, char *target, char *filesystemtype, SCM mountflags, void *data)
{
	asm("lea_rdi,[rsp+DWORD] %40"
	    "mov_rdi,[rdi]"
	    "lea_rsi,[rsp+DWORD] %32"
	    "mov_rsi,[rsi]"
	    "lea_rdx,[rsp+DWORD] %24"
	    "mov_rdx,[rdx]"
	    "lea_r10,[rsp+DWORD] %16"
	    "mov_r10,[r10]"
	    "lea_r8,[rsp+DWORD] %8"
	    "mov_r8,[r8]"
	    "mov_rax, %165"
	    "syscall");
}

int chroot(char *path)
{
	asm("lea_rdi,[rsp+DWORD] %8"
	    "mov_rdi,[rdi]"
	    "mov_rax, %161"
	    "syscall");
}

#endif
