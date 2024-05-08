/* Copyright (C) 2022 Andrius Štikonas
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

#include <uefi/uefi.c>
#include <sys/utsname.h>
#include <stdio.h>

#define NULL 0
#define EOF 0xFFFFFFFF

/* For lseek */
#define SEEK_SET 0
#define SEEK_CUR 1
#define SEEK_END 2

void* malloc(unsigned size);
size_t strlen(char const* str);
char* strncpy(char* dest, char const* src, size_t count);
char* strncat(char* dest, char const* src, size_t count);
void* memcpy(void* dest, void const* src, size_t count);

int open(char* name, int flag, int mode);
int close(int fd);
int access(char* pathname, int mode)
{
	int fd = open(pathname, 0, 0);
	if (fd == -1)
	{
		return -1;
	}
	close(fd);
	return 0;
}

int chdir(char* path)
{
	char* absolute_path = _relative_path_to_absolute(path);
	strncpy(_cwd, absolute_path, __PATH_MAX);
	if(_cwd[strlen(_cwd) - 1] != '\\')
	{
		strncat(_cwd, "/", __PATH_MAX);
	}
	free(absolute_path);
	return 0;
}

int fchdir(int fd)
{
	/* TODO: not yet implemented. */
	return -1;
}

int _get_file_size(struct efi_file_protocol* f)
{
	/* Preallocate some extra space for file_name */
	size_t file_info_size = sizeof(struct efi_file_info);
	struct efi_file_info* file_info = calloc(1, file_info_size);
	unsigned rval = __uefi_4(f, &EFI_FILE_INFO_GUID, &file_info_size, file_info, f->get_info);
	if(rval != EFI_SUCCESS)
	{
		return -1;
	}
	int file_size = file_info->file_size;
	free(file_info);
	return file_size;
}

void _set_environment(char** envp)
{
	unsigned i;
	unsigned j;
	unsigned length = _array_length(envp);
	char* name;
	char* value;
	for(i = 0; i < length; i += 1)
	{
		j = 0;
		name = envp[i];
		while(envp[i][j] != '=')
		{
			j += 1;
		}
		envp[i][j] = 0;
		value = envp[i] + j + 1;
		_set_variable(name, value);
		envp[i][j] = '=';
	}
}

FILE* fopen(char const* filename, char const* mode);
size_t fread(void* buffer, size_t size, size_t count, FILE* stream);
int fclose(FILE* stream);
int spawn(char* file_name, char** argv, char** envp)
{
	FILE* fcmd = fopen(file_name, "r");
	if(fcmd == NULL) return -1;

	long program_size = _get_file_size(fcmd->fd);

	void* executable = malloc(program_size);
	size_t count = fread(executable, 1, program_size, fcmd);
	if(count < program_size)
	{
		free(executable);
		fclose(fcmd);
		return -1;
	}
	fclose(fcmd);

	struct efi_device_path_protocol* device_path = calloc(2, sizeof(struct efi_device_path_protocol));
	device_path->type = HARDWARE_DEVICE_PATH;
	device_path->subtype = MEMORY_MAPPED;
	device_path->length = sizeof(struct efi_device_path_protocol);
	device_path->memory_type = EFI_LOADER_DATA;
	device_path->start_address = executable;
	device_path->end_address = executable + program_size;
	device_path[1].type = END_HARDWARE_DEVICE_PATH;
	device_path[1].subtype = END_ENTIRE_DEVICE_PATH;
	device_path[1].length = 4;

	void* child_ih;

	unsigned rval = __uefi_6(0, _image_handle, device_path, executable, program_size, &child_ih, _system->boot_services->load_image);
	free(device_path);
	free(executable);
	if(rval != EFI_SUCCESS) return -1;
	struct efi_loaded_image_protocol* child_image;
	rval = _open_protocol(child_ih, &EFI_LOADED_IMAGE_PROTOCOL_GUID, &child_image, child_ih, 0, EFI_OPEN_PROTOCOL_BY_HANDLE_PROTOCOL);
	if(rval != EFI_SUCCESS) return -1;

	/* Concatenate char** argv array */
	unsigned arg_length = -1 ;
	unsigned i = 0;
	while(argv[i] != NULL)
	{
		arg_length += strlen(argv[i]) + 1;
		i += 1;
	}
	char* load_options = calloc(arg_length + 1, 1);
	strcpy(load_options, argv[0]);
	i = 1;
	while(argv[i] != NULL)
	{
		strcat(load_options, " ");
		strcat(load_options, argv[i]);
		i += 1;
	}
	char* uefi_path = _string2wide(load_options);

	child_image->load_options = uefi_path;
	child_image->load_options_size = 2 * arg_length;
	free(load_options);
	child_image->device = _image->device;
	rval = _close_protocol(child_ih, &EFI_LOADED_IMAGE_PROTOCOL_GUID, child_ih, 0);
	if(rval != EFI_SUCCESS) return -1;

	/* Setup environment for child process */
	_set_environment(envp);
	_set_variable("cwd", _cwd);
	_set_variable("root", _root);

	/* Run command */
	rval = __uefi_3(child_ih, 0, 0, _system->boot_services->start_image);
	free(uefi_path);

	/* Restore initial environment
	 * For simplicity we just delete all variables and restore them from _envp.
	 * This assumes that _envp is not modified by application, e.g. kaem.
	 */
	_wipe_environment();
	_set_environment(_envp);

	return rval;
}

int fork()
{
	return -1;
}


int waitpid (int pid, int* status_ptr, int options)
{
	return -1;
}


int execve(char* file_name, char** argv, char** envp)
{
	return -1;
}

int read(int fd, char* buf, unsigned count)
{
	struct efi_file_protocol* f = fd;
	__uefi_3(fd, &count, buf, f->read);
	return count;
}

int write(int fd, char* buf, unsigned count)
{
	struct efi_file_protocol* f = fd;
	unsigned i;
	char c = 0;

	/* In UEFI StdErr might not be printing stuff to console, so just use stdout */
	if(f == STDOUT_FILENO || f == STDERR_FILENO)
	{
		for(i = 0; i < count; i += 1)
		{
			c = buf[i];
			__uefi_2(_system->con_out, &c, _system->con_out->output_string);
			if('\n' == c)
			{
				c = '\r';
				__uefi_2(_system->con_out, &c, _system->con_out->output_string);
			}
		}
		return i;
	}

	/* Otherwise write to file */
	__uefi_3(f, &count, buf, f->write);
	return count;
}


int lseek(int fd, int offset, int whence)
{
	struct efi_file_protocol* f = fd;
	if(whence == SEEK_SET)
	{
	}
	else if(whence == SEEK_CUR)
	{
		unsigned position;
		__uefi_2(f, &position, f->get_position);
		offset += position;
	}
	else if(whence == SEEK_END)
	{
		offset += _get_file_size(fd);
	}
	else
	{
		return -1;
	}

	unsigned rval = __uefi_2(f, offset, f->set_position);
	if(rval == EFI_SUCCESS)
	{
		return offset;
	}
	return -1;
}


int close(int fd)
{
	struct efi_file_protocol* f = fd;
	unsigned rval = __uefi_1(f, f->close);
	if(rval != EFI_SUCCESS)
	{
	    return -1;
	}
	return rval;
}


int unlink(char* filename)
{
	FILE* f = fopen(filename, "w");
	struct efi_file_protocol* fd = f->fd;
	__uefi_1(fd, fd->delete);
}


char* getcwd(char* buf, unsigned size)
{
	size_t length = strlen(_cwd);
	if(length >= size) return NULL;
	strcpy(buf, _cwd);
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
	return -1;
}

int uname(struct utsname* unameData)
{
	memcpy(unameData->sysname, "UEFI", 5);
	memcpy(unameData->release, "1.0", 4);
	memcpy(unameData->version, "1.0", 4);
#ifdef __x86_64__
	memcpy(unameData->machine, "x86_64", 7);
#else
#error unsupported arch
#endif
}

int unshare(int flags)
{
	if (flags != 0)
	{
		return -1; // Any unshare operation is invalid
	}
	return 0;
}

int geteuid(int flags)
{
	return 0;
}

int getegid(int flags)
{
	return 0;
}

int chroot(char const *path)
{
	char *newroot = _relative_path_to_absolute(path);
	free(_root);
	_root = newroot;
	if(_root[strlen(_root) - 1] != '\\')
	{
		strncat(_root, "/", __PATH_MAX);
	}
	return 0;
}

int mount(char const *source, char const *target, char const *filesystemtype, SCM mountflags, void const *data)
{
	return -1;
}

#endif
