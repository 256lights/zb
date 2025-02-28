/* Copyright (C) 2016 Jeremiah Orians
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

#ifndef _FCNTL_C
#define _FCNTL_C

#ifdef __M2__
#if __uefi__
#include <uefi/fcntl.c>
#elif __i386__
#include <x86/linux/fcntl.c>
#elif __x86_64__
#include <amd64/linux/fcntl.c>
#elif __arm__
#include <armv7l/linux/fcntl.c>
#elif __aarch64__
#include <aarch64/linux/fcntl.c>
#elif __riscv && __riscv_xlen==32
#include <riscv32/linux/fcntl.c>
#elif __riscv && __riscv_xlen==64
#include <riscv64/linux/fcntl.c>
#elif __knight_posix__
#include <knight/linux/fcntl.c>
#elif __knight__
#include <knight/native/fcntl.c>
#else
#error arch not supported
#endif
#else
extern int _open(char* name, int flag, int mode);
#endif

int errno;

int open(char* name, int flag, int mode)
{
	int fd = _open(name, flag, mode);
	if(0 > fd)
	{
		errno = -fd;
		fd = -1;
	}
	return fd;
}

#endif
