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

#include <uefi/uefi.c>
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
	return 0;
}


int fchmod(int a, mode_t b)
{
	return 0;
}

int __open(struct efi_file_protocol* _rootdir, char* name, long mode, long attributes);
int mkdir(char const* name, mode_t _mode)
{
	struct efi_file_protocol* new_directory;
	long mode = EFI_FILE_MODE_CREATE | EFI_FILE_MODE_WRITE | EFI_FILE_MODE_READ;
	long attributes = EFI_FILE_DIRECTORY;
	long new_directory = __open(_rootdir, name, mode, attributes);
	if(new_directory != -1)
	{
		_close(new_directory);
		return 0;
	}
	return -1;
}


int mknod(char const* a, mode_t b, dev_t c)
{
	return -1;
}


mode_t umask(mode_t m)
{
	return 0;
}

#endif
