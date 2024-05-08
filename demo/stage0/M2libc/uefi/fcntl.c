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

#ifndef __FCNTL_C
#define __FCNTL_C

#define O_RDONLY 0
#define O_WRONLY 1
#define O_RDWR 2
#define O_CREAT 00100
#define O_EXCL 00200
#define O_TRUNC 001000
#define O_APPEND 002000

#define S_IXUSR 00100
#define S_IWUSR 00200
#define S_IRUSR 00400
#define S_IRWXU 00700

#include <uefi/uefi.c>

void free(void* l);

int __open(struct efi_file_protocol* _rootdir, char* name, long mode, long attributes)
{
	struct efi_file_protocol* new_handle;
	char* wide_name = _posix_path_to_uefi(name);
	unsigned rval = __uefi_5(_rootdir, &new_handle, wide_name, mode, attributes, _rootdir->open);
	free(wide_name);
	if(rval != EFI_SUCCESS)
	{
		return -1;
	}
	return new_handle;
}

int _open(char* name, int flag, int mode)
{
	long mode = 0;
	long attributes = 0;
	if ((flag == (O_WRONLY | O_CREAT | O_TRUNC)) || (flag == (O_RDWR | O_CREAT | O_EXCL)))
	{
		mode = EFI_FILE_MODE_CREATE | EFI_FILE_MODE_WRITE | EFI_FILE_MODE_READ;
	}
	else
	{       /* Everything else is a read */
		mode = EFI_FILE_MODE_READ;
		attributes = EFI_FILE_READ_ONLY;
	}
	return __open(_rootdir, name, mode, attributes);
}

#define STDIN_FILENO  0
#define STDOUT_FILENO 1
#define STDERR_FILENO 2
#endif
