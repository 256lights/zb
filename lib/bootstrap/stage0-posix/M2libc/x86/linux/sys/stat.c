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

#ifndef _SYS_STAT_C
#define _SYS_STAT_C

#include <sys/types.h>

#define S_IRWXU 00700
#define S_IXUSR 00100
#define S_IWUSR 00200
#define S_IRUSR 00400

#define S_ISUID 04000
#define S_ISGID 02000
#define S_IXGRP 00010
#define S_IXOTH 00001
#define S_IRGRP 00040
#define S_IROTH 00004
#define S_IWGRP 00020
#define S_IWOTH 00002
#define S_IRWXG 00070
#define S_IRWXO 00007


int chmod(char *pathname, int mode)
{
	asm("lea_ebx,[esp+DWORD] %8"
	    "mov_ebx,[ebx]"
	    "lea_ecx,[esp+DWORD] %4"
	    "mov_ecx,[ecx]"
	    "mov_eax, %15"
	    "int !0x80");
}


int fchmod(int a, mode_t b)
{
	asm("lea_ebx,[esp+DWORD] %8"
	    "mov_ebx,[ebx]"
	    "lea_ecx,[esp+DWORD] %4"
	    "mov_ecx,[ecx]"
	    "mov_eax, %94"
	    "int !0x80");
}


int mkdir(char const* a, mode_t b)
{
	asm("lea_ebx,[esp+DWORD] %8"
	    "mov_ebx,[ebx]"
	    "lea_ecx,[esp+DWORD] %4"
	    "mov_ecx,[ecx]"
	    "mov_eax, %39"
	    "int !0x80");
}


int mknod(char const* a, mode_t b, dev_t c)
{
	asm("lea_ebx,[esp+DWORD] %12"
	    "mov_ebx,[ebx]"
	    "lea_ecx,[esp+DWORD] %8"
	    "mov_ecx,[ecx]"
	    "lea_edx,[esp+DWORD] %4"
	    "mov_edx,[edx]"
	    "mov_eax, %14"
	    "int !0x80");
}


mode_t umask(mode_t m)
{
	asm("lea_ebx,[esp+DWORD] %4"
	    "mov_ebx,[ebx]"
	    "mov_eax, %60"
	    "int !0x80");
}

#endif
