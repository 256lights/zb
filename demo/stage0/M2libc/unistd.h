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

#ifndef _UNISTD_H
#define _UNISTD_H
#include <sys/utsname.h>
#ifdef __M2__
#if __uefi__
#include <uefi/unistd.c>
#elif __i386__
#include <x86/linux/unistd.c>
#elif __x86_64__
#include <amd64/linux/unistd.c>
#elif __arm__
#include <armv7l/linux/unistd.c>
#elif __aarch64__
#include <aarch64/linux/unistd.c>
#elif __riscv && __riscv_xlen==32
#include <riscv32/linux/unistd.c>
#elif __riscv && __riscv_xlen==64
#include <riscv64/linux/unistd.c>
#else
#error arch not supported
#endif

#else
#define NULL 0
#define __PATH_MAX 4096

void* malloc(unsigned size);
int access(char* pathname, int mode);
int chdir(char* path);
int fchdir(int fd);
void _exit(int value);
int fork();
int waitpid (int pid, int* status_ptr, int options);
int execve(char* file_name, char** argv, char** envp);
int read(int fd, char* buf, unsigned count);
int write(int fd, char* buf, unsigned count);
int lseek(int fd, int offset, int whence);
int close(int fd);
int unlink (char *filename);
int _getcwd(char* buf, int size);
char* getcwd(char* buf, unsigned size);
char* getwd(char* buf);
char* get_current_dir_name();
int brk(void *addr);

int uname(struct utsname* unameData);

int unshare(int flags);

int geteuid();

int getegid();

int chroot(char const *path);

int mount(char const *source, char const *target, char const *filesystemtype, SCM mountflags, void const *data);

#endif
#endif
