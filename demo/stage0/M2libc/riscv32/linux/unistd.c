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

#include <signal.h>
#include <sys/utsname.h>
#define NULL 0
#define __PATH_MAX 4096

#define P_PID 1
#define WEXITED 4
#define __SI_SWAP_ERRNO_CODE

void* malloc(unsigned size);

int access(char* pathname, int mode)
{
	asm("rd_a0 !-100 addi" /* AT_FDCWD */
	    "rd_a1 rs1_fp !-4 lw"
	    "rd_a2 rs1_fp !-8 lw"
	    "rd_a3 addi" /* flags = 0 */
	    "rd_a7 !48 addi"
	    "ecall");
}

int chdir(char* path)
{
	asm("rd_a0 rs1_fp !-4 lw"
	    "rd_a7 !49 addi"
	    "ecall");
}

int fchdir(int fd)
{
	asm("rd_a0 rs1_fp !-4 lw"
	    "rd_a7 !50 addi"
	    "ecall");
}

void _exit(int value);

int fork()
{
	asm("rd_a7 !220 addi"
	    "rd_a0 !17 addi" /* SIGCHLD */
	    "rd_a1 mv"       /* Child uses duplicate of parent's stack */
	    "ecall");
}

int waitid(int idtype, int id, struct siginfo_t *infop, int options, void *rusage)
{
	asm("rd_a0 rs1_fp !-4 lw"
	    "rd_a1 rs1_fp !-8 lw"
	    "rd_a2 rs1_fp !-12 lw"
	    "rd_a3 rs1_fp !-16 lw"
	    "rd_a4 rs1_fp !-20 lw"
	    "rd_a7 !95 addi"
	    "ecall");
}

void* calloc(int count, int size);
void free(void* l);
struct siginfo_t *__waitpid_info;
int waitpid(int pid, int* status_ptr, int options)
{
	if(NULL == __waitpid_info) __waitpid_info = calloc(1, sizeof(struct siginfo_t));
	int r = waitid(P_PID, pid, __waitpid_info, options|WEXITED, NULL);

	if(__waitpid_info->si_pid != 0)
	{
		int sw = 0;
		if(__waitpid_info->si_code == CLD_EXITED)
		{
			sw = (__waitpid_info->si_status & 0xff) << 8;
		}
		else if(__waitpid_info->si_code == CLD_KILLED)
		{
			sw = __waitpid_info->si_status & 0x7f;
		}
		else if(__waitpid_info->si_code == CLD_DUMPED)
		{
			sw = (__waitpid_info->si_status & 0x7f) | 0x80;
		}
		else if(__waitpid_info->si_code == CLD_CONTINUED)
		{
			sw = 0xffff;
		}
		else if(__waitpid_info->si_code == CLD_STOPPED || __waitpid_info->si_code == CLD_TRAPPED)
		{
			sw = ((__waitpid_info->si_status & 0xff) << 8) + 0x7f;
		}
		if(status_ptr != NULL) *status_ptr = sw;
	}
	int rval = __waitpid_info->si_pid;

	if(r < 0)
	{
		return r;
	}
	return rval;
}

int execve(char* file_name, char** argv, char** envp)
{
	asm("rd_a0 rs1_fp !-4 lw"
	    "rd_a1 rs1_fp !-8 lw"
	    "rd_a2 rs1_fp !-12 lw"
	    "rd_a7 !221 addi"
	    "ecall");
}

int read(int fd, char* buf, unsigned count)
{
	asm("rd_a0 rs1_fp !-4 lw"
	    "rd_a1 rs1_fp !-8 lw"
	    "rd_a2 rs1_fp !-12 lw"
	    "rd_a7 !63 addi"
	    "ecall");
}

int write(int fd, char* buf, unsigned count)
{
	asm("rd_a0 rs1_fp !-4 lw"
	    "rd_a1 rs1_fp !-8 lw"
	    "rd_a2 rs1_fp !-12 lw"
	    "rd_a7 !64 addi"
	    "ecall");
}

int llseek(int fd, int offset_high, int offset_low, int result, int whence)
{
	asm("rd_a0 rs1_fp !-4 lw"
	    "rd_a1 rs1_fp !-8 lw"
	    "rd_a2 rs1_fp !-12 lw"
	    "rd_a3 rs1_fp !-16 lw"
	    "rd_a4 rs1_fp !-20 lw"
	    "rd_a7 !62 addi"
	    "ecall");
}

int lseek(int fd, int offset, int whence)
{
	int result;
	if(llseek(fd, offset >> 32, offset, &result, whence))
	{
		return -1;
	}
	return result;
}

int close(int fd)
{
	asm("rd_a0 rs1_fp !-4 lw"
	    "rd_a7 !57 addi"    /* close */
	    "ecall");
}


int unlink (char* filename)
{
	asm("rd_a0 !-100 addi" /* AT_FDCWD */
	    "rd_a1 rs1_fp !-4 lw"
	    "rd_a2 !0 addi"     /* No flags */
	    "rd_a7 !35 addi"    /* unlinkat */
	    "ecall");
}


int _getcwd(char* buf, int size)
{
	asm("rd_a0 rs1_fp !-4 lw"
	    "rd_a1 rs1_fp !-8 lw"
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
	asm("rd_a0 rs1_fp !-4 lw"
	    "rd_a7 !214 addi"
	    "ecall");
}

int uname(struct utsname* unameData)
{
	asm("rd_a0 rs1_fp !-4 lw"
	    "rd_a7 !160 addi"
	    "ecall");
}

int unshare(int flags)
{
	asm("rd_a0 rs1_fp !-4 lw"
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

int mount (char *source, char *target, char *filesystemtype, SCM mountflags, void *data)
{
	asm("rd_a0 rs1_fp !-4 lw"
	    "rd_a1 rs1_fp !-8 lw"
	    "rd_a2 rs1_fp !-12 lw"
	    "rd_a3 rs1_fp !-16 lw"
	    "rd_a4 rs1_fp !-20 lw"
	    "rd_a7 !40 addi"
	    "ecall");
}

int chroot(char *path)
{
	asm("rd_a0 rs1_fp !-4 lw"
	    "rd_a7 !51 addi"
	    "ecall");
}

#endif
