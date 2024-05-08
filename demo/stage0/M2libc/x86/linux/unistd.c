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
	asm("lea_ebx,[esp+DWORD] %8"
	    "mov_ebx,[ebx]"
	    "lea_ecx,[esp+DWORD] %4"
	    "mov_ecx,[ecx]"
	    "mov_eax, %33"
	    "int !0x80");
}

int chdir(char* path)
{
	asm("lea_ebx,[esp+DWORD] %4"
	    "mov_ebx,[ebx]"
	    "mov_eax, %12"
	    "int !0x80");
}

int fchdir(int fd)
{
	asm("lea_ebx,[esp+DWORD] %4"
	    "mov_ebx,[ebx]"
	    "mov_eax, %133"
	    "int !0x80");
}

/* Defined in the libc */
void _exit(int value);

int fork()
{
	asm("mov_eax, %2"
	    "mov_ebx, %0"
	    "int !0x80");
}


int waitpid (int pid, int* status_ptr, int options)
{
	asm("lea_ebx,[esp+DWORD] %12"
	    "mov_ebx,[ebx]"
	    "lea_ecx,[esp+DWORD] %8"
	    "mov_ecx,[ecx]"
	    "lea_edx,[esp+DWORD] %4"
	    "mov_edx,[edx]"
	    "mov_eax, %7"
	    "int !0x80");
}


int execve(char* file_name, char** argv, char** envp)
{
	asm("lea_ebx,[esp+DWORD] %12"
	    "mov_ebx,[ebx]"
	    "lea_ecx,[esp+DWORD] %8"
	    "mov_ecx,[ecx]"
	    "lea_edx,[esp+DWORD] %4"
	    "mov_edx,[edx]"
	    "mov_eax, %11"
	    "int !0x80");
}

int read(int fd, char* buf, unsigned count) {
	asm("lea_ebx,[esp+DWORD] %12"
	    "mov_ebx,[ebx]"
	    "lea_ecx,[esp+DWORD] %8"
	    "mov_ecx,[ecx]"
	    "lea_edx,[esp+DWORD] %4"
	    "mov_edx,[edx]"
	    "mov_eax, %3"
	    "int !0x80");
}

int write(int fd, char* buf, unsigned count) {
	asm("lea_ebx,[esp+DWORD] %12"
	    "mov_ebx,[ebx]"
	    "lea_ecx,[esp+DWORD] %8"
	    "mov_ecx,[ecx]"
	    "lea_edx,[esp+DWORD] %4"
	    "mov_edx,[edx]"
	    "mov_eax, %4"
	    "int !0x80");
}

int lseek(int fd, int offset, int whence)
{
	asm("lea_ebx,[esp+DWORD] %12"
	    "mov_ebx,[ebx]"
	    "lea_ecx,[esp+DWORD] %8"
	    "mov_ecx,[ecx]"
	    "lea_edx,[esp+DWORD] %4"
	    "mov_edx,[edx]"
	    "mov_eax, %19"
	    "int !0x80");
}

int close(int fd)
{
	asm("lea_ebx,[esp+DWORD] %4"
	    "mov_ebx,[ebx]"
	    "mov_eax, %6"
	    "int !0x80");
}


int unlink (char *filename)
{
	asm("lea_ebx,[esp+DWORD] %4"
	    "mov_ebx,[ebx]"
	    "mov_eax, %10"
	    "int !0x80");
}


int _getcwd(char* buf, int size)
{
	asm("lea_ebx,[esp+DWORD] %8"
	    "mov_ebx,[ebx]"
	    "lea_ecx,[esp+DWORD] %4"
	    "mov_ecx,[ecx]"
	    "mov_eax, %183"
	    "int !0x80");
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
	asm("mov_eax,[esp+DWORD] %4"
	    "push_eax"
	    "mov_eax, %45"
	    "pop_ebx"
	    "int !0x80");
}

int uname(struct utsname* unameData)
{
	asm("lea_ebx,[esp+DWORD] %4"
	    "mov_ebx,[ebx]"
	    "mov_eax, %109"
	    "int !0x80");
}

int unshare(int flags)
{
	asm("lea_ebx,[esp+DWORD] %4"
	    "mov_ebx,[ebx]"
	    "mov_eax, %310"
	    "int !0x80");
}

int geteuid()
{
	asm("mov_eax, %201"
	    "int !0x80");
}

int getegid()
{
	asm("mov_eax, %202"
	    "int !0x80");
}

int mount(char *source, char *target, char *filesystemtype, SCM mountflags, void *data)
{
	asm("lea_ebx,[esp+DWORD] %20"
	    "mov_ebx,[ebx]"
	    "lea_ecx,[esp+DWORD] %16"
	    "mov_ecx,[ecx]"
	    "lea_edx,[esp+DWORD] %12"
	    "mov_edx,[edx]"
	    "lea_esi,[esp+DWORD] %8"
	    "mov_esi,[esi]"
	    "lea_edi,[esp+DWORD] %4"
	    "mov_edi,[edi]"
	    "mov_eax, %21"
	    "int !0x80");
}

int chroot(char *path)
{
	asm("lea_ebx,[esp+DWORD] %4"
	    "mov_ebx,[ebx]"
	    "mov_eax, %61"
	    "int !0x80");
}

#endif
