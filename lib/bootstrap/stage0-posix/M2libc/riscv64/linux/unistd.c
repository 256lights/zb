/* Copyright (C) 2020 Jeremiah Orians
 * Copyright (C) 2021 Andrius Štikonas
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
#define __SI_SWAP_ERRNO_CODE

void* malloc(unsigned size);

int access(char* pathname, int mode)
{
	asm("rd_a0 !-100 addi" /* AT_FDCWD */
	    "rd_a1 rs1_fp !-8 ld"
	    "rd_a2 rs1_fp !-16 ld"
	    "rd_a3 addi" /* flags = 0 */
	    "rd_a7 !48 addi"
	    "ecall");
}

int chdir(char* path)
{
	asm("rd_a0 rs1_fp !-8 ld"
	    "rd_a7 !49 addi"
	    "ecall");
}

int fchdir(int fd)
{
	asm("rd_a0 rs1_fp !-8 ld"
	    "rd_a7 !50 addi"
	    "ecall");
}

void _exit(int value);

int fork()
{
	asm("rd_a7 !220 addi"
	    "rd_a0 !17 addi" /* SIGCHld */
	    "rd_a1 mv"       /* Child uses duplicate of parent's stack */
	    "ecall");
}


int waitpid (int pid, int* status_ptr, int options)
{
	/* Uses wait4 with struct rusage *ru set to NULL */
	asm("rd_a0 rs1_fp !-8 ld"
	    "rd_a1 rs1_fp !-16 ld"
	    "rd_a2 rs1_fp !-24 ld"
	    "rd_a3 addi"
	    "rd_a7 !260 addi"
	    "ecall");
}


int execve(char* file_name, char** argv, char** envp)
{
	asm("rd_a0 rs1_fp !-8 ld"
	    "rd_a1 rs1_fp !-16 ld"
	    "rd_a2 rs1_fp !-24 ld"
	    "rd_a7 !221 addi"
	    "ecall");
}

int read(int fd, char* buf, unsigned count)
{
	asm("rd_a0 rs1_fp !-8 ld"
	    "rd_a1 rs1_fp !-16 ld"
	    "rd_a2 rs1_fp !-24 ld"
	    "rd_a7 !63 addi"
	    "ecall");
}

int write(int fd, char* buf, unsigned count)
{
	asm("rd_a0 rs1_fp !-8 ld"
	    "rd_a1 rs1_fp !-16 ld"
	    "rd_a2 rs1_fp !-24 ld"
	    "rd_a7 !64 addi"
	    "ecall");
}

int lseek(int fd, int offset, int whence)
{
	asm("rd_a0 rs1_fp !-8 ld"
	    "rd_a1 rs1_fp !-16 ld"
	    "rd_a2 rs1_fp !-24 ld"
	    "rd_a7 !62 addi"
	    "ecall");
}


int close(int fd)
{
	asm("rd_a0 rs1_fp !-8 ld"
	    "rd_a7 !57 addi"    /* close */
	    "ecall");
}


int unlink (char* filename)
{
	asm("rd_a0 !-100 addi" /* AT_FDCWD */
	    "rd_a1 rs1_fp !-8 ld"
	    "rd_a2 !0 addi"     /* No flags */
	    "rd_a7 !35 addi"    /* unlinkat */
	    "ecall");
}


int _getcwd(char* buf, int size)
{
	asm("rd_a0 rs1_fp !-8 ld"
	    "rd_a1 rs1_fp !-16 ld"
	    "rd_a7 !17 addi"
	    "ecall");
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
	asm("rd_a0 rs1_fp !-8 ld"
	    "rd_a7 !214 addi"
	    "ecall");
}

int uname(struct utsname* unameData)
{
	asm("rd_a0 rs1_fp !-8 ld"
	    "rd_a7 !160 addi"
	    "ecall");
}

int unshare(int flags)
{
	asm("rd_a0 rs1_fp !-8 ld"
	    "rd_a7 !97 addi"
	    "ecall");
}

int geteuid()
{
	asm("rd_a7 !175 addi"
	    "ecall");
}

int getegid()
{
	asm("rd_a7 !177 addi"
	    "ecall");
}

int mount(char *source, char *target, char *filesystemtype, SCM mountflags, void *data)
{
	asm("rd_a0 rs1_fp !-8 ld"
	    "rd_a1 rs1_fp !-16 ld"
	    "rd_a2 rs1_fp !-24 ld"
	    "rd_a3 rs1_fp !-32 ld"
	    "rd_a4 rs1_fp !-40 ld"
	    "rd_a7 !40 addi"
	    "ecall");
}

int chroot(char *path)
{
	asm("rd_a0 rs1_fp !-8 ld"
	    "rd_a7 !51 addi"
	    "ecall");
}

#endif
