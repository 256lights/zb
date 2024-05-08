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

#include <sys/utsname.h>
#define NULL 0
#define __PATH_MAX 4096
#define EOF 0xFFFFFFFF

void* malloc(unsigned size);

int access(char* pathname, int mode)
{
	/* Completely meaningless in bare metal */
	return 0;
}

int chdir(char* path)
{
	/* Completely meaningless in bare metal */
	return 0;
}

int fchdir(int fd)
{
	/* Completely meaningless in bare metal */
	return 0;
}


int fork()
{
	/* Completely meaningless in bare metal */
	return 0;
}


int waitpid (int pid, int* status_ptr, int options)
{
	/* Completely meaningless in bare metal */
	return 0;
}


int execve(char* file_name, char** argv, char** envp)
{
	/* Completely meaningless in bare metal */
	return 0;
}

int __read(int fd)
{
	asm("LOAD R1 R14 0"
	    "FGETC");
}

int read(int fd, char* buf, unsigned count)
{
	unsigned i = 0;
	int c;
	while(i < count)
	{
		c = __read(fd);
		if(EOF == c) break;
		buf[i] = c;
		i = i + 1;
	}
	return i;
}

void __write(char s, int fd)
{
	asm("LOAD R0 R14 0"
	    "LOAD R1 R14 4"
	    "FPUTC");
}

int write(int fd, char* buf, unsigned count)
{
	unsigned i = 0;
	while(i < count)
	{
		__write(buf[i], fd);
		i = i + 1;
	}
}

int lseek(int fd, int offset, int whence)
{
	asm("LOAD R0 R14 0"
	    "LOAD R1 R14 4"
	    "LOAD R2 R14 8"
	    "FSEEK");
}

int close(int fd)
{
	asm("LOAD R0 R14 0"
	    "FCLOSE");
}


int unlink (char* filename)
{
	/* Completely meaningless in bare metal */
	return 0;
}


int _getcwd(char* buf, int size)
{
	/* Completely meaningless in bare metal */
	return 0;
}


char* getcwd(char* buf, unsigned size)
{
	/* Completely meaningless in bare metal */
	return 0;

}


char* getwd(char* buf)
{
	/* Completely meaningless in bare metal */
	return 0;
}


char* get_current_dir_name()
{
	/* Completely meaningless in bare metal */
	return 0;
}

/********************************************************************************
 * We are running on bare metal. Just don't go past the end of install RAM      *
 ********************************************************************************/
int brk(void *addr)
{
	asm("LOAD R0 R14 0"
	    "ADDU R0 R12 R0"
	    "SWAP R0 R12");
}

int uname(struct utsname* unameData)
{
	/* Completely meaningless in bare metal */
	return 0;
}
